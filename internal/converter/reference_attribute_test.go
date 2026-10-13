package converter

import "testing"

func TestIndependentNestedAttributeIndexKeepsResourceName(t *testing.T) {
	ref := parseReference([]any{`aws_cloudfront_distribution.example.origin[0].domain_name`})
	if ref == nil || ref.name != "example" || ref.attr != "origin[0].domain_name" {
		t.Fatalf("nested attribute mistaken for resource index: %#v", ref)
	}
}

func TestIndependentIndexedResourceAndNestedAttribute(t *testing.T) {
	ref := parseReference([]any{`aws_cloudfront_distribution.example["dev"].origin[0].domain_name`})
	if ref == nil || ref.name != `example["dev"]` || ref.attr != "origin[0].domain_name" {
		t.Fatalf("nested attribute swallowed into indexed resource name: %#v", ref)
	}
}

func TestIndexedReferenceQuotedDelimiterAndMalformedIndex(t *testing.T) {
	for _, key := range []string{`"a].b"`, `"a\"].b"`} {
		name := "example[" + key + "]"
		ref := parseReference([]any{"aws_cloudfront_distribution." + name + ".origin[0].domain_name"})
		if ref == nil || ref.name != name || ref.attr != "origin[0].domain_name" {
			t.Fatalf("quoted delimiter parsed incorrectly: %#v", ref)
		}
	}
	for _, input := range []string{`example["dev".id`, `example[bad].id`, `example["dev"]id`, `example[].id`} {
		if ref := parseReference([]any{"aws_cloudfront_distribution." + input}); ref != nil {
			t.Fatalf("malformed index accepted: %s: %#v", input, ref)
		}
	}
}
