package converter

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

var securityFields = []struct{ terraform, property, resourceType string }{
	{"cache_policy_id", "CachePolicyId", "aws_cloudfront_cache_policy"},
	{"origin_request_policy_id", "OriginRequestPolicyId", "aws_cloudfront_origin_request_policy"},
	{"response_headers_policy_id", "ResponseHeadersPolicyId", "aws_cloudfront_response_headers_policy"},
	{"trusted_key_groups", "TrustedKeyGroups", "aws_cloudfront_key_group"},
	{"origin_access_control_id", "OriginAccessControlId", "aws_cloudfront_origin_access_control"},
}

func mapValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func putValue(n *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			n.Content[i+1] = value
			return
		}
	}
	n.Content = append(n.Content, scalar(key), value)
}

func containsUnknown(v any) bool {
	switch v := v.(type) {
	case bool:
		return v
	case []any:
		for _, e := range v {
			if containsUnknown(e) {
				return true
			}
		}
	case map[string]any:
		for _, e := range v {
			if containsUnknown(e) {
				return true
			}
		}
	}
	return false
}

func validateReferenceHints(p *plan, hints map[string]map[string][]string) error {
	for address := range hints {
		found := false
		for _, r := range p.PlannedValues.RootModule.Resources {
			if r.Address == address && r.Mode == "managed" && r.Type == "aws_cloudfront_distribution" {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("reference hints: distribution %s is not managed in this plan", address)
		}
	}
	return nil
}

// The plan's dependency list is not an expression AST. Never infer an instance
// relationship from a bare collection reference and each.key.
func resolveDistributionSecurity(p *plan, r plannedResource, props *yaml.Node, hints map[string][]string) error {
	var unknown map[string]any
	for _, change := range p.ResourceChanges {
		if change.Address == r.Address {
			unknown = change.Change.AfterUnknown
			break
		}
	}
	used := map[string]bool{}
	dc := mapValue(props, "DistributionConfig")
	for _, block := range []struct{ terraform, property string }{
		{"default_cache_behavior", "DefaultCacheBehavior"}, {"ordered_cache_behavior", "CacheBehaviors"}, {"origin", "Origins"},
	} {
		raw := unknown[block.terraform]
		list, ok := raw.([]any)
		if !ok {
			if containsUnknown(raw) {
				return fmt.Errorf("%s: unresolved %s block", r.Address, block.terraform)
			}
			continue
		}
		for index, entry := range list {
			fields, ok := entry.(map[string]any)
			if !ok {
				if containsUnknown(entry) {
					return fmt.Errorf("%s: unresolved %s[%d] block", r.Address, block.terraform, index)
				}
				continue
			}
			output := mapValue(dc, block.property)
			if block.terraform != "default_cache_behavior" {
				if output != nil && output.Kind == yaml.SequenceNode && index < len(output.Content) {
					output = output.Content[index]
				} else {
					output = nil
				}
			} else if index != 0 {
				output = nil
			}
			for _, spec := range securityFields {
				field := spec.terraform
				if !containsUnknown(fields[field]) {
					continue
				}
				path := fmt.Sprintf("%s[%d].%s", block.terraform, index, field)
				// The provider computes an omitted optional trusted_key_groups
				// field as unknown. A represented static block proves omission;
				// an absent dynamic block does not.
				if field == "trusted_key_groups" && fields[field] == true {
					cfg := p.configFor(r.Address)
					var bc map[string]any
					if block.terraform == "default_cache_behavior" {
						bc = firstBlock(cfg, block.terraform)
					}
					if block.terraform == "ordered_cache_behavior" {
						planned := blocks(r.Values, block.terraform)
						if index < len(planned) {
							bc = matchBehaviorConfig(blocks(cfg, block.terraform), planned, index)
						}
					}
					if bc != nil {
						if _, configured := bc[field]; !configured {
							continue
						}
					}
				}
				if output == nil {
					return fmt.Errorf("%s: unresolved %s block", r.Address, path)
				}
				if targets, exists := hints[path]; exists {
					used[path] = true
					if len(targets) == 0 || (field != "trusted_key_groups" && len(targets) != 1) {
						requirement := "exactly one"
						if field == "trusted_key_groups" {
							requirement = "one or more"
						}
						return fmt.Errorf("%s: %s requires %s managed reference(s)", r.Address, path, requirement)
					}
					var nodes []*yaml.Node
					seen := map[string]bool{}
					for _, target := range targets {
						var match *plannedResource
						for i := range p.PlannedValues.RootModule.Resources {
							candidate := &p.PlannedValues.RootModule.Resources[i]
							if candidate.Address == target && candidate.Mode == "managed" && candidate.Type == spec.resourceType {
								match = candidate
								break
							}
						}
						if match == nil || seen[target] {
							return fmt.Errorf("%s: %s target %s must be a distinct managed %s in the plan", r.Address, path, target, spec.resourceType)
						}
						seen[target] = true
						nodes = append(nodes, refNode(resourceLogicalID(*match)))
					}
					node := nodes[0]
					if field == "trusted_key_groups" {
						node = sequence(nodes...)
					}
					putValue(output, spec.property, node)
					if field == "origin_access_control_id" && mapValue(output, "S3OriginConfig") == nil && mapValue(output, "CustomOriginConfig") == nil {
						putValue(output, "S3OriginConfig", newMapping().set("OriginAccessIdentity", scalar("")).emptyNode())
					}
				}
				node := mapValue(output, spec.property)
				if field == "trusted_key_groups" {
					planned := blocks(r.Values, block.terraform)
					if index < len(planned) && !retainsKnownGroups(node, planned[index][field], p.PlannedValues.RootModule.Resources) {
						return fmt.Errorf("%s: %s would discard known trusted key groups", r.Address, path)
					}
				}
				if !validManagedReference(node, spec.resourceType, p.PlannedValues.RootModule.Resources) {
					return fmt.Errorf("%s: unresolved security reference %s; supply --references with exact resource addresses", r.Address, path)
				}
			}
		}
	}
	for path := range hints {
		if !used[path] {
			return fmt.Errorf("%s: unused reference hint %s (only unknown security fields accept hints)", r.Address, path)
		}
	}
	return nil
}

func validManagedReference(n *yaml.Node, typ string, resources []plannedResource) bool {
	if n == nil {
		return false
	}
	if n.Kind == yaml.SequenceNode {
		if len(n.Content) == 0 {
			return false
		}
		for _, child := range n.Content {
			if !validManagedReference(child, typ, resources) {
				return false
			}
		}
		return true
	}
	if n.Tag != "!Ref" || strings.TrimSpace(n.Value) == "" {
		return false
	}
	matches := 0
	for _, r := range resources {
		if r.Mode == "managed" && r.Type == typ && resourceLogicalID(r) == n.Value {
			matches++
		}
	}
	return matches == 1
}

// A hint or recovered dependency must not discard known elements of a partially
// unknown signer list. External elements cannot be represented by managed hints.
func retainsKnownGroups(n *yaml.Node, value any, resources []plannedResource) bool {
	values, ok := value.([]any)
	if !ok {
		return true
	}
	if n == nil || n.Kind != yaml.SequenceNode || len(n.Content) < len(values) {
		return false
	}
	for _, raw := range values {
		id, known := raw.(string)
		if !known {
			continue
		}
		found := false
		for _, child := range n.Content {
			if child.Tag == "!!str" && child.Value == id {
				found = true
			}
			if child.Tag == "!Ref" {
				for _, r := range resources {
					if r.Mode == "managed" && r.Type == "aws_cloudfront_key_group" && str(r.Values, "id") == id && resourceLogicalID(r) == child.Value {
						found = true
					}
				}
			}
		}
		if !found {
			return false
		}
	}
	return true
}
