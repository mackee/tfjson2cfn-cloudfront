package converter

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// reference is a parsed Terraform cross-resource reference such as
// "aws_cloudfront_function.router.arn".
type reference struct {
	resourceType string // aws_cloudfront_function
	name         string // router
	attr         string // arn
}

// parseReference picks the most specific CloudFront reference out of a
// configuration `references` array. Terraform lists both the attribute form
// ("aws_cloudfront_function.router.arn") and the bare resource
// ("aws_cloudfront_function.router"); we want the former.
func parseReference(refs []any) *reference {
	var best *reference
	for _, r := range refs {
		s, ok := r.(string)
		if !ok {
			continue
		}
		resourceType, rest, ok := strings.Cut(s, ".")
		if !ok || !strings.HasPrefix(resourceType, "aws_cloudfront_") {
			continue
		}
		end := strings.IndexByte(rest, '.')
		if bracket := strings.IndexByte(rest, '['); bracket >= 0 && (end < 0 || bracket < end) {
			// Decode only the resource's JSON index. Attribute paths may contain
			// their own indexes; quoted keys may contain dots, brackets or escapes.
			decoder := json.NewDecoder(strings.NewReader(rest[bracket+1:]))
			var index any
			if decoder.Decode(&index) != nil {
				continue
			}
			close := bracket + 1 + int(decoder.InputOffset())
			if close+1 >= len(rest) || rest[close] != ']' || rest[close+1] != '.' {
				continue
			}
			end = close + 1
		}
		if end < 1 || end+1 >= len(rest) {
			continue
		}
		best = &reference{
			resourceType: resourceType,
			name:         rest[:end],
			attr:         rest[end+1:],
		}
		break
	}
	return best
}

// refFromExpr returns the reference configured for field within a configuration
// expression map, or nil when the field is a constant or absent.
func refFromExpr(expr map[string]any, field string) *reference {
	if expr == nil {
		return nil
	}
	node, ok := expr[field].(map[string]any)
	if !ok {
		return nil
	}
	refs, ok := node["references"].([]any)
	if !ok {
		return nil
	}
	return parseReference(refs)
}

// resolve turns a reference into the CloudFormation intrinsic that yields the
// same value. CloudFront resources expose their id via Ref and their ARN via a
// GetAtt, with a couple of resource-specific attribute names.
func (r *reference) resolve() *yaml.Node {
	id := logicalID(r.resourceType, r.name)
	switch r.resourceType {
	case "aws_cloudfront_function":
		switch r.attr {
		case "arn":
			return getAttNode(id, "FunctionARN")
		case "name":
			return refNode(id)
		default:
			return getAttNode(id, "FunctionARN")
		}
	case "aws_cloudfront_key_value_store":
		switch r.attr {
		case "id":
			return getAttNode(id, "Id")
		default: // arn
			return getAttNode(id, "Arn")
		}
	case "aws_cloudfront_cache_policy",
		"aws_cloudfront_origin_request_policy",
		"aws_cloudfront_response_headers_policy",
		"aws_cloudfront_key_group",
		"aws_cloudfront_public_key",
		"aws_cloudfront_origin_access_control":
		// Ref returns the id for all of these.
		return refNode(id)
	default:
		return refNode(id)
	}
}

// logicalID derives a CloudFormation logical ID from a Terraform resource's
// full address (type + local name), PascalCased on underscores:
// aws_cloudfront_distribution.spa_distribution -> "AwsCloudfrontDistributionSpaDistribution".
// Including the resource type keeps logical IDs unique across resource kinds
// that share the same local name (e.g. an origin access control, a key group
// and a public key all named "tools" would otherwise collide).
func logicalID(resourceType, name string) string {
	base, index, indexed := strings.Cut(name, "[")
	if !indexed {
		return pascalCase(resourceType + "_" + name)
	}
	index = strings.TrimSuffix(index, "]")
	var value any
	if err := json.Unmarshal([]byte(index), &value); err == nil {
		switch value := value.(type) {
		case string:
			if value != "" && asciiAlphanumeric(value) {
				return pascalCase(resourceType+"_"+base) + "Key" + value
			}
		case float64:
			if value >= 0 && value == float64(int64(value)) {
				return pascalCase(resourceType+"_"+base) + "Index" + strconv.FormatInt(int64(value), 10)
			}
		}
	}
	// Arbitrary for_each keys still need stable, alphanumeric logical IDs.
	digest := sha256.Sum256([]byte(index))
	return pascalCase(resourceType+"_"+base) + fmt.Sprintf("Hash%x", digest[:8])
}

func asciiAlphanumeric(s string) bool {
	for _, c := range []byte(s) {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

func resourceLogicalID(r plannedResource) string {
	name := r.Name
	if strings.HasPrefix(r.Address, r.Type+"."+r.Name+"[") {
		name = strings.TrimPrefix(r.Address, r.Type+".")
	}
	return logicalID(r.Type, name)
}

// A local/each expression may hide a resource reference in configuration.
// Restore known IDs only when the expected resource type matches uniquely.
// External and ambiguous IDs remain literal values.
func restorePolicyReferences(n *yaml.Node, resources []plannedResource) {
	if n == nil {
		return
	}
	policyTypes := map[string]string{"CachePolicyId": "aws_cloudfront_cache_policy", "OriginRequestPolicyId": "aws_cloudfront_origin_request_policy", "ResponseHeadersPolicyId": "aws_cloudfront_response_headers_policy", "OriginAccessControlId": "aws_cloudfront_origin_access_control"}
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			value := n.Content[i+1]
			if n.Content[i].Value == "TrustedKeyGroups" && value.Kind == yaml.SequenceNode {
				for j, item := range value.Content {
					value.Content[j] = restoreIDReference(item, resources, "aws_cloudfront_key_group")
				}
			}
			if typ, ok := policyTypes[n.Content[i].Value]; ok && value.Kind == yaml.ScalarNode && value.Tag == "!!str" {
				n.Content[i+1] = restoreIDReference(value, resources, typ)
			}
		}
	}
	for _, child := range n.Content {
		restorePolicyReferences(child, resources)
	}
}

// restoreIDReference replaces only a nonempty, uniquely matched managed ID.
func restoreIDReference(value *yaml.Node, resources []plannedResource, typ string) *yaml.Node {
	if value.Kind != yaml.ScalarNode || value.Tag != "!!str" || value.Value == "" {
		return value
	}
	var match *plannedResource
	for i := range resources {
		r := &resources[i]
		if r.Mode == "managed" && r.Type == typ && str(r.Values, "id") == value.Value {
			if match != nil {
				return value
			}
			match = r
		}
	}
	if match != nil {
		return refNode(resourceLogicalID(*match))
	}
	return value
}

// pascalCase PascalCases an identifier on underscores: "spa_distribution" ->
// "SpaDistribution", "feature_flags" -> "FeatureFlags".
func pascalCase(s string) string {
	parts := strings.Split(s, "_")
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]))
		b.WriteString(p[1:])
	}
	return b.String()
}
