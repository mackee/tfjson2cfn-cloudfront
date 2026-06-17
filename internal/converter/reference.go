package converter

import (
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
		parts := strings.Split(s, ".")
		if len(parts) < 3 || !strings.HasPrefix(parts[0], "aws_cloudfront_") {
			continue
		}
		// parts: [type, name, attr...]; attr is the remainder joined.
		best = &reference{
			resourceType: parts[0],
			name:         parts[1],
			attr:         strings.Join(parts[2:], "."),
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
	return pascalCase(resourceType + "_" + name)
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
