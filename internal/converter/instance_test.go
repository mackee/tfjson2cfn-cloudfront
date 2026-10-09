package converter

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestForEachDistributionsAndResolvedPolicies(t *testing.T) {
	data, err := os.ReadFile("testdata/for_each_policies.json")
	if err != nil {
		t.Fatal(err)
	}
	res, err := Convert(data, Options{Format: "json"})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Resources map[string]struct {
			Properties struct {
				DistributionConfig struct {
					Aliases              []string
					DefaultCacheBehavior struct{ ResponseHeadersPolicyId map[string]string }
				}
			}
		}
	}
	if err := json.Unmarshal(res.Output, &doc); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"Keydev", "dev"}, {"Keyprod", "prod"}} {
		id := "AwsCloudfrontDistributionGuest" + pair[0]
		r, ok := doc.Resources[id]
		if !ok {
			t.Errorf("missing distribution %s", id)
			continue
		}
		want := "AwsCloudfrontResponseHeadersPolicy" + pascalCase("tools_"+pair[1])
		if got := r.Properties.DistributionConfig.DefaultCacheBehavior.ResponseHeadersPolicyId["Ref"]; got != want {
			t.Errorf("%s policy Ref = %q, want %q", id, got, want)
		}
		if len(r.Properties.DistributionConfig.Aliases) != 1 {
			t.Errorf("%s aliases not retained", id)
		}
	}
	if len(doc.Resources) != 4 {
		t.Errorf("resource count = %d, want 4", len(doc.Resources))
	}
}

func TestIndexedLogicalIDsAndReferences(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{`guest["dev"]`, "AwsCloudfrontDistributionGuestKeydev"},
		{`guest[0]`, "AwsCloudfrontDistributionGuestIndex0"},
		{`guest[1]`, "AwsCloudfrontDistributionGuestIndex1"},
	} {
		if got := logicalID("aws_cloudfront_distribution", tc.name); got != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, got, tc.want)
		}
	}
	a := logicalID("aws_cloudfront_distribution", `guest["a-b"]`)
	b := logicalID("aws_cloudfront_distribution", `guest["a_b"]`)
	if a == b || !regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]+$`).MatchString(a) {
		t.Fatal("arbitrary keys lost uniqueness or validity")
	}
	ref := parseReference([]any{`aws_cloudfront_response_headers_policy.headers["prod"].id`})
	if ref == nil || ref.resolve().Value != "AwsCloudfrontResponseHeadersPolicyHeadersKeyprod" {
		t.Fatal("indexed symbolic Ref not resolved")
	}
	ref = parseReference([]any{`aws_cloudfront_response_headers_policy.headers["a.b"].id`})
	if ref == nil || ref.resolve().Value != logicalID("aws_cloudfront_response_headers_policy", `headers["a.b"]`) {
		t.Fatal("dot in key misparsed")
	}
}

func TestLogicalIDCollisionFailsInsteadOfDroppingResource(t *testing.T) {
	data, err := os.ReadFile("testdata/for_each_policies.json")
	if err != nil {
		t.Fatal(err)
	}
	var p plan
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatal(err)
	}
	p.PlannedValues.RootModule.Resources[3].Address = p.PlannedValues.RootModule.Resources[1].Address
	data, err = json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Convert(data, Options{Format: "json"}); err == nil || !strings.Contains(err.Error(), "collides") {
		t.Fatalf("ambiguous logical IDs should fail: %v", err)
	}
}

func TestResolvedPolicyReferencesAreTypedAndUnambiguous(t *testing.T) {
	resources := []plannedResource{{Mode: "managed", Type: "aws_cloudfront_response_headers_policy", Name: "headers", Values: map[string]any{"id": "owned"}}}
	for _, tc := range []struct {
		field, value string
		wantRef      bool
	}{
		{"ResponseHeadersPolicyId", "owned", true},
		{"CachePolicyId", "owned", false},
		{"ResponseHeadersPolicyId", "aws-managed", false},
	} {
		n := newMapping()
		n.set(tc.field, scalar(tc.value))
		node := n.emptyNode()
		restorePolicyReferences(node, resources)
		got := node.Content[1]
		if tc.wantRef {
			if got.Tag != "!Ref" || got.Value != "AwsCloudfrontResponseHeadersPolicyHeaders" {
				t.Fatal("owned Ref missing")
			}
		} else if got.Value != tc.value {
			t.Fatal("unrelated policy literal changed")
		}
	}
	resources = append(resources, plannedResource{Mode: "managed", Type: resources[0].Type, Name: "other", Values: resources[0].Values})
	n := newMapping()
	n.set("ResponseHeadersPolicyId", scalar("owned"))
	node := n.emptyNode()
	restorePolicyReferences(node, resources)
	if node.Content[1].Value != "owned" {
		t.Fatal("ambiguous policy resolved arbitrarily")
	}
}

func TestStringInstanceKeysPreserveCaseAndType(t *testing.T) {
	seen := map[string]string{}
	for _, key := range []string{`"dev"`, `"Dev"`, `"DEV"`, `"0"`, `0`, `"Index0"`, `"a-b"`, `"a_b"`} {
		name := "guest[" + key + "]"
		id := logicalID("aws_cloudfront_distribution", name)
		if prev, ok := seen[id]; ok {
			t.Fatalf("%s and %s collide", prev, key)
		}
		seen[id] = key
		ref := parseReference([]any{"aws_cloudfront_distribution." + name + ".id"})
		if ref == nil || ref.resolve().Value != id {
			t.Fatalf("reference disagrees for %s", name)
		}
	}
	data, err := os.ReadFile("testdata/for_each_policies.json")
	if err != nil {
		t.Fatal(err)
	}
	var p plan
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatal(err)
	}
	for i := range p.PlannedValues.RootModule.Resources {
		r := &p.PlannedValues.RootModule.Resources[i]
		r.Address = strings.ReplaceAll(r.Address, `["prod"]`, `["Dev"]`)
	}
	data, err = json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Convert(data, Options{Format: "json"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(result.Output), "AwsCloudfrontDistributionGuestKeydev") || !strings.Contains(string(result.Output), "AwsCloudfrontDistributionGuestKeyDev") {
		t.Fatal("case-distinct distributions lost")
	}
}
