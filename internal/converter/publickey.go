package converter

import "gopkg.in/yaml.v3"

// convertPublicKey renders aws_cloudfront_public_key as
// AWS::CloudFront::PublicKey.
func convertPublicKey(r plannedResource, _ map[string]any) (string, *yaml.Node) {
	v := r.Values

	cfg := newMapping()
	if n := str(v, "name"); n != "" {
		cfg.set("Name", scalar(n))
	}
	if c := str(v, "comment"); c != "" {
		cfg.set("Comment", scalar(c))
	}
	// CloudFormation requires CallerReference, but Terraform computes it after
	// apply, so it is absent from the plan. Synthesize a stable one from the key
	// name (falling back to the logical id) to keep the template valid and
	// deterministic across runs.
	cr := str(v, "name")
	if cr == "" {
		cr = logicalID(r.Name)
	}
	cfg.set("CallerReference", scalar(cr))
	cfg.set("EncodedKey", literalScalar(str(v, "encoded_key")))

	props := newMapping()
	props.set("PublicKeyConfig", cfg.emptyNode())
	return "AWS::CloudFront::PublicKey", props.emptyNode()
}
