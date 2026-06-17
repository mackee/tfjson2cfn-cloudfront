package converter

import "gopkg.in/yaml.v3"

// convertCachePolicy renders aws_cloudfront_cache_policy as
// AWS::CloudFront::CachePolicy. Terraform's flat body is nested under
// CachePolicyConfig, and the parameters_in_cache_key_and_forwarded_to_origin
// block keeps its (long) CloudFormation name.
func convertCachePolicy(r plannedResource, _ map[string]any) (string, *yaml.Node) {
	v := r.Values

	cfg := newMapping()
	cfg.set("Name", scalar(str(v, "name")))
	if c := str(v, "comment"); c != "" {
		cfg.set("Comment", scalar(c))
	}
	// MinTTL is required by CloudFormation; DefaultTTL/MaxTTL are emitted when set.
	cfg.set("MinTTL", scalar(num(v, "min_ttl")))
	if t := numPtr(v, "max_ttl"); t != nil {
		cfg.set("MaxTTL", scalar(*t))
	}
	if t := numPtr(v, "default_ttl"); t != nil {
		cfg.set("DefaultTTL", scalar(*t))
	}
	if p := firstBlock(v, "parameters_in_cache_key_and_forwarded_to_origin"); p != nil {
		cfg.set("ParametersInCacheKeyAndForwardedToOrigin", cacheKeyParamsNode(p))
	}

	props := newMapping()
	props.set("CachePolicyConfig", cfg.emptyNode())
	return "AWS::CloudFront::CachePolicy", props.emptyNode()
}

func cacheKeyParamsNode(p map[string]any) *yaml.Node {
	m := newMapping()
	// EnableAcceptEncodingGzip is required by CloudFormation; Brotli is optional.
	m.set("EnableAcceptEncodingGzip", scalar(boolVal(p, "enable_accept_encoding_gzip")))
	if boolVal(p, "enable_accept_encoding_brotli") {
		m.set("EnableAcceptEncodingBrotli", scalar(true))
	}
	m.set("CookiesConfig", policyConfigNode(firstBlock(p, "cookies_config"), "cookie_behavior", "CookieBehavior", "cookies", "Cookies"))
	m.set("HeadersConfig", policyConfigNode(firstBlock(p, "headers_config"), "header_behavior", "HeaderBehavior", "headers", "Headers"))
	m.set("QueryStringsConfig", policyConfigNode(firstBlock(p, "query_strings_config"), "query_string_behavior", "QueryStringBehavior", "query_strings", "QueryStrings"))
	return m.emptyNode()
}
