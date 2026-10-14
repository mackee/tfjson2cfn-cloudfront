package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReferenceFileAndFailureDoesNotWriteTemplate(t *testing.T) {
	dir := t.TempDir()
	output := filepath.Join(dir, "template.json")
	c := cli{Input: "../../internal/converter/testdata/dynamic_security/plan.json", Output: output, Format: "json"}
	if err := run(c); err == nil {
		t.Fatal("unknown security reference succeeded")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("failed conversion wrote output")
	}
	hints := map[string]map[string][]string{}
	for _, key := range []string{"alpha", "beta"} {
		address := `aws_cloudfront_distribution.sites["` + key + `"]`
		target := `aws_cloudfront_response_headers_policy.headers["` + key + `"]`
		hints[address] = map[string][]string{"default_cache_behavior[0].response_headers_policy_id": {target}}
		for _, index := range []string{"0", "1", "2", "3", "4"} {
			hints[address]["ordered_cache_behavior["+index+"].response_headers_policy_id"] = []string{target}
		}
	}
	raw, err := json.Marshal(hints)
	if err != nil {
		t.Fatal(err)
	}
	c.References = filepath.Join(dir, "references.json")
	if err := os.WriteFile(c.References, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(c); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.References, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(c); err == nil {
		t.Fatal("invalid hints accepted")
	}
}

func TestUnknownFunctionARNFailureDoesNotClobberOutput(t *testing.T) {
	dir := t.TempDir()
	output := filepath.Join(dir, "template.json")
	const existing = "existing template\n"
	if err := os.WriteFile(output, []byte(existing), 0600); err != nil {
		t.Fatal(err)
	}
	c := cli{Input: "../../internal/converter/testdata/unknown_function_associations.json", Output: output, Format: "json"}
	if err := run(c); err == nil {
		t.Fatal("unresolved function ARN accepted")
	}
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != existing {
		t.Fatalf("failed conversion changed output: %q", got)
	}
}

func TestMalformedFunctionAssociationDoesNotClobberOutput(t *testing.T) {
	data, err := os.ReadFile("../../internal/converter/testdata/unknown_function_associations.json")
	if err != nil {
		t.Fatal(err)
	}
	var plan map[string]any
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	resources := plan["planned_values"].(map[string]any)["root_module"].(map[string]any)["resources"].([]any)
	behavior := resources[2].(map[string]any)["values"].(map[string]any)["default_cache_behavior"].([]any)[0].(map[string]any)
	behavior["function_association"] = true
	data, err = json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "plan.json")
	output := filepath.Join(dir, "template.json")
	if err := os.WriteFile(input, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, []byte("existing\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(cli{Input: input, Output: output, Format: "json"}); err == nil {
		t.Fatal("malformed association accepted")
	}
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "existing\n" {
		t.Fatalf("failed conversion changed output: %q", got)
	}
}
