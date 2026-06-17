package converter

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// stringSeq builds a sequence node from strings.
func stringSeq(ss []string) *yaml.Node {
	nodes := make([]*yaml.Node, 0, len(ss))
	for _, s := range ss {
		nodes = append(nodes, scalar(s))
	}
	return sequence(nodes...)
}

// configConst returns the constant_value configured for field, or nil.
func configConst(expr map[string]any, field string) any {
	node, ok := expr[field].(map[string]any)
	if !ok {
		return nil
	}
	return node["constant_value"]
}

// matchConfigBlock finds the configuration block whose keyField constant equals
// keyVal. Terraform reorders set-typed blocks, so blocks are matched by an
// identifying field rather than by position.
func matchConfigBlock(list []map[string]any, keyField, keyVal string) map[string]any {
	for _, e := range list {
		if s, ok := configConst(e, keyField).(string); ok && s == keyVal {
			return e
		}
	}
	return nil
}

// distinctCloudFrontRefs collapses a configuration references array into one
// reference per distinct CloudFront resource, preferring the entry that carries
// an attribute (e.g. ".arn") over the bare resource address.
func distinctCloudFrontRefs(refs []any) []*reference {
	byName := map[string]*reference{}
	var order []string
	for _, r := range refs {
		s, ok := r.(string)
		if !ok {
			continue
		}
		parts := strings.Split(s, ".")
		if len(parts) < 2 || !strings.HasPrefix(parts[0], "aws_cloudfront_") {
			continue
		}
		name := parts[1]
		if _, seen := byName[name]; !seen {
			order = append(order, name)
		}
		if len(parts) >= 3 {
			byName[name] = &reference{resourceType: parts[0], name: name, attr: strings.Join(parts[2:], ".")}
		} else if byName[name] == nil {
			byName[name] = &reference{resourceType: parts[0], name: name, attr: "arn"}
		}
	}
	out := make([]*reference, 0, len(order))
	for _, n := range order {
		out = append(out, byName[n])
	}
	return out
}
