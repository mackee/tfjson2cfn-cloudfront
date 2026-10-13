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
	byAddress := map[string]*reference{}
	var order []string
	collections := map[string]bool{}
	for _, raw := range refs {
		s, ok := raw.(string)
		if !ok {
			continue
		}
		ref := parseCloudFrontReference(s)
		if ref == nil {
			continue
		}
		address := ref.resourceType + "." + ref.name
		if _, seen := byAddress[address]; !seen {
			order = append(order, address)
		}
		if previous := byAddress[address]; previous == nil || (previous.attr == "" && ref.attr != "") {
			byAddress[address] = ref
		}
		if base, _, indexed := strings.Cut(ref.name, "["); indexed {
			collections[ref.resourceType+"."+base] = true
		}
	}
	out := make([]*reference, 0, len(order))
	for _, address := range order {
		ref := byAddress[address]
		// Terraform includes the collection dependency alongside an indexed
		// reference. It must not become a separate, nonexistent resource Ref.
		if ref.attr == "" && collections[address] {
			continue
		}
		if ref.attr == "" {
			ref.attr = "arn"
		}
		out = append(out, ref)
	}
	return out
}

// A single represented static block must correspond to the single planned
// block, even when its identifying expression is a variable/local. Never use
// positional matching when dynamic blocks or multiple behaviors are possible.
func matchBehaviorConfig(cfg, planned []map[string]any, index int) map[string]any {
	if index >= len(planned) {
		return nil
	}
	if match := matchConfigBlock(cfg, "path_pattern", str(planned[index], "path_pattern")); match != nil {
		return match
	}
	if len(cfg) == 1 && len(planned) == 1 && configConst(cfg[0], "path_pattern") == nil {
		return cfg[0]
	}
	return nil
}
