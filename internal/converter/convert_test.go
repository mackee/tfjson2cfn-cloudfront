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
