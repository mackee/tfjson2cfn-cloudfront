package converter

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

func securityFixture(t *testing.T) ([]byte, map[string]map[string][]string) {
	t.Helper()
	data, err := os.ReadFile("testdata/unknown_instance_security.json")
	if err != nil {
		t.Fatal(err)
	}
	hints := map[string]map[string][]string{}
	for _, key := range []string{"alpha", "beta"} {
		address := fmt.Sprintf("aws_cloudfront_distribution.sites[%q]", key)
		target := fmt.Sprintf("aws_cloudfront_response_headers_policy.headers[%q]", key)
		hints[address] = map[string][]string{"default_cache_behavior[0].response_headers_policy_id": {target}}
		for i := range 5 {
			hints[address][fmt.Sprintf("ordered_cache_behavior[%d].response_headers_policy_id", i)] = []string{target}
		}
	}
	return data, hints
}

func TestUnknownInstanceSecurityWithExplicitReferences(t *testing.T) {
	data, hints := securityFixture(t)
	res, err := Convert(data, Options{Format: "json", References: hints})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(res.Output, &doc); err != nil {
		t.Fatal(err)
	}
	resources := doc["Resources"].(map[string]any)
	for _, key := range []string{"Keyalpha", "Keybeta"} {
		dc := resources["AwsCloudfrontDistributionSites"+key].(map[string]any)["Properties"].(map[string]any)["DistributionConfig"].(map[string]any)
		behaviors := append([]any{dc["DefaultCacheBehavior"]}, dc["CacheBehaviors"].([]any)...)
		for i, raw := range behaviors {
			b := raw.(map[string]any)
			ref := b["ResponseHeadersPolicyId"].(map[string]any)["Ref"].(string)
			if ref != "AwsCloudfrontResponseHeadersPolicyHeaders"+key || resources[ref] == nil {
				t.Fatalf("wrong instance policy: %s", ref)
			}
			groups, signed := b["TrustedKeyGroups"]
			if signed != (i < 2) {
				t.Fatalf("behavior %d signed = %v", i, signed)
			}
			if signed && groups.([]any)[0].(map[string]any)["Ref"] != "AwsCloudfrontKeyGroupSigners" {
				t.Fatal("managed signer reference lost")
			}
		}
		origin := dc["Origins"].([]any)[0].(map[string]any)
		if origin["OriginAccessControlId"].(map[string]any)["Ref"] != "AwsCloudfrontOriginAccessControlStorage" {
			t.Fatal("managed OAC reference lost")
		}
	}
}

func TestUnknownSecurityFailsWithoutGuessing(t *testing.T) {
	data, hints := securityFixture(t)
	for _, tc := range []struct {
		name   string
		mutate func(map[string]map[string][]string)
		want   string
	}{
		{"no hints", func(h map[string]map[string][]string) { clear(h) }, "unresolved security reference"},
		{"missing behavior", func(h map[string]map[string][]string) {
			delete(h[`aws_cloudfront_distribution.sites["alpha"]`], "ordered_cache_behavior[4].response_headers_policy_id")
		}, "unresolved security reference"},
		{"bare collection", func(h map[string]map[string][]string) {
			h[`aws_cloudfront_distribution.sites["alpha"]`]["default_cache_behavior[0].response_headers_policy_id"] = []string{"aws_cloudfront_response_headers_policy.headers"}
		}, "must be a distinct managed"},
		{"wrong type", func(h map[string]map[string][]string) {
			h[`aws_cloudfront_distribution.sites["alpha"]`]["default_cache_behavior[0].response_headers_policy_id"] = []string{"aws_cloudfront_key_group.signers"}
		}, "must be a distinct managed"},
		{"unused", func(h map[string]map[string][]string) {
			h[`aws_cloudfront_distribution.sites["alpha"]`]["ordered_cache_behavior[1].trusted_key_groups"] = []string{"aws_cloudfront_key_group.signers"}
		}, "unused reference hint"},
		{"unknown distribution", func(h map[string]map[string][]string) {
			h["aws_cloudfront_distribution.missing"] = map[string][]string{}
		}, "not managed in this plan"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, h := securityFixture(t)
			tc.mutate(h)
			_, err := Convert(data, Options{References: h})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %s, got %v", tc.want, err)
			}
		})
	}
	// A default policy reference never authorizes propagation to dynamic blocks.
	delete(hints[`aws_cloudfront_distribution.sites["alpha"]`], "ordered_cache_behavior[0].response_headers_policy_id")
	if _, err := Convert(data, Options{References: hints}); err == nil {
		t.Fatal("dynamic policy guessed from default")
	}
}

func TestKnownSignerAndOACIDs(t *testing.T) {
	for _, typ := range []string{"aws_cloudfront_key_group", "aws_cloudfront_origin_access_control"} {
		for _, ambiguous := range []bool{false, true} {
			resources := []plannedResource{{Mode: "managed", Type: typ, Name: "owned", Values: map[string]any{"id": "owned-id"}}}
			if ambiguous {
				resources = append(resources, plannedResource{Mode: "managed", Type: typ, Name: "other", Values: map[string]any{"id": "owned-id"}})
			}
			for _, id := range []string{"owned-id", "external-id"} {
				m := newMapping()
				if typ == "aws_cloudfront_key_group" {
					m.set("TrustedKeyGroups", stringSeq([]string{id}))
				} else {
					m.set("OriginAccessControlId", scalar(id))
				}
				n := m.emptyNode()
				restorePolicyReferences(n, resources)
				got := n.Content[1]
				if typ == "aws_cloudfront_key_group" {
					got = got.Content[0]
				}
				if id == "owned-id" && !ambiguous {
					if got.Tag != "!Ref" {
						t.Fatal("unique managed reference missing")
					}
				} else if got.Tag != "!!str" || got.Value != id {
					t.Fatal("literal changed")
				}
			}
		}
	}
	cfg := map[string]any{"trusted_key_groups": map[string]any{"references": []any{"aws_cloudfront_key_group.owned.id"}}}
	if trustedKeyGroups(map[string]any{"trusted_key_groups": []any{}}, cfg) != nil {
		t.Fatal("unsigned behavior gained signer")
	}
}

func TestUnknownSignerReferencesAndProviderDefaults(t *testing.T) {
	data, hints := securityFixture(t)
	var p map[string]any
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatal(err)
	}
	r := p["planned_values"].(map[string]any)["root_module"].(map[string]any)["resources"].([]any)[1].(map[string]any)
	r["values"].(map[string]any)["default_cache_behavior"].([]any)[0].(map[string]any)["trusted_key_groups"] = []any{nil}
	changes := p["resource_changes"].([]any)
	changes[0].(map[string]any)["change"].(map[string]any)["after_unknown"].(map[string]any)["default_cache_behavior"].([]any)[0].(map[string]any)["trusted_key_groups"] = []any{true}
	cfg := p["configuration"].(map[string]any)["root_module"].(map[string]any)["resources"].([]any)[0].(map[string]any)["expressions"].(map[string]any)["default_cache_behavior"].([]any)[0].(map[string]any)
	cfg["trusted_key_groups"] = map[string]any{"references": []any{"aws_cloudfront_key_group.signers.id", "aws_cloudfront_key_group.signers"}}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Convert(data, Options{References: hints}); err != nil {
		t.Fatal(err)
	}
	cfg["trusted_key_groups"] = map[string]any{"references": []any{"local.default_behavior"}}
	data, err = json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Convert(data, Options{References: hints}); err == nil {
		t.Fatal("unknown local signer silently omitted")
	}
	hints[`aws_cloudfront_distribution.sites["alpha"]`]["default_cache_behavior[0].trusted_key_groups"] = []string{"aws_cloudfront_key_group.signers"}
	if _, err := Convert(data, Options{References: hints}); err != nil {
		t.Fatal(err)
	}
}

// This fixture comes from terraform plan -out followed by terraform show -json,
// using only dummy credentials and example.test domains (see adjacent main.tf).
func TestFormalDynamicPlanUnknownPolicies(t *testing.T) {
	data, err := os.ReadFile("testdata/dynamic_security/plan.json")
	if err != nil {
		t.Fatal(err)
	}
	_, hints := securityFixture(t)
	if _, err := Convert(data, Options{References: hints}); err != nil {
		t.Fatal(err)
	}
	res, err := Convert(data, Options{Format: "json", References: hints})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(res.Output, &doc); err != nil {
		t.Fatal(err)
	}
	resources := doc["Resources"].(map[string]any)
	for _, key := range []string{"Keyalpha", "Keybeta"} {
		dc := resources["AwsCloudfrontDistributionSites"+key].(map[string]any)["Properties"].(map[string]any)["DistributionConfig"].(map[string]any)
		behaviors := append([]any{dc["DefaultCacheBehavior"]}, dc["CacheBehaviors"].([]any)...)
		if len(behaviors) != 6 {
			t.Fatal("behavior count changed")
		}
		for i, raw := range behaviors {
			b := raw.(map[string]any)
			if b["ResponseHeadersPolicyId"].(map[string]any)["Ref"] != "AwsCloudfrontResponseHeadersPolicyHeaders"+key {
				t.Fatal("wrong policy instance")
			}
			groups, ok := b["TrustedKeyGroups"]
			if ok != (i < 2) {
				t.Fatal("signature requirement changed")
			}
			if ok && groups.([]any)[0] != "external-signing-id" {
				t.Fatal("external signer ID changed")
			}
		}
	}
	if _, err := Convert(data, Options{}); err == nil {
		t.Fatal("unknown policies silently dropped")
	}
}

func TestPartiallyUnknownSignerListPreservesKnownElements(t *testing.T) {
	resources := []plannedResource{{Mode: "managed", Type: "aws_cloudfront_key_group", Name: "signers", Values: map[string]any{"id": "owned"}}}
	n := sequence(refNode("AwsCloudfrontKeyGroupSigners"))
	if retainsKnownGroups(n, []any{"external", nil}, resources) {
		t.Fatal("external signer silently discarded")
	}
	if retainsKnownGroups(n, []any{"owned", nil}, resources) {
		t.Fatal("unknown signer silently discarded")
	}
	if !retainsKnownGroups(n, []any{"owned"}, resources) {
		t.Fatal("managed signer not recognized")
	}
}

func TestVariableStaticPathDoesNotRequireUnconfiguredSigners(t *testing.T) {
	for _, tc := range []struct {
		name               string
		configs, behaviors int
		wantError          bool
	}{
		{"single static variable path", 1, 1, false},
		{"absent dynamic configuration", 0, 1, true},
		{"static and dynamic ambiguous", 1, 2, true},
		{"multiple static ambiguous", 2, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, _ := securityFixture(t)
			p, err := parsePlan(data)
			if err != nil {
				t.Fatal(err)
			}
			var r plannedResource
			for _, candidate := range p.PlannedValues.RootModule.Resources {
				if candidate.Type == "aws_cloudfront_distribution" {
					r = candidate
					break
				}
			}
			r.Values["default_cache_behavior"] = []any{}
			r.Values["ordered_cache_behavior"] = []any{}
			configs := []any{}
			unknown := []any{}
			for i := range tc.behaviors {
				r.Values["ordered_cache_behavior"] = append(r.Values["ordered_cache_behavior"].([]any), map[string]any{"path_pattern": fmt.Sprintf("/path%d", i), "trusted_key_groups": nil})
				unknown = append(unknown, map[string]any{"trusted_key_groups": true})
			}
			for range tc.configs {
				configs = append(configs, map[string]any{"path_pattern": map[string]any{"references": []any{"var.path"}}})
			}
			p.Configuration.RootModule.Resources[0].Expressions = map[string]any{"ordered_cache_behavior": configs}
			p.ResourceChanges[0].Change.AfterUnknown = map[string]any{"ordered_cache_behavior": unknown}
			_, props := convertDistribution(r, p.configFor(r.Address))
			err = resolveDistributionSecurity(p, r, props, nil)
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v, wantError=%v", err, tc.wantError)
			}
		})
	}
}

func TestOACHintSynthesizesS3OriginConfig(t *testing.T) {
	for _, custom := range []bool{false, true} {
		data, _ := securityFixture(t)
		p, err := parsePlan(data)
		if err != nil {
			t.Fatal(err)
		}
		var r plannedResource
		for _, candidate := range p.PlannedValues.RootModule.Resources {
			if candidate.Type == "aws_cloudfront_distribution" {
				r = candidate
				break
			}
		}
		origin := blocks(r.Values, "origin")[0]
		delete(origin, "origin_access_control_id")
		if custom {
			origin["custom_origin_config"] = []any{map[string]any{"origin_protocol_policy": "https-only"}}
		}
		p.ResourceChanges[0].Change.AfterUnknown = map[string]any{"origin": []any{map[string]any{"origin_access_control_id": true}}}
		_, props := convertDistribution(r, p.configFor(r.Address))
		hints := map[string][]string{"origin[0].origin_access_control_id": {"aws_cloudfront_origin_access_control.storage"}}
		if err := resolveDistributionSecurity(p, r, props, hints); err != nil {
			t.Fatal(err)
		}
		out := mapValue(mapValue(props, "DistributionConfig"), "Origins").Content[0]
		if mapValue(out, "OriginAccessControlId").Tag != "!Ref" {
			t.Fatal("OAC reference missing")
		}
		s3 := mapValue(out, "S3OriginConfig")
		if custom {
			if s3 != nil || mapValue(out, "CustomOriginConfig") == nil {
				t.Fatal("custom origin discriminator changed")
			}
		} else {
			if s3 == nil || mapValue(s3, "OriginAccessIdentity") == nil || mapValue(s3, "OriginAccessIdentity").Value != "" {
				t.Fatal("empty S3 discriminator missing")
			}
		}
	}
}
