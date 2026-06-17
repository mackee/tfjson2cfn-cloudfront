package converter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const examplesRoot = "../../examples"

// TestExamples converts each examples/<name>/plan.json fixture and checks the
// result is CloudFront-equivalent to the template.yaml that localfront ships.
//
// Examples are discovered automatically: drop in a directory with a plan.json
// and a template.yaml and it is picked up. Regenerate the plan.json fixtures
// from their Terraform with `go test ./internal/converter -update` (see
// regen_test.go).
func TestExamples(t *testing.T) {
	dirs := exampleDirs(t)
	if len(dirs) == 0 {
		t.Fatalf("no example fixtures discovered under %s", examplesRoot)
	}
	for _, dir := range dirs {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			planData := mustRead(t, filepath.Join(dir, "plan.json"))
			goldenData := mustRead(t, filepath.Join(dir, "template.yaml"))

			res, err := Convert(planData, Options{Format: "yaml"})
			if err != nil {
				t.Fatalf("Convert: %v", err)
			}

			got := canonOf(t, res.Output)
			want := canonOf(t, goldenData)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("converted template is not CloudFront-equivalent to golden\n=== generated ===\n%s\n=== golden (canonical) ===\n%s\n=== generated (canonical) ===\n%s",
					res.Output, pretty(want), pretty(got))
			}
		})
	}
}

// TestJSONOutputMatchesYAML checks the json format is semantically identical to
// the yaml format (intrinsics differ only in long vs short form).
func TestJSONOutputMatchesYAML(t *testing.T) {
	planData := mustRead(t, filepath.Join(examplesRoot, "functions", "plan.json"))
	jsonRes, err := Convert(planData, Options{Format: "json"})
	if err != nil {
		t.Fatalf("convert json: %v", err)
	}
	yamlRes, err := Convert(planData, Options{Format: "yaml"})
	if err != nil {
		t.Fatalf("convert yaml: %v", err)
	}
	if !reflect.DeepEqual(canonOf(t, jsonRes.Output), canonOf(t, yamlRes.Output)) {
		t.Errorf("json output is not equivalent to yaml output\njson:\n%s", jsonRes.Output)
	}
}

// TestLogicalIDsDisambiguateByType checks that resources of different kinds that
// share one Terraform local name get distinct logical IDs (derived from the
// resource type as well as the name) rather than colliding and overwriting one
// another, and that references to them resolve to the right per-type logical ID.
func TestLogicalIDsDisambiguateByType(t *testing.T) {
	const plan = `{
  "format_version": "1.2",
  "planned_values": {
    "root_module": {
      "resources": [
        {
          "address": "aws_cloudfront_origin_access_control.tools",
          "mode": "managed",
          "type": "aws_cloudfront_origin_access_control",
          "name": "tools",
          "values": {
            "name": "tools-oac",
            "origin_access_control_origin_type": "s3",
            "signing_behavior": "always",
            "signing_protocol": "sigv4"
          }
        },
        {
          "address": "aws_cloudfront_public_key.tools",
          "mode": "managed",
          "type": "aws_cloudfront_public_key",
          "name": "tools",
          "values": {"name": "tools-pubkey", "encoded_key": "KEY"}
        },
        {
          "address": "aws_cloudfront_key_group.tools",
          "mode": "managed",
          "type": "aws_cloudfront_key_group",
          "name": "tools",
          "values": {"name": "tools-kg", "items": null}
        }
      ]
    }
  },
  "configuration": {
    "root_module": {
      "resources": [
        {
          "address": "aws_cloudfront_key_group.tools",
          "type": "aws_cloudfront_key_group",
          "name": "tools",
          "expressions": {
            "items": {"references": ["aws_cloudfront_public_key.tools.id", "aws_cloudfront_public_key.tools"]}
          }
        }
      ]
    }
  }
}`

	res, err := Convert([]byte(plan), Options{Format: "json"})
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	for _, w := range res.Warnings {
		if strings.Contains(w, "collides") {
			t.Errorf("unexpected collision warning: %s", w)
		}
	}

	var doc struct {
		Resources map[string]struct {
			Type string `json:"Type"`
		} `json:"Resources"`
	}
	if err := json.Unmarshal(res.Output, &doc); err != nil {
		t.Fatalf("parse output: %v\n%s", err, res.Output)
	}
	want := map[string]string{
		"AwsCloudfrontOriginAccessControlTools": "AWS::CloudFront::OriginAccessControl",
		"AwsCloudfrontPublicKeyTools":           "AWS::CloudFront::PublicKey",
		"AwsCloudfrontKeyGroupTools":            "AWS::CloudFront::KeyGroup",
	}
	if len(doc.Resources) != len(want) {
		t.Fatalf("got %d resources, want %d: %s", len(doc.Resources), len(want), res.Output)
	}
	for id, typ := range want {
		got, ok := doc.Resources[id]
		if !ok {
			t.Errorf("missing logical ID %q in output:\n%s", id, res.Output)
			continue
		}
		if got.Type != typ {
			t.Errorf("logical ID %q has type %q, want %q", id, got.Type, typ)
		}
	}

	// The key group's Items must reference the public key by its per-type
	// logical ID, not a bare "Tools".
	if !strings.Contains(string(res.Output), `"Ref": "AwsCloudfrontPublicKeyTools"`) {
		t.Errorf("key group Items did not resolve to the public key's logical ID:\n%s", res.Output)
	}
}

// exampleDirs returns the example directories that have both a plan.json and a
// template.yaml, sorted for stable test ordering.
func exampleDirs(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(examplesRoot)
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(examplesRoot, e.Name())
		if fileExists(filepath.Join(dir, "plan.json")) && fileExists(filepath.Join(dir, "template.yaml")) {
			dirs = append(dirs, dir)
		}
	}
	sort.Strings(dirs)
	return dirs
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// unorderedKeys names CloudFormation properties whose list order is not
// semantically meaningful. Terraform reorders set-typed lists, so these are
// compared as multisets. CacheBehaviors is deliberately absent: its order
// encodes precedence.
var unorderedKeys = map[string]bool{
	"Origins":                    true,
	"Aliases":                    true,
	"AllowedMethods":             true,
	"CachedMethods":              true,
	"CustomErrorResponses":       true,
	"FunctionAssociations":       true,
	"LambdaFunctionAssociations": true,
	"KeyValueStoreAssociations":  true,
	"TrustedKeyGroups":           true,
	"TrustedSigners":             true,
	"OriginSSLProtocols":         true,
	"OriginCustomHeaders":        true,
	"Locations":                  true,
	// Policy allowlists are Terraform sets, reordered relative to source.
	"Cookies":      true,
	"Headers":      true,
	"QueryStrings": true,
	"Items":        true,
}

func canonOf(t *testing.T, data []byte) any {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse yaml: %v\n%s", err, data)
	}
	return canon(&doc, "")
}

// canon converts a yaml.Node into a comparable Go value: mappings become maps
// (order-independent), set-typed sequences are sorted, intrinsics keep their
// tag, and numbers are normalized to float64.
func canon(n *yaml.Node, key string) any {
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil
		}
		return canon(n.Content[0], "")
	case yaml.MappingNode:
		m := make(map[string]any, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i].Value
			m[k] = canon(n.Content[i+1], k)
		}
		// Normalize CloudFormation JSON long-form intrinsics to the same
		// sentinel the YAML short-form (!Ref / !GetAtt) produces, so json and
		// yaml output compare equal.
		if len(m) == 1 {
			if ref, ok := m["Ref"].(string); ok {
				return "!Ref " + ref
			}
			if ga, ok := m["Fn::GetAtt"].([]any); ok && len(ga) == 2 {
				return fmt.Sprintf("!GetAtt %v.%v", ga[0], ga[1])
			}
		}
		return m
	case yaml.SequenceNode:
		arr := make([]any, len(n.Content))
		for i, c := range n.Content {
			arr[i] = canon(c, "")
		}
		if unorderedKeys[key] {
			sort.Slice(arr, func(i, j int) bool {
				return fmt.Sprintf("%v", arr[i]) < fmt.Sprintf("%v", arr[j])
			})
		}
		return arr
	default: // scalar
		switch n.Tag {
		case "!Ref":
			return "!Ref " + n.Value
		case "!GetAtt":
			return "!GetAtt " + n.Value
		default:
			var v any
			_ = n.Decode(&v)
			switch x := v.(type) {
			case int:
				return float64(x)
			case int64:
				return float64(x)
			case uint64:
				return float64(x)
			}
			return v
		}
	}
}

func pretty(v any) string {
	out, _ := yaml.Marshal(v)
	return string(out)
}
