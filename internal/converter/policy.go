package converter

import "gopkg.in/yaml.v3"

// policyConfigNode renders one of the {behavior, items} sub-blocks shared by the
// cache and origin-request policies — cookies_config, headers_config, and
// query_strings_config. In CloudFormation the allowlist is a flat list of
// strings (Cookies / Headers / QueryStrings), unlike the response-headers
// policy, which nests it under Items.
//
// behaviorAttr is the Terraform behavior attribute ("cookie_behavior"),
// cfnBehavior/cfnList are the CloudFormation key names, and itemsBlock is the
// nested block that carries the set ("cookies"). The block is omitted entirely
// when absent.
func policyConfigNode(block map[string]any, behaviorAttr, cfnBehavior, itemsBlock, cfnList string) *yaml.Node {
	if block == nil {
		return nil
	}
	m := newMapping()
	m.set(cfnBehavior, scalar(str(block, behaviorAttr)))
	if ib := firstBlock(block, itemsBlock); ib != nil {
		if items := strList(ib, "items"); len(items) > 0 {
			m.set(cfnList, stringSeq(items))
		}
	}
	return m.emptyNode()
}
