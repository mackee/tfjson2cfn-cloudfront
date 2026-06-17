package converter

import "gopkg.in/yaml.v3"

// convertResponseHeadersPolicy renders aws_cloudfront_response_headers_policy as
// AWS::CloudFront::ResponseHeadersPolicy. Each sub-config is emitted only when
// present. Unlike the cache/origin-request policies, the CORS allowlists keep
// their nested Items shape ({ Items: [...] }), matching CloudFormation.
func convertResponseHeadersPolicy(r plannedResource, _ map[string]any) (string, *yaml.Node) {
	v := r.Values

	cfg := newMapping()
	cfg.set("Name", scalar(str(v, "name")))
	if c := str(v, "comment"); c != "" {
		cfg.set("Comment", scalar(c))
	}
	cfg.set("CorsConfig", corsConfigNode(firstBlock(v, "cors_config")))
	cfg.set("CustomHeadersConfig", customHeadersConfigNode(firstBlock(v, "custom_headers_config")))
	cfg.set("RemoveHeadersConfig", removeHeadersConfigNode(firstBlock(v, "remove_headers_config")))
	cfg.set("SecurityHeadersConfig", securityHeadersConfigNode(firstBlock(v, "security_headers_config")))
	cfg.set("ServerTimingHeadersConfig", serverTimingConfigNode(firstBlock(v, "server_timing_headers_config")))

	props := newMapping()
	props.set("ResponseHeadersPolicyConfig", cfg.emptyNode())
	return "AWS::CloudFront::ResponseHeadersPolicy", props.emptyNode()
}

func corsConfigNode(c map[string]any) *yaml.Node {
	if c == nil {
		return nil
	}
	m := newMapping()
	// AccessControlAllowCredentials and OriginOverride are required by
	// CloudFormation, so they are emitted even when false.
	m.set("AccessControlAllowCredentials", scalar(boolVal(c, "access_control_allow_credentials")))
	m.set("AccessControlAllowHeaders", itemsListNode(firstBlock(c, "access_control_allow_headers")))
	m.set("AccessControlAllowMethods", itemsListNode(firstBlock(c, "access_control_allow_methods")))
	m.set("AccessControlAllowOrigins", itemsListNode(firstBlock(c, "access_control_allow_origins")))
	m.set("AccessControlExposeHeaders", itemsListNode(firstBlock(c, "access_control_expose_headers")))
	if t := numPtr(c, "access_control_max_age_sec"); t != nil && *t != 0 {
		m.set("AccessControlMaxAgeSec", scalar(*t))
	}
	m.set("OriginOverride", scalar(boolVal(c, "origin_override")))
	return m.orNil()
}

// itemsListNode wraps a Terraform { items = [...] } block as the CloudFormation
// { Items: [...] } shape used throughout the CORS config.
func itemsListNode(block map[string]any) *yaml.Node {
	if block == nil {
		return nil
	}
	items := strList(block, "items")
	if len(items) == 0 {
		return nil
	}
	m := newMapping()
	m.set("Items", stringSeq(items))
	return m.emptyNode()
}

func customHeadersConfigNode(c map[string]any) *yaml.Node {
	if c == nil {
		return nil
	}
	var nodes []*yaml.Node
	for _, it := range blocks(c, "items") {
		hm := newMapping()
		hm.set("Header", scalar(str(it, "header")))
		hm.set("Value", scalar(str(it, "value")))
		hm.set("Override", scalar(boolVal(it, "override")))
		nodes = append(nodes, hm.emptyNode())
	}
	seq := sequence(nodes...)
	if seq == nil {
		return nil
	}
	m := newMapping()
	m.set("Items", seq)
	return m.emptyNode()
}

func removeHeadersConfigNode(c map[string]any) *yaml.Node {
	if c == nil {
		return nil
	}
	var nodes []*yaml.Node
	for _, it := range blocks(c, "items") {
		hm := newMapping()
		hm.set("Header", scalar(str(it, "header")))
		nodes = append(nodes, hm.emptyNode())
	}
	seq := sequence(nodes...)
	if seq == nil {
		return nil
	}
	m := newMapping()
	m.set("Items", seq)
	return m.emptyNode()
}

func securityHeadersConfigNode(s map[string]any) *yaml.Node {
	if s == nil {
		return nil
	}
	m := newMapping()
	if b := firstBlock(s, "content_security_policy"); b != nil {
		cm := newMapping()
		cm.set("ContentSecurityPolicy", scalar(str(b, "content_security_policy")))
		cm.set("Override", scalar(boolVal(b, "override")))
		m.set("ContentSecurityPolicy", cm.emptyNode())
	}
	if b := firstBlock(s, "content_type_options"); b != nil {
		cm := newMapping()
		cm.set("Override", scalar(boolVal(b, "override")))
		m.set("ContentTypeOptions", cm.emptyNode())
	}
	if b := firstBlock(s, "frame_options"); b != nil {
		fm := newMapping()
		fm.set("FrameOption", scalar(str(b, "frame_option")))
		fm.set("Override", scalar(boolVal(b, "override")))
		m.set("FrameOptions", fm.emptyNode())
	}
	if b := firstBlock(s, "referrer_policy"); b != nil {
		rm := newMapping()
		rm.set("ReferrerPolicy", scalar(str(b, "referrer_policy")))
		rm.set("Override", scalar(boolVal(b, "override")))
		m.set("ReferrerPolicy", rm.emptyNode())
	}
	if b := firstBlock(s, "strict_transport_security"); b != nil {
		sm := newMapping()
		sm.set("AccessControlMaxAgeSec", scalar(num(b, "access_control_max_age_sec")))
		if boolVal(b, "include_subdomains") {
			sm.set("IncludeSubdomains", scalar(true))
		}
		if boolVal(b, "preload") {
			sm.set("Preload", scalar(true))
		}
		sm.set("Override", scalar(boolVal(b, "override")))
		m.set("StrictTransportSecurity", sm.emptyNode())
	}
	if b := firstBlock(s, "xss_protection"); b != nil {
		xm := newMapping()
		xm.set("Protection", scalar(boolVal(b, "protection")))
		if boolVal(b, "mode_block") {
			xm.set("ModeBlock", scalar(true))
		}
		if u := str(b, "report_uri"); u != "" {
			xm.set("ReportUri", scalar(u))
		}
		xm.set("Override", scalar(boolVal(b, "override")))
		m.set("XSSProtection", xm.emptyNode())
	}
	return m.orNil()
}

func serverTimingConfigNode(s map[string]any) *yaml.Node {
	if s == nil {
		return nil
	}
	m := newMapping()
	m.set("Enabled", scalar(boolVal(s, "enabled")))
	if r := numPtr(s, "sampling_rate"); r != nil {
		m.set("SamplingRate", scalar(*r))
	}
	return m.orNil()
}
