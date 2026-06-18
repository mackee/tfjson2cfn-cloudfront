package converter

import "gopkg.in/yaml.v3"

// convertDistribution renders aws_cloudfront_distribution as
// AWS::CloudFront::Distribution. Terraform's flat resource body is nested under
// DistributionConfig, and default/empty attributes are omitted.
func convertDistribution(r plannedResource, cfg map[string]any) (string, *yaml.Node) {
	v := r.Values
	dc := newMapping()

	dc.set("Enabled", scalar(boolVal(v, "enabled")))
	if s := str(v, "default_root_object"); s != "" {
		dc.set("DefaultRootObject", scalar(s))
	}
	if al := strList(v, "aliases"); len(al) > 0 {
		dc.set("Aliases", stringSeq(al))
	}
	if c := str(v, "comment"); c != "" {
		dc.set("Comment", scalar(c))
	}
	if pc := str(v, "price_class"); pc != "" && pc != "PriceClass_All" {
		dc.set("PriceClass", scalar(pc))
	}
	if hv := str(v, "http_version"); hv != "" && hv != "http2" {
		dc.set("HttpVersion", scalar(hv))
	}
	if boolVal(v, "is_ipv6_enabled") {
		dc.set("IPV6Enabled", scalar(true))
	}
	if w := str(v, "web_acl_id"); w != "" {
		dc.set("WebACLId", scalar(w))
	}

	// Origins (origin is a set: matched to config by origin_id).
	originCfg := blocks(cfg, "origin")
	var origins []*yaml.Node
	for _, o := range blocks(v, "origin") {
		oc := matchConfigBlock(originCfg, "origin_id", str(o, "origin_id"))
		origins = append(origins, originNode(o, oc))
	}
	dc.set("Origins", sequence(origins...))

	if db := firstBlock(v, "default_cache_behavior"); db != nil {
		dc.set("DefaultCacheBehavior", cacheBehaviorNode(db, firstBlock(cfg, "default_cache_behavior"), false))
	}

	orderedCfg := blocks(cfg, "ordered_cache_behavior")
	var behaviors []*yaml.Node
	for _, b := range blocks(v, "ordered_cache_behavior") {
		bc := matchConfigBlock(orderedCfg, "path_pattern", str(b, "path_pattern"))
		behaviors = append(behaviors, cacheBehaviorNode(b, bc, true))
	}
	dc.set("CacheBehaviors", sequence(behaviors...))

	var errs []*yaml.Node
	for _, ce := range blocks(v, "custom_error_response") {
		errs = append(errs, customErrorNode(ce))
	}
	dc.set("CustomErrorResponses", sequence(errs...))

	dc.set("Restrictions", restrictionsNode(firstBlock(v, "restrictions")))
	dc.set("ViewerCertificate", viewerCertificateNode(firstBlock(v, "viewer_certificate")))

	if lc := firstBlock(v, "logging_config"); lc != nil {
		lm := newMapping()
		lm.set("Bucket", scalar(str(lc, "bucket")))
		if p := str(lc, "prefix"); p != "" {
			lm.set("Prefix", scalar(p))
		}
		if boolVal(lc, "include_cookies") {
			lm.set("IncludeCookies", scalar(true))
		}
		dc.set("Logging", lm.orNil())
	}

	props := newMapping()
	props.set("DistributionConfig", dc.emptyNode())
	return "AWS::CloudFront::Distribution", props.emptyNode()
}

func originNode(o, oc map[string]any) *yaml.Node {
	m := newMapping()
	m.set("Id", scalar(str(o, "origin_id")))
	m.set("DomainName", scalar(str(o, "domain_name")))
	if p := str(o, "origin_path"); p != "" {
		m.set("OriginPath", scalar(p))
	}
	hasOAC := false
	if ref := refFromExpr(oc, "origin_access_control_id"); ref != nil {
		m.set("OriginAccessControlId", ref.resolve())
		hasOAC = true
	} else if s := str(o, "origin_access_control_id"); s != "" {
		m.set("OriginAccessControlId", scalar(s))
		hasOAC = true
	}

	if hs := blocks(o, "custom_header"); len(hs) > 0 {
		var headers []*yaml.Node
		for _, h := range hs {
			hm := newMapping()
			hm.set("HeaderName", scalar(str(h, "name")))
			hm.set("HeaderValue", scalar(str(h, "value")))
			headers = append(headers, hm.emptyNode())
		}
		m.set("OriginCustomHeaders", sequence(headers...))
	}

	// An Origin needs exactly one type discriminator. Emit S3OriginConfig when
	// the plan carries an s3_origin_config block, or — for the modern OAC
	// pattern, where origin_access_control_id is set and the block is omitted —
	// synthesize one with an empty OriginAccessIdentity (the form AWS itself
	// uses to describe OAC-fronted S3 origins). The synthesis is skipped when
	// the origin is an explicit custom HTTP origin.
	s3 := firstBlock(o, "s3_origin_config")
	co := firstBlock(o, "custom_origin_config")
	if s3 != nil || (hasOAC && co == nil) {
		s3m := newMapping()
		if s3 != nil {
			// An empty {} is meaningful here, so it is emitted as-is.
			if oai := str(s3, "origin_access_identity"); oai != "" {
				s3m.set("OriginAccessIdentity", scalar(oai))
			}
		} else {
			// Modern OAC pattern: the empty OriginAccessIdentity is the discriminator.
			s3m.set("OriginAccessIdentity", scalar(""))
		}
		m.set("S3OriginConfig", s3m.emptyNode())
	}
	if co != nil {
		// Emit every value the plan carries, rather than dropping ones that
		// happen to equal a CloudFront/Terraform default: the converter is a
		// fidelity translator, so the output should reflect the resolved plan.
		cm := newMapping()
		cm.set("HTTPPort", scalar(num(co, "http_port")))
		if hp := numPtr(co, "https_port"); hp != nil {
			cm.set("HTTPSPort", scalar(*hp))
		}
		cm.set("OriginProtocolPolicy", scalar(str(co, "origin_protocol_policy")))
		if ssl := strList(co, "origin_ssl_protocols"); len(ssl) > 0 {
			cm.set("OriginSSLProtocols", stringSeq(ssl))
		}
		if rt := numPtr(co, "origin_read_timeout"); rt != nil {
			cm.set("OriginReadTimeout", scalar(*rt))
		}
		if kt := numPtr(co, "origin_keepalive_timeout"); kt != nil {
			cm.set("OriginKeepaliveTimeout", scalar(*kt))
		}
		m.set("CustomOriginConfig", cm.emptyNode())
	}
	return m.emptyNode()
}

func cacheBehaviorNode(b, bc map[string]any, ordered bool) *yaml.Node {
	m := newMapping()
	if ordered {
		m.set("PathPattern", scalar(str(b, "path_pattern")))
	}
	m.set("TargetOriginId", scalar(str(b, "target_origin_id")))
	m.set("ViewerProtocolPolicy", scalar(str(b, "viewer_protocol_policy")))
	if boolVal(b, "compress") {
		m.set("Compress", scalar(true))
	}
	// Emit the method lists whenever the plan sets them (both are required on a
	// cache behavior), including when they equal CloudFront's GET/HEAD default,
	// so the output stays faithful to the resolved plan.
	if am := strList(b, "allowed_methods"); len(am) > 0 {
		m.set("AllowedMethods", stringSeq(am))
	}
	if cm := strList(b, "cached_methods"); len(cm) > 0 {
		m.set("CachedMethods", stringSeq(cm))
	}
	m.set("CachePolicyId", policyField(b, bc, "cache_policy_id"))
	m.set("OriginRequestPolicyId", policyField(b, bc, "origin_request_policy_id"))
	m.set("ResponseHeadersPolicyId", policyField(b, bc, "response_headers_policy_id"))
	if t := numPtr(b, "min_ttl"); t != nil && *t != 0 {
		m.set("MinTTL", scalar(*t))
	}
	if t := numPtr(b, "default_ttl"); t != nil && *t != 0 {
		m.set("DefaultTTL", scalar(*t))
	}
	if t := numPtr(b, "max_ttl"); t != nil && *t != 0 {
		m.set("MaxTTL", scalar(*t))
	}
	m.set("FunctionAssociations", functionAssociations(b, bc))
	m.set("LambdaFunctionAssociations", lambdaAssociations(b))
	m.set("TrustedKeyGroups", trustedKeyGroups(b, bc))
	if fv := firstBlock(b, "forwarded_values"); fv != nil {
		m.set("ForwardedValues", forwardedValuesNode(fv))
	}
	return m.emptyNode()
}

// policyField resolves a *_policy_id attribute to a Ref when it points at a
// managed policy resource in the plan, otherwise emits the literal id.
func policyField(b, bc map[string]any, field string) *yaml.Node {
	if ref := refFromExpr(bc, field); ref != nil {
		return ref.resolve()
	}
	if s := str(b, field); s != "" {
		return scalar(s)
	}
	return nil
}

func functionAssociations(b, bc map[string]any) *yaml.Node {
	fas := blocks(b, "function_association")
	if len(fas) == 0 {
		return nil
	}
	cfgFas := blocks(bc, "function_association")
	var nodes []*yaml.Node
	for _, fa := range fas {
		et := str(fa, "event_type")
		fm := newMapping()
		fm.set("EventType", scalar(et))
		if ref := refFromExpr(matchConfigBlock(cfgFas, "event_type", et), "function_arn"); ref != nil {
			fm.set("FunctionARN", ref.resolve())
		} else if s := str(fa, "function_arn"); s != "" {
			fm.set("FunctionARN", scalar(s))
		}
		nodes = append(nodes, fm.emptyNode())
	}
	return sequence(nodes...)
}

func lambdaAssociations(b map[string]any) *yaml.Node {
	las := blocks(b, "lambda_function_association")
	if len(las) == 0 {
		return nil
	}
	var nodes []*yaml.Node
	for _, la := range las {
		lm := newMapping()
		lm.set("EventType", scalar(str(la, "event_type")))
		if s := str(la, "lambda_arn"); s != "" {
			lm.set("LambdaFunctionARN", scalar(s))
		}
		if boolVal(la, "include_body") {
			lm.set("IncludeBody", scalar(true))
		}
		nodes = append(nodes, lm.emptyNode())
	}
	return sequence(nodes...)
}

func trustedKeyGroups(b, bc map[string]any) *yaml.Node {
	if node, ok := bc["trusted_key_groups"].(map[string]any); ok {
		if refs, ok := node["references"].([]any); ok {
			var nodes []*yaml.Node
			for _, ref := range distinctCloudFrontRefs(refs) {
				nodes = append(nodes, refNode(logicalID(ref.resourceType, ref.name)))
			}
			return sequence(nodes...)
		}
	}
	if kgs := strList(b, "trusted_key_groups"); len(kgs) > 0 {
		return stringSeq(kgs)
	}
	return nil
}

func customErrorNode(ce map[string]any) *yaml.Node {
	m := newMapping()
	m.set("ErrorCode", scalar(num(ce, "error_code")))
	if rc := numPtr(ce, "response_code"); rc != nil && *rc != 0 {
		m.set("ResponseCode", scalar(*rc))
	}
	if p := str(ce, "response_page_path"); p != "" {
		m.set("ResponsePagePath", scalar(p))
	}
	// 0 is a valid ErrorCachingMinTTL ("do not cache"), so emit whenever set;
	// 0 is not a valid ResponseCode, so it is treated as unset above.
	if t := numPtr(ce, "error_caching_min_ttl"); t != nil {
		m.set("ErrorCachingMinTTL", scalar(*t))
	}
	return m.emptyNode()
}

// restrictionsNode emits Restrictions only when a real geo restriction is set;
// the default "none" is omitted.
func restrictionsNode(r map[string]any) *yaml.Node {
	if r == nil {
		return nil
	}
	geo := firstBlock(r, "geo_restriction")
	if geo == nil {
		return nil
	}
	rt := str(geo, "restriction_type")
	locs := strList(geo, "locations")
	if (rt == "" || rt == "none") && len(locs) == 0 {
		return nil
	}
	gm := newMapping()
	gm.set("RestrictionType", scalar(rt))
	if len(locs) > 0 {
		gm.set("Locations", stringSeq(locs))
	}
	rm := newMapping()
	rm.set("GeoRestriction", gm.emptyNode())
	return rm.emptyNode()
}

// viewerCertificateNode emits ViewerCertificate only when a real certificate is
// configured; the default CloudFront certificate is omitted (localfront serves
// plain HTTP).
func viewerCertificateNode(vc map[string]any) *yaml.Node {
	if vc == nil {
		return nil
	}
	acm := str(vc, "acm_certificate_arn")
	iam := str(vc, "iam_certificate_id")
	if boolVal(vc, "cloudfront_default_certificate") && acm == "" && iam == "" {
		return nil
	}
	m := newMapping()
	if acm != "" {
		m.set("AcmCertificateArn", scalar(acm))
	}
	if iam != "" {
		m.set("IamCertificateId", scalar(iam))
	}
	if s := str(vc, "ssl_support_method"); s != "" {
		m.set("SslSupportMethod", scalar(s))
	}
	if s := str(vc, "minimum_protocol_version"); s != "" {
		m.set("MinimumProtocolVersion", scalar(s))
	}
	return m.orNil()
}

func forwardedValuesNode(fv map[string]any) *yaml.Node {
	m := newMapping()
	m.set("QueryString", scalar(boolVal(fv, "query_string")))
	if cookies := firstBlock(fv, "cookies"); cookies != nil {
		cm := newMapping()
		cm.set("Forward", scalar(str(cookies, "forward")))
		if wl := strList(cookies, "whitelisted_names"); len(wl) > 0 {
			cm.set("WhitelistedNames", stringSeq(wl))
		}
		m.set("Cookies", cm.orNil())
	}
	if h := strList(fv, "headers"); len(h) > 0 {
		m.set("Headers", stringSeq(h))
	}
	if qs := strList(fv, "query_string_cache_keys"); len(qs) > 0 {
		m.set("QueryStringCacheKeys", stringSeq(qs))
	}
	return m.orNil()
}
