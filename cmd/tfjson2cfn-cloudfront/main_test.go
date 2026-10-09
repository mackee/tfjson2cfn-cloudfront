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
