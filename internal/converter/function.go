package converter

import "gopkg.in/yaml.v3"

// convertFunction renders aws_cloudfront_function as AWS::CloudFront::Function.
// Terraform's flat runtime/comment/associations attributes are nested under
// FunctionConfig, and code becomes FunctionCode.
func convertFunction(r plannedResource, cfg map[string]any) (string, *yaml.Node) {
	v := r.Values

	props := newMapping()
	props.set("Name", scalar(str(v, "name")))

	fc := newMapping()
	fc.set("Runtime", scalar(str(v, "runtime")))
	fc.set("Comment", scalar(str(v, "comment"))) // required by CloudFormation
	fc.set("KeyValueStoreAssociations", kvsAssociations(v, cfg))
	props.set("FunctionConfig", fc.emptyNode())

	props.set("FunctionCode", literalScalar(str(v, "code")))

	// CloudFormation AutoPublish defaults to true; only emit when explicitly off.
	if pub, ok := v["publish"].(bool); ok && !pub {
		props.set("AutoPublish", scalar(false))
	}

	return "AWS::CloudFront::Function", props.emptyNode()
}

// kvsAssociations builds FunctionConfig.KeyValueStoreAssociations. The ARN is a
// known-after-apply reference to the KeyValueStore resource, so it comes from
// the configuration section and resolves to a GetAtt.
func kvsAssociations(v, cfg map[string]any) *yaml.Node {
	var items []*yaml.Node
	if lit := strList(v, "key_value_store_associations"); len(lit) > 0 {
		for _, arn := range lit {
			im := newMapping()
			im.set("KeyValueStoreARN", scalar(arn))
			items = append(items, im.emptyNode())
		}
		return sequence(items...)
	}
	if node, ok := cfg["key_value_store_associations"].(map[string]any); ok {
		if refs, ok := node["references"].([]any); ok {
			for _, ref := range distinctCloudFrontRefs(refs) {
				im := newMapping()
				im.set("KeyValueStoreARN", ref.resolve())
				items = append(items, im.emptyNode())
			}
			return sequence(items...)
		}
	}
	return nil
}
