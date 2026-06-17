package converter

import "gopkg.in/yaml.v3"

// convertOriginRequestPolicy renders aws_cloudfront_origin_request_policy as
// AWS::CloudFront::OriginRequestPolicy. The three *_config blocks share their
// shape with the cache policy, so they reuse policyConfigNode.
func convertOriginRequestPolicy(r plannedResource, _ map[string]any) (string, *yaml.Node) {
	v := r.Values

	cfg := newMapping()
	cfg.set("Name", scalar(str(v, "name")))
	if c := str(v, "comment"); c != "" {
		cfg.set("Comment", scalar(c))
	}
	cfg.set("CookiesConfig", policyConfigNode(firstBlock(v, "cookies_config"), "cookie_behavior", "CookieBehavior", "cookies", "Cookies"))
	cfg.set("HeadersConfig", policyConfigNode(firstBlock(v, "headers_config"), "header_behavior", "HeaderBehavior", "headers", "Headers"))
	cfg.set("QueryStringsConfig", policyConfigNode(firstBlock(v, "query_strings_config"), "query_string_behavior", "QueryStringBehavior", "query_strings", "QueryStrings"))

	props := newMapping()
	props.set("OriginRequestPolicyConfig", cfg.emptyNode())
	return "AWS::CloudFront::OriginRequestPolicy", props.emptyNode()
}
