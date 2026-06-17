// Package converter turns `terraform show -json` plan output into a
// CloudFront-only CloudFormation template, suitable for feeding to localfront.
package converter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Options controls conversion output.
type Options struct {
	// Format is "yaml" (default) or "json".
	Format string
}

// Result is the converted template plus any non-fatal warnings (e.g. resource
// types that were skipped).
type Result struct {
	Output   []byte
	Warnings []string
}

// resourceConverter renders one Terraform resource into a CloudFormation
// (Type, Properties) pair. cfg is the resource's configuration expressions,
// used to recover cross-resource references.
type resourceConverter func(r plannedResource, cfg map[string]any) (cfnType string, properties *yaml.Node)

var converters = map[string]resourceConverter{
	"aws_cloudfront_distribution":            convertDistribution,
	"aws_cloudfront_function":                convertFunction,
	"aws_cloudfront_key_value_store":         convertKeyValueStore,
	"aws_cloudfront_cache_policy":            convertCachePolicy,
	"aws_cloudfront_origin_request_policy":   convertOriginRequestPolicy,
	"aws_cloudfront_response_headers_policy": convertResponseHeadersPolicy,
	"aws_cloudfront_public_key":              convertPublicKey,
	"aws_cloudfront_key_group":               convertKeyGroup,
	"aws_cloudfront_origin_access_control":   convertOriginAccessControl,
}

// Convert reads `terraform show -json` output and returns a CloudFormation
// template containing the AWS::CloudFront::* resources it found.
func Convert(data []byte, opts Options) (*Result, error) {
	p, err := parsePlan(data)
	if err != nil {
		return nil, err
	}

	res := &Result{}
	if len(p.PlannedValues.RootModule.ChildModules) > 0 || len(p.Configuration.RootModule.ChildModules) > 0 {
		res.Warnings = append(res.Warnings, "child modules are not yet traversed; only root-module resources are converted")
	}

	resources := newMapping()
	seen := map[string]string{}
	count := 0
	for _, r := range p.PlannedValues.RootModule.Resources {
		if r.Mode != "managed" {
			continue
		}
		if !strings.HasPrefix(r.Type, "aws_cloudfront_") {
			continue // silently ignore non-CloudFront resources
		}
		conv, ok := converters[r.Type]
		if !ok {
			res.Warnings = append(res.Warnings, fmt.Sprintf("skipping %s: %s is not supported yet", r.Address, r.Type))
			continue
		}
		id := logicalID(r.Name)
		if prev, dup := seen[id]; dup {
			res.Warnings = append(res.Warnings, fmt.Sprintf("logical ID %q for %s collides with %s; the earlier resource is overwritten", id, r.Address, prev))
		}
		seen[id] = r.Address
		cfnType, props := conv(r, p.configFor(r.Address))
		resources.set(id, resourceNode(cfnType, props))
		count++
	}

	if count == 0 {
		return nil, fmt.Errorf("no supported AWS::CloudFront::* resources found in plan")
	}

	root := newMapping()
	root.set("Resources", resources.emptyNode())

	switch opts.Format {
	case "", "yaml", "yml":
		res.Output, err = marshalYAML(root.emptyNode())
	case "json":
		res.Output, err = marshalJSON(root.emptyNode())
	default:
		return nil, fmt.Errorf("unknown output format %q (want yaml or json)", opts.Format)
	}
	if err != nil {
		return nil, err
	}
	return res, nil
}

// resourceNode wraps properties as { Type: <cfnType>, Properties: {...} }.
func resourceNode(cfnType string, properties *yaml.Node) *yaml.Node {
	m := newMapping()
	m.set("Type", scalar(cfnType))
	m.set("Properties", properties)
	return m.emptyNode()
}

func marshalYAML(node *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(node); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func marshalJSON(node *yaml.Node) ([]byte, error) {
	v := nodeToJSON(node)
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// nodeToJSON converts the output node tree to a generic value, rendering
// intrinsics in CloudFormation's JSON long form ({"Ref":...}, {"Fn::GetAtt":...}).
func nodeToJSON(n *yaml.Node) any {
	switch n.Kind {
	case yaml.DocumentNode:
		return nodeToJSON(n.Content[0])
	case yaml.MappingNode:
		m := make(map[string]any, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			m[n.Content[i].Value] = nodeToJSON(n.Content[i+1])
		}
		return m
	case yaml.SequenceNode:
		arr := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			arr = append(arr, nodeToJSON(c))
		}
		return arr
	default: // scalar
		switch n.Tag {
		case "!Ref":
			return map[string]any{"Ref": n.Value}
		case "!GetAtt":
			logical, attr, _ := strings.Cut(n.Value, ".")
			return map[string]any{"Fn::GetAtt": []any{logical, attr}}
		default:
			var v any
			_ = n.Decode(&v)
			return v
		}
	}
}
