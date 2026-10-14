package converter

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
)

const functionFixture = "testdata/unknown_function_associations.json"
const distributionAddress = "aws_cloudfront_distribution.example"

func functionHints() map[string]map[string][]string {
	hints := map[string]map[string][]string{distributionAddress: {}}
	for index := range 2 {
		for association, name := range []string{"viewer", "response"} {
			path := "ordered_cache_behavior[" + strconv.Itoa(index) + "].function_association[" + strconv.Itoa(association) + "].function_arn"
			hints[distributionAddress][path] = []string{"aws_cloudfront_function." + name}
		}
	}
	return hints
}

func TestUnknownFunctionARNsResolveByExactAssociationPath(t *testing.T) {
	data, err := os.ReadFile(functionFixture)
	if err != nil {
		t.Fatal(err)
	}
	hints := functionHints()
	res, err := Convert(data, Options{Format: "json", References: hints})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(res.Output, &doc); err != nil {
		t.Fatal(err)
	}
	resources := doc["Resources"].(map[string]any)
	dc := resources["AwsCloudfrontDistributionExample"].(map[string]any)["Properties"].(map[string]any)["DistributionConfig"].(map[string]any)
	for index, raw := range append([]any{dc["DefaultCacheBehavior"]}, dc["CacheBehaviors"].([]any)...) {
		associations := raw.(map[string]any)["FunctionAssociations"].([]any)
		for association, item := range associations {
			name := "viewer"
			if association == 1 {
				name = "response"
			}
			want := "AwsCloudfrontFunction" + strings.ToUpper(name[:1]) + name[1:]
			got := item.(map[string]any)["FunctionARN"].(map[string]any)["Fn::GetAtt"].([]any)
			if got[0] != want || got[1] != "FunctionARN" || resources[want] == nil {
				t.Fatalf("behavior %d association %d: got %v, want %s.FunctionARN", index, association, got, want)
			}
		}
	}
}

func TestUnknownFunctionARNsFailClosed(t *testing.T) {
	data, err := os.ReadFile(functionFixture)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, path, target, want string
	}{
		{"missing", "ordered_cache_behavior[1].function_association[1].function_arn", "", "unresolved function ARN"},
		{"wrong type", "ordered_cache_behavior[1].function_association[1].function_arn", distributionAddress, "must be a managed aws_cloudfront_function"},
		{"wrong index", "ordered_cache_behavior[2].function_association[1].function_arn", "aws_cloudfront_function.response", "unused reference hint"},
		{"wrong association", "ordered_cache_behavior[1].function_association[2].function_arn", "aws_cloudfront_function.response", "unused reference hint"},
		{"empty target list", "ordered_cache_behavior[1].function_association[1].function_arn", "empty", "requires exactly one"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hints := functionHints()
			switch tc.target {
			case "":
				delete(hints[distributionAddress], tc.path)
			case "empty":
				hints[distributionAddress][tc.path] = []string{}
			default:
				hints[distributionAddress][tc.path] = []string{tc.target}
			}
			_, err := Convert(data, Options{References: hints})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

func TestKnownFunctionARNPreserved(t *testing.T) {
	data, err := os.ReadFile(functionFixture)
	if err != nil {
		t.Fatal(err)
	}
	var plan map[string]any
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	resources := plan["planned_values"].(map[string]any)["root_module"].(map[string]any)["resources"].([]any)
	distribution := resources[2].(map[string]any)["values"].(map[string]any)
	defaultBehavior := distribution["default_cache_behavior"].([]any)[0].(map[string]any)
	defaultBehavior["function_association"].([]any)[0].(map[string]any)["function_arn"] = "arn:aws:cloudfront::123456789012:function/external"
	modified, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Convert(modified, Options{Format: "json", References: functionHints()})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res.Output), "arn:aws:cloudfront::123456789012:function/external") {
		t.Fatal("known function ARN was replaced")
	}
	hints := functionHints()
	hints[distributionAddress]["default_cache_behavior[0].function_association[0].function_arn"] = []string{"aws_cloudfront_function.viewer"}
	if _, err := Convert(modified, Options{References: hints}); err == nil || !strings.Contains(err.Error(), "unused reference hint") {
		t.Fatalf("hint for known ARN accepted: %v", err)
	}
}

func TestMalformedFunctionAssociationUnknownsFail(t *testing.T) {
	data, err := os.ReadFile(functionFixture)
	if err != nil {
		t.Fatal(err)
	}
	for _, malformed := range []any{true, []any{true}, []any{map[string]any{"function_arn": true}, map[string]any{"function_arn": true}, map[string]any{"function_arn": true}}} {
		var plan map[string]any
		if err := json.Unmarshal(data, &plan); err != nil {
			t.Fatal(err)
		}
		unknown := plan["resource_changes"].([]any)[0].(map[string]any)["change"].(map[string]any)["after_unknown"].(map[string]any)
		unknown["ordered_cache_behavior"].([]any)[0].(map[string]any)["function_association"] = malformed
		modified, err := json.Marshal(plan)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Convert(modified, Options{References: functionHints()}); err == nil {
			t.Fatalf("malformed function association unknown accepted: %v", malformed)
		}
	}
}

func TestFunctionARNRequiresManagedFunctionReference(t *testing.T) {
	data, err := os.ReadFile(functionFixture)
	if err != nil {
		t.Fatal(err)
	}
	var plan map[string]any
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	config := plan["configuration"].(map[string]any)["root_module"].(map[string]any)["resources"].([]any)[0].(map[string]any)["expressions"].(map[string]any)
	association := config["default_cache_behavior"].([]any)[0].(map[string]any)["function_association"].([]any)[0].(map[string]any)
	association["function_arn"] = map[string]any{"references": []any{"aws_cloudfront_distribution.example.arn"}}
	modified, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Convert(modified, Options{References: functionHints()}); err == nil || !strings.Contains(err.Error(), "unresolved function ARN") {
		t.Fatalf("invalid inferred target accepted: %v", err)
	}
}

func TestMalformedPlannedFunctionAssociationFails(t *testing.T) {
	data, err := os.ReadFile(functionFixture)
	if err != nil {
		t.Fatal(err)
	}
	for _, malformed := range []any{true, []any{true}} {
		var plan map[string]any
		if err := json.Unmarshal(data, &plan); err != nil {
			t.Fatal(err)
		}
		resources := plan["planned_values"].(map[string]any)["root_module"].(map[string]any)["resources"].([]any)
		behavior := resources[2].(map[string]any)["values"].(map[string]any)["ordered_cache_behavior"].([]any)[0].(map[string]any)
		behavior["function_association"] = malformed
		modified, err := json.Marshal(plan)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Convert(modified, Options{References: functionHints()}); err == nil || !strings.Contains(err.Error(), "invalid ordered_cache_behavior[0].function_association block") {
			t.Fatalf("malformed planned association accepted: %v", err)
		}
	}
}
