package converter

import "gopkg.in/yaml.v3"

// convertKeyGroup renders aws_cloudfront_key_group as AWS::CloudFront::KeyGroup.
// Its Items list the public keys in the group; those ids are known after apply,
// so they are recovered from the configuration section as !Ref.
func convertKeyGroup(r plannedResource, cfg map[string]any) (string, *yaml.Node) {
	v := r.Values

	kg := newMapping()
	kg.set("Name", scalar(str(v, "name")))
	if c := str(v, "comment"); c != "" {
		kg.set("Comment", scalar(c))
	}
	kg.set("Items", keyGroupItems(v, cfg))

	props := newMapping()
	props.set("KeyGroupConfig", kg.emptyNode())
	return "AWS::CloudFront::KeyGroup", props.emptyNode()
}

func keyGroupItems(v, cfg map[string]any) *yaml.Node {
	if node, ok := cfg["items"].(map[string]any); ok {
		if refs, ok := node["references"].([]any); ok {
			var nodes []*yaml.Node
			for _, ref := range distinctCloudFrontRefs(refs) {
				nodes = append(nodes, refNode(logicalID(ref.resourceType, ref.name)))
			}
			if len(nodes) > 0 {
				return sequence(nodes...)
			}
		}
	}
	if items := strList(v, "items"); len(items) > 0 {
		return stringSeq(items)
	}
	return nil
}
