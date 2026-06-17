package converter

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"gopkg.in/yaml.v3"
)

// examples are converted from their plan.json and compared against the golden
// CloudFormation template copied from localfront.
var examples = []string{"spa-hosting", "static-and-api", "functions", "cors-security"}

func TestGolden(t *testing.T) {
	for _, ex := range examples {
		t.Run(ex, func(t *testing.T) {
			dir := filepath.Join("..", "..", "examples", ex)
			planData, err := os.ReadFile(filepath.Join(dir, "plan.json"))
			if err != nil {
				t.Fatal(err)
			}
			goldenData, err := os.ReadFile(filepath.Join(dir, "template.yaml"))
			if err != nil {
				t.Fatal(err)
			}

			res, err := Convert(planData, Options{Format: "yaml"})
			if err != nil {
				t.Fatalf("Convert: %v", err)
			}

			got := canonOf(t, res.Output)
			want := canonOf(t, goldenData)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("converted template does not match golden\n=== generated YAML ===\n%s\n=== generated (canonical) ===\n%s\n=== golden (canonical) ===\n%s",
					res.Output, pretty(got), pretty(want))
			}
		})
	}
}

func TestJSONOutputParses(t *testing.T) {
	dir := filepath.Join("..", "..", "examples", "functions")
	planData, err := os.ReadFile(filepath.Join(dir, "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Convert(planData, Options{Format: "json"})
	if err != nil {
		t.Fatalf("Convert json: %v", err)
	}
	// JSON output must be semantically equal to the YAML output.
	yamlRes, _ := Convert(planData, Options{Format: "yaml"})
	if !reflect.DeepEqual(canonOf(t, res.Output), canonOf(t, yamlRes.Output)) {
		t.Errorf("json output differs from yaml output\njson:\n%s", res.Output)
	}
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
