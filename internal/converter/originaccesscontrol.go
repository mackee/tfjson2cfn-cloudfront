package converter

import "gopkg.in/yaml.v3"

// convertOriginAccessControl renders aws_cloudfront_origin_access_control as
// AWS::CloudFront::OriginAccessControl.
func convertOriginAccessControl(r plannedResource, _ map[string]any) (string, *yaml.Node) {
	v := r.Values

	cfg := newMapping()
	cfg.set("Name", scalar(str(v, "name")))
	if d := str(v, "description"); d != "" {
		cfg.set("Description", scalar(d))
	}
	cfg.set("OriginAccessControlOriginType", scalar(str(v, "origin_access_control_origin_type")))
	cfg.set("SigningBehavior", scalar(str(v, "signing_behavior")))
	cfg.set("SigningProtocol", scalar(str(v, "signing_protocol")))

	props := newMapping()
	props.set("OriginAccessControlConfig", cfg.emptyNode())
	return "AWS::CloudFront::OriginAccessControl", props.emptyNode()
}
