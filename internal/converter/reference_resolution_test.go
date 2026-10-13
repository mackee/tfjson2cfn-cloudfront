package converter

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestKnownPolicyAndOACIDsTakePrecedenceOverDependencies(t *testing.T) {
	for _, spec := range securityFields {
		if spec.terraform == "trusted_key_groups" {
			continue
		}
		t.Run(spec.terraform, func(t *testing.T) {
			cfg := map[string]any{spec.terraform: map[string]any{"references": []any{spec.resourceType + ".dependency.id", spec.resourceType + ".dependency", "var.use_managed"}}}
			base := []plannedResource{
				{Mode: "managed", Type: spec.resourceType, Name: "dependency", Values: map[string]any{"id": "dependency-id"}},
				{Mode: "managed", Type: spec.resourceType, Name: "selected", Values: map[string]any{"id": "selected-id"}},
			}
			for _, tc := range []struct {
				name, value, tag, want string
				ambiguous              bool
			}{
				{"external", "external-id", "!!str", "external-id", false},
				{"managed selected", "selected-id", "!Ref", logicalID(spec.resourceType, "selected"), false},
				{"ambiguous", "selected-id", "!!str", "selected-id", true},
				{"unknown fallback", "", "!Ref", logicalID(spec.resourceType, "dependency"), false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					planned := map[string]any{spec.terraform: tc.value}
					var node *yaml.Node
					if spec.terraform == "origin_access_control_id" {
						node = originNode(planned, cfg)
					} else {
						node = newMapping().set(spec.property, policyField(planned, cfg, spec.terraform)).emptyNode()
					}
					resources := append([]plannedResource(nil), base...)
					if tc.ambiguous {
						resources = append(resources, plannedResource{Mode: "managed", Type: spec.resourceType, Name: "duplicate", Values: map[string]any{"id": "selected-id"}})
					}
					restorePolicyReferences(node, resources)
					got := mapValue(node, spec.property)
					if got == nil || got.Tag != tc.tag || got.Value != tc.want {
						t.Fatalf("got %#v, want %s %s", got, tc.tag, tc.want)
					}
				})
			}
		})
	}
}

func TestDistinctReferencesPreserveQuotedInstanceKeysAndResourceTypes(t *testing.T) {
	refs := []any{
		`local.signers`,
		`aws_cloudfront_key_group.signers["a.b"]`,
		`aws_cloudfront_key_group.signers["a.b"].id`,
		`aws_cloudfront_key_group.signers`,
		`aws_cloudfront_key_group.signers["a.b"].id`,
		`aws_cloudfront_key_group.signers["a\"].b"].id`,
		`aws_cloudfront_public_key.signers["a.b"].id`,
		`aws_cloudfront_public_key.signers["a.b"]`,
		`aws_cloudfront_public_key.signers`,
		`aws_cloudfront_key_group.signers[bad].id`,
	}
	got := distinctCloudFrontRefs(refs)
	want := []reference{
		{resourceType: "aws_cloudfront_key_group", name: `signers["a.b"]`, attr: "id"},
		{resourceType: "aws_cloudfront_key_group", name: `signers["a\"].b"]`, attr: "id"},
		{resourceType: "aws_cloudfront_public_key", name: `signers["a.b"]`, attr: "id"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d refs, want %d: %#v", len(got), len(want), got)
	}
	for i, ref := range got {
		if *ref != want[i] {
			t.Fatalf("reference %d = %#v, want %#v", i, *ref, want[i])
		}
	}
}

func TestIndexedListReferencesResolveToInstanceLogicalIDs(t *testing.T) {
	for _, key := range []string{`"a.b"`, `"a\"].b"`, `"a_b"`, `"日本語"`, `0`} {
		name := "signers[" + key + "]"
		for _, tc := range []struct {
			field, typ string
			render     func(map[string]any, map[string]any) *yaml.Node
		}{
			{"trusted_key_groups", "aws_cloudfront_key_group", trustedKeyGroups},
			{"items", "aws_cloudfront_public_key", keyGroupItems},
			{"key_value_store_associations", "aws_cloudfront_key_value_store", kvsAssociations},
		} {
			t.Run(tc.field+key, func(t *testing.T) {
				address := tc.typ + "." + name
				cfg := map[string]any{tc.field: map[string]any{"references": []any{address + ".id", address, tc.typ + ".signers"}}}
				if tc.field == "key_value_store_associations" {
					cfg[tc.field].(map[string]any)["references"] = []any{address + ".arn", address, tc.typ + ".signers"}
				}
				node := tc.render(nil, cfg)
				if node == nil || len(node.Content) != 1 {
					t.Fatalf("expected one instance reference: %#v", node)
				}
				got := node.Content[0]
				want := logicalID(tc.typ, name)
				if tc.field == "key_value_store_associations" {
					got = mapValue(got, "KeyValueStoreARN")
					if got.Tag != "!GetAtt" || got.Value != want+".Arn" {
						t.Fatalf("incorrect KVS reference: %#v", got)
					}
				} else if got.Tag != "!Ref" || got.Value != want {
					t.Fatalf("incorrect reference: %#v, want %s", got, want)
				}
			})
		}
	}
}
