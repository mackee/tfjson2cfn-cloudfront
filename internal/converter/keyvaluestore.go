package converter

import "gopkg.in/yaml.v3"

// convertKeyValueStore renders aws_cloudfront_key_value_store as
// AWS::CloudFront::KeyValueStore.
func convertKeyValueStore(r plannedResource, _ map[string]any) (string, *yaml.Node) {
	v := r.Values
	props := newMapping()
	props.set("Name", scalar(str(v, "name")))
	if c := str(v, "comment"); c != "" {
		props.set("Comment", scalar(c))
	}
	return "AWS::CloudFront::KeyValueStore", props.emptyNode()
}
