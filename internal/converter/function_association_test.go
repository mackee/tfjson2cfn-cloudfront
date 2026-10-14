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

func modifiedFunctionFixture(t *testing.T, mutate func(map[string]any)) []byte {
	t.Helper()
	data, err := os.ReadFile(functionFixture)
	if err != nil {
		t.Fatal(err)
	}
	var plan map[string]any
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	mutate(plan)
	data, err = json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func functionDistributionValues(plan map[string]any) map[string]any {
	resources := plan["planned_values"].(map[string]any)["root_module"].(map[string]any)["resources"].([]any)
	return resources[2].(map[string]any)["values"].(map[string]any)
}

func functionUnknowns(plan map[string]any) map[string]any {
	return plan["resource_changes"].([]any)[0].(map[string]any)["change"].(map[string]any)["after_unknown"].(map[string]any)
}

func functionHints() map[string]map[string][]string {
	hints := map[string]map[string][]string{distributionAddress: {}}
	for index := range 2 {
		for event, name := range map[string]string{"viewer-request": "viewer", "viewer-response": "response"} {
			path := "ordered_cache_behavior[" + strconv.Itoa(index) + "].function_association[" + event + "].function_arn"
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
		{"missing", "ordered_cache_behavior[1].function_association[viewer-response].function_arn", "", "unresolved function ARN"},
		{"wrong type", "ordered_cache_behavior[1].function_association[viewer-response].function_arn", distributionAddress, "must be a managed aws_cloudfront_function"},
		{"wrong index", "ordered_cache_behavior[2].function_association[viewer-response].function_arn", "aws_cloudfront_function.response", "unused reference hint"},
		{"wrong association", "ordered_cache_behavior[1].function_association[2].function_arn", "aws_cloudfront_function.response", "unused reference hint"},
		{"empty target list", "ordered_cache_behavior[1].function_association[viewer-response].function_arn", "empty", "requires exactly one"},
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
	config := plan["configuration"].(map[string]any)["root_module"].(map[string]any)["resources"].([]any)[0].(map[string]any)["expressions"].(map[string]any)
	config["default_cache_behavior"].([]any)[0].(map[string]any)["function_association"].([]any)[0].(map[string]any)["function_arn"] = map[string]any{"constant_value": "arn:aws:cloudfront::123456789012:function/external"}
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
	hints[distributionAddress]["default_cache_behavior[0].function_association[viewer-request].function_arn"] = []string{"aws_cloudfront_function.viewer"}
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

func TestKnownManagedFunctionARNRestored(t *testing.T) {
	known := "arn:aws:cloudfront::123456789012:function/viewer"
	data := modifiedFunctionFixture(t, func(plan map[string]any) {
		resources := plan["planned_values"].(map[string]any)["root_module"].(map[string]any)["resources"].([]any)
		resources[0].(map[string]any)["values"].(map[string]any)["arn"] = known
		functionDistributionValues(plan)["default_cache_behavior"].([]any)[0].(map[string]any)["function_association"].([]any)[0].(map[string]any)["function_arn"] = known
	})
	res, err := Convert(data, Options{Format: "json", References: functionHints()})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(res.Output, &doc); err != nil {
		t.Fatal(err)
	}
	resources := doc["Resources"].(map[string]any)
	behavior := resources["AwsCloudfrontDistributionExample"].(map[string]any)["Properties"].(map[string]any)["DistributionConfig"].(map[string]any)["DefaultCacheBehavior"].(map[string]any)
	arn := behavior["FunctionAssociations"].([]any)[0].(map[string]any)["FunctionARN"].(map[string]any)["Fn::GetAtt"].([]any)
	if arn[0] != "AwsCloudfrontFunctionViewer" || arn[1] != "FunctionARN" {
		t.Fatalf("known managed ARN was not restored: %v", arn)
	}
}

func TestFunctionHintConflictingWithConfigurationFails(t *testing.T) {
	data, err := os.ReadFile(functionFixture)
	if err != nil {
		t.Fatal(err)
	}
	hints := functionHints()
	hints[distributionAddress]["default_cache_behavior[0].function_association[viewer-request].function_arn"] = []string{"aws_cloudfront_function.response"}
	if _, err := Convert(data, Options{References: hints}); err == nil || !strings.Contains(err.Error(), "conflicting function hint") {
		t.Fatalf("conflicting hint accepted: %v", err)
	}
}

func TestFunctionHintRequiresExactUnknownMarker(t *testing.T) {
	for _, mutate := range []func(map[string]any){
		func(plan map[string]any) { delete(functionUnknowns(plan), "ordered_cache_behavior") },
		func(plan map[string]any) {
			unknown := functionUnknowns(plan)["ordered_cache_behavior"].([]any)[0].(map[string]any)["function_association"].([]any)[0].(map[string]any)
			delete(unknown, "function_arn")
		},
		func(plan map[string]any) { delete(plan, "resource_changes") },
	} {
		data := modifiedFunctionFixture(t, mutate)
		if _, err := Convert(data, Options{References: functionHints()}); err == nil || !strings.Contains(err.Error(), "not marked unknown") {
			t.Fatalf("hint without marker accepted: %v", err)
		}
	}
}

func TestFunctionHintsFollowEventsWhenSetOrderChanges(t *testing.T) {
	data := modifiedFunctionFixture(t, func(plan map[string]any) {
		behaviors := functionDistributionValues(plan)["ordered_cache_behavior"].([]any)
		unknown := functionUnknowns(plan)["ordered_cache_behavior"].([]any)
		for i := range behaviors {
			items := behaviors[i].(map[string]any)["function_association"].([]any)
			items[0], items[1] = items[1], items[0]
			markers := unknown[i].(map[string]any)["function_association"].([]any)
			markers[0], markers[1] = markers[1], markers[0]
		}
	})
	res, err := Convert(data, Options{Format: "json", References: functionHints()})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(res.Output, &doc); err != nil {
		t.Fatal(err)
	}
	resources := doc["Resources"].(map[string]any)
	behaviors := resources["AwsCloudfrontDistributionExample"].(map[string]any)["Properties"].(map[string]any)["DistributionConfig"].(map[string]any)["CacheBehaviors"].([]any)
	for _, raw := range behaviors {
		for _, item := range raw.(map[string]any)["FunctionAssociations"].([]any) {
			association := item.(map[string]any)
			want := "AwsCloudfrontFunctionViewer"
			if association["EventType"] == "viewer-response" {
				want = "AwsCloudfrontFunctionResponse"
			}
			got := association["FunctionARN"].(map[string]any)["Fn::GetAtt"].([]any)[0]
			if got != want {
				t.Fatalf("event %s resolved to %v, want %s", association["EventType"], got, want)
			}
		}
	}
}

func TestDuplicateFunctionEventsFail(t *testing.T) {
	data := modifiedFunctionFixture(t, func(plan map[string]any) {
		items := functionDistributionValues(plan)["ordered_cache_behavior"].([]any)[0].(map[string]any)["function_association"].([]any)
		items[1].(map[string]any)["event_type"] = "viewer-request"
	})
	if _, err := Convert(data, Options{References: functionHints()}); err == nil || !strings.Contains(err.Error(), "duplicate event type") {
		t.Fatalf("duplicate event accepted: %v", err)
	}
}

func TestFunctionModuleOutputRequiresEmittedTarget(t *testing.T) {
	data := modifiedFunctionFixture(t, func(plan map[string]any) {
		cfg := plan["configuration"].(map[string]any)["root_module"].(map[string]any)["resources"].([]any)[0].(map[string]any)["expressions"].(map[string]any)
		cfg["default_cache_behavior"].([]any)[0].(map[string]any)["function_association"].([]any)[0].(map[string]any)["function_arn"] = map[string]any{"references": []any{"module.edge.viewer_arn"}}
	})
	hints := functionHints()
	hints[distributionAddress]["default_cache_behavior[0].function_association[viewer-request].function_arn"] = []string{"module.edge.aws_cloudfront_function.viewer"}
	if _, err := Convert(data, Options{References: hints}); err == nil || !strings.Contains(err.Error(), "must be a managed aws_cloudfront_function") {
		t.Fatalf("non-emitted module target accepted: %v", err)
	}
	hints[distributionAddress]["default_cache_behavior[0].function_association[viewer-request].function_arn"] = []string{"aws_cloudfront_function.viewer"}
	if _, err := Convert(data, Options{References: hints}); err != nil {
		t.Fatalf("explicit emitted target rejected: %v", err)
	}
}

func TestKnownManagedFunctionConflictingWithConfigurationFails(t *testing.T) {
	data := modifiedFunctionFixture(t, func(plan map[string]any) {
		resources := plan["planned_values"].(map[string]any)["root_module"].(map[string]any)["resources"].([]any)
		resources[0].(map[string]any)["values"].(map[string]any)["arn"] = "arn:aws:cloudfront::123456789012:function/viewer"
		resources[1].(map[string]any)["values"].(map[string]any)["arn"] = "arn:aws:cloudfront::123456789012:function/response"
		functionDistributionValues(plan)["default_cache_behavior"].([]any)[0].(map[string]any)["function_association"].([]any)[0].(map[string]any)["function_arn"] = "arn:aws:cloudfront::123456789012:function/viewer"
		cfg := plan["configuration"].(map[string]any)["root_module"].(map[string]any)["resources"].([]any)[0].(map[string]any)["expressions"].(map[string]any)
		cfg["default_cache_behavior"].([]any)[0].(map[string]any)["function_association"].([]any)[0].(map[string]any)["function_arn"] = map[string]any{"references": []any{"aws_cloudfront_function.response.arn"}}
	})
	if _, err := Convert(data, Options{References: functionHints()}); err == nil || !strings.Contains(err.Error(), "conflicting configured function") {
		t.Fatalf("conflicting configuration accepted: %v", err)
	}
}

func TestAmbiguousKnownFunctionARNFails(t *testing.T) {
	data := modifiedFunctionFixture(t, func(plan map[string]any) {
		resources := plan["planned_values"].(map[string]any)["root_module"].(map[string]any)["resources"].([]any)
		for _, resource := range resources[:2] {
			resource.(map[string]any)["values"].(map[string]any)["arn"] = "arn:aws:cloudfront::123456789012:function/shared"
		}
		functionDistributionValues(plan)["default_cache_behavior"].([]any)[0].(map[string]any)["function_association"].([]any)[0].(map[string]any)["function_arn"] = "arn:aws:cloudfront::123456789012:function/shared"
	})
	if _, err := Convert(data, Options{References: functionHints()}); err == nil || !strings.Contains(err.Error(), "ambiguous managed function ARN") {
		t.Fatalf("ambiguous ARN accepted: %v", err)
	}
}

func TestDuplicateConfiguredFunctionEventsFail(t *testing.T) {
	data := modifiedFunctionFixture(t, func(plan map[string]any) {
		cfg := plan["configuration"].(map[string]any)["root_module"].(map[string]any)["resources"].([]any)[0].(map[string]any)["expressions"].(map[string]any)
		configured := cfg["default_cache_behavior"].([]any)[0].(map[string]any)["function_association"].([]any)
		cfg["default_cache_behavior"].([]any)[0].(map[string]any)["function_association"] = append(configured, configured[0])
	})
	if _, err := Convert(data, Options{References: functionHints()}); err == nil || !strings.Contains(err.Error(), "duplicate configured event") {
		t.Fatalf("duplicate configured event accepted: %v", err)
	}
}

func TestPositionalFunctionHintRejected(t *testing.T) {
	data, err := os.ReadFile(functionFixture)
	if err != nil {
		t.Fatal(err)
	}
	hints := functionHints()
	delete(hints[distributionAddress], "ordered_cache_behavior[0].function_association[viewer-request].function_arn")
	hints[distributionAddress]["ordered_cache_behavior[0].function_association[0].function_arn"] = []string{"aws_cloudfront_function.viewer"}
	if _, err := Convert(data, Options{References: hints}); err == nil || !strings.Contains(err.Error(), "unresolved function ARN") {
		t.Fatalf("positional hint accepted: %v", err)
	}
}

func TestKnownARNWinsOverAmbiguousConfiguration(t *testing.T) {
	known := "arn:aws:cloudfront::123456789012:function/viewer"
	data := modifiedFunctionFixture(t, func(plan map[string]any) {
		resources := plan["planned_values"].(map[string]any)["root_module"].(map[string]any)["resources"].([]any)
		resources[0].(map[string]any)["values"].(map[string]any)["arn"] = known
		functionDistributionValues(plan)["default_cache_behavior"].([]any)[0].(map[string]any)["function_association"].([]any)[0].(map[string]any)["function_arn"] = known
		setDefaultFunctionReferences(plan, []any{"aws_cloudfront_function.viewer.arn", "aws_cloudfront_function.response.arn"})
	})
	res, err := Convert(data, Options{Format: "json", References: functionHints()})
	if err != nil {
		t.Fatal(err)
	}
	got := defaultFunctionARN(t, res.Output).(map[string]any)["Fn::GetAtt"].([]any)[0]
	if got != "AwsCloudfrontFunctionViewer" {
		t.Fatalf("known managed target changed to %v", got)
	}
}

func TestUnknownAmbiguousConfigurationRequiresHint(t *testing.T) {
	data := modifiedFunctionFixture(t, func(plan map[string]any) {
		setDefaultFunctionReferences(plan, []any{"aws_cloudfront_function.viewer.arn", "aws_cloudfront_function.response.arn"})
	})
	if _, err := Convert(data, Options{References: functionHints()}); err == nil || !strings.Contains(err.Error(), "unresolved function ARN") {
		t.Fatalf("ambiguous config without hint accepted: %v", err)
	}
	hints := functionHints()
	hints[distributionAddress]["default_cache_behavior[0].function_association[viewer-request].function_arn"] = []string{"aws_cloudfront_function.viewer"}
	res, err := Convert(data, Options{Format: "json", References: hints})
	if err != nil {
		t.Fatal(err)
	}
	got := defaultFunctionARN(t, res.Output).(map[string]any)["Fn::GetAtt"].([]any)[0]
	if got != "AwsCloudfrontFunctionViewer" {
		t.Fatalf("hint selected %v", got)
	}
}

func TestUnknownUnsupportedConfigurationCanUseHint(t *testing.T) {
	data := modifiedFunctionFixture(t, func(plan map[string]any) {
		setDefaultFunctionReferences(plan, []any{"aws_cloudfront_function.missing.arn"})
	})
	if _, err := Convert(data, Options{References: functionHints()}); err == nil || !strings.Contains(err.Error(), "unresolved function ARN") {
		t.Fatalf("unsupported config without hint accepted: %v", err)
	}
	hints := functionHints()
	hints[distributionAddress]["default_cache_behavior[0].function_association[viewer-request].function_arn"] = []string{"aws_cloudfront_function.viewer"}
	if _, err := Convert(data, Options{References: hints}); err != nil {
		t.Fatalf("hint for unsupported config rejected: %v", err)
	}
}

func TestOtherDependenciesDoNotSelectFallback(t *testing.T) {
	for _, dependency := range []string{"module.edge.viewer_arn", "local.viewer_arn", "var.viewer_arn", "data.aws_cloudfront_function.viewer.arn"} {
		t.Run(dependency, func(t *testing.T) {
			data := modifiedFunctionFixture(t, func(plan map[string]any) {
				setDefaultFunctionReferences(plan, []any{dependency, "aws_cloudfront_function.viewer.arn", "aws_cloudfront_function.viewer"})
			})
			if _, err := Convert(data, Options{References: functionHints()}); err == nil || !strings.Contains(err.Error(), "unresolved function ARN") {
				t.Fatalf("mixed dependency selected fallback: %v", err)
			}
			hints := functionHints()
			hints[distributionAddress]["default_cache_behavior[0].function_association[viewer-request].function_arn"] = []string{"aws_cloudfront_function.response"}
			res, err := Convert(data, Options{Format: "json", References: hints})
			if err != nil {
				t.Fatal(err)
			}
			got := defaultFunctionARN(t, res.Output).(map[string]any)["Fn::GetAtt"].([]any)[0]
			if got != "AwsCloudfrontFunctionResponse" {
				t.Fatalf("hint selected %v", got)
			}
		})
	}
}

func TestKnownARNMismatchWithUnambiguousConfigurationFails(t *testing.T) {
	data := modifiedFunctionFixture(t, func(plan map[string]any) {
		functionDistributionValues(plan)["default_cache_behavior"].([]any)[0].(map[string]any)["function_association"].([]any)[0].(map[string]any)["function_arn"] = "arn:aws:cloudfront::123456789012:function/external"
	})
	if _, err := Convert(data, Options{References: functionHints()}); err == nil || !strings.Contains(err.Error(), "does not match configured") {
		t.Fatalf("known/config mismatch accepted: %v", err)
	}
}

func TestIndexedFunctionAndCollectionAncestorResolve(t *testing.T) {
	address := `aws_cloudfront_function.viewer["alpha"]`
	data := modifiedFunctionFixture(t, func(plan map[string]any) {
		resources := plan["planned_values"].(map[string]any)["root_module"].(map[string]any)["resources"].([]any)
		resources[0].(map[string]any)["address"] = address
		setDefaultFunctionReferences(plan, []any{address + ".arn", address, "aws_cloudfront_function.viewer"})
	})
	hints := functionHints()
	for path, targets := range hints[distributionAddress] {
		if targets[0] == "aws_cloudfront_function.viewer" {
			hints[distributionAddress][path] = []string{address}
		}
	}
	res, err := Convert(data, Options{Format: "json", References: hints})
	if err != nil {
		t.Fatal(err)
	}
	got := defaultFunctionARN(t, res.Output).(map[string]any)["Fn::GetAtt"].([]any)[0]
	if got != "AwsCloudfrontFunctionViewerKeyalpha" {
		t.Fatalf("indexed function resolved to %v", got)
	}
}

func setDefaultFunctionReferences(plan map[string]any, refs []any) {
	config := plan["configuration"].(map[string]any)["root_module"].(map[string]any)["resources"].([]any)[0].(map[string]any)["expressions"].(map[string]any)
	config["default_cache_behavior"].([]any)[0].(map[string]any)["function_association"].([]any)[0].(map[string]any)["function_arn"] = map[string]any{"references": refs}
}

func defaultFunctionARN(t *testing.T, data []byte) any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	resources := doc["Resources"].(map[string]any)
	behavior := resources["AwsCloudfrontDistributionExample"].(map[string]any)["Properties"].(map[string]any)["DistributionConfig"].(map[string]any)["DefaultCacheBehavior"].(map[string]any)
	return behavior["FunctionAssociations"].([]any)[0].(map[string]any)["FunctionARN"]
}

// The plan value, unknown marker, dependency evidence, and explicit hint are
// independent inputs. Keep their precedence in one table as cases are added.
func TestFunctionARNDecisionTable(t *testing.T) {
	const managedARN = "arn:aws:cloudfront::123456789012:function/viewer"
	const externalARN = "arn:aws:cloudfront::123456789012:function/external"
	for _, tc := range []struct {
		name, known, hint, want, wantError string
		managed, duplicateARN, marker      bool
		configRefs                         []any
	}{
		{"unknown unique", "", "", "AwsCloudfrontFunctionViewer", "", false, false, true, []any{"aws_cloudfront_function.viewer.arn"}},
		{"unknown unique conflicting hint", "", "response", "", "conflicting function hint", false, false, true, []any{"aws_cloudfront_function.viewer.arn"}},
		{"unknown ambiguous", "", "", "", "unresolved function ARN", false, false, true, []any{"aws_cloudfront_function.viewer.arn", "aws_cloudfront_function.response.arn"}},
		{"unknown ambiguous with hint", "", "response", "AwsCloudfrontFunctionResponse", "", false, false, true, []any{"aws_cloudfront_function.viewer.arn", "aws_cloudfront_function.response.arn"}},
		{"unknown ambiguous with invalid hint", "", "missing", "", "must be a managed aws_cloudfront_function", false, false, true, []any{"aws_cloudfront_function.viewer.arn", "aws_cloudfront_function.response.arn"}},
		{"unknown mixed", "", "", "", "unresolved function ARN", false, false, true, []any{"module.edge.viewer_arn", "aws_cloudfront_function.viewer.arn"}},
		{"unknown mixed with hint", "", "response", "AwsCloudfrontFunctionResponse", "", false, false, true, []any{"module.edge.viewer_arn", "aws_cloudfront_function.viewer.arn"}},
		{"unknown unsupported with hint", "", "viewer", "AwsCloudfrontFunctionViewer", "", false, false, true, []any{"aws_cloudfront_function.missing.arn"}},
		{"missing marker with hint", "", "viewer", "", "not marked unknown", false, false, false, []any{"aws_cloudfront_function.viewer.arn"}},
		{"missing marker without hint", "", "", "", "not marked unknown", false, false, false, []any{"aws_cloudfront_function.viewer.arn"}},
		{"known managed ambiguous config", managedARN, "", "AwsCloudfrontFunctionViewer", "", true, false, false, []any{"aws_cloudfront_function.viewer.arn", "aws_cloudfront_function.response.arn"}},
		{"known managed mixed dependencies", managedARN, "", "AwsCloudfrontFunctionViewer", "", true, false, false, []any{"local.viewer_arn", "aws_cloudfront_function.response.arn"}},
		{"known managed conflicting config", managedARN, "", "", "conflicting configured function", true, false, false, []any{"aws_cloudfront_function.response.arn"}},
		{"known managed with hint", managedARN, "viewer", "", "unused reference hint", true, false, false, []any{"aws_cloudfront_function.viewer.arn"}},
		{"known ambiguous match", managedARN, "", "", "ambiguous managed function ARN", true, true, false, nil},
		{"known external", externalARN, "", externalARN, "", false, false, false, nil},
		{"known literal mismatches config", externalARN, "", "", "does not match configured", false, false, false, []any{"aws_cloudfront_function.viewer.arn"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := modifiedFunctionFixture(t, func(plan map[string]any) {
				resources := plan["planned_values"].(map[string]any)["root_module"].(map[string]any)["resources"].([]any)
				if tc.managed {
					resources[0].(map[string]any)["values"].(map[string]any)["arn"] = managedARN
				}
				if tc.duplicateARN {
					resources[1].(map[string]any)["values"].(map[string]any)["arn"] = managedARN
				}
				association := functionDistributionValues(plan)["default_cache_behavior"].([]any)[0].(map[string]any)["function_association"].([]any)[0].(map[string]any)
				if tc.known != "" {
					association["function_arn"] = tc.known
				}
				if !tc.marker {
					marker := functionUnknowns(plan)["default_cache_behavior"].([]any)[0].(map[string]any)["function_association"].([]any)[0].(map[string]any)
					delete(marker, "function_arn")
				}
				if tc.configRefs != nil {
					setDefaultFunctionReferences(plan, tc.configRefs)
				} else {
					setDefaultFunctionReferences(plan, []any{})
				}
			})
			hints := functionHints()
			if tc.hint != "" {
				hints[distributionAddress]["default_cache_behavior[0].function_association[viewer-request].function_arn"] = []string{"aws_cloudfront_function." + tc.hint}
			}
			result, err := Convert(data, Options{Format: "json", References: hints})
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) || result != nil {
					t.Fatalf("want %q and no output, got result=%v err=%v", tc.wantError, result, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := defaultFunctionARN(t, result.Output)
			if tc.want == externalARN {
				if got != externalARN {
					t.Fatalf("external ARN changed to %v", got)
				}
			} else if got.(map[string]any)["Fn::GetAtt"].([]any)[0] != tc.want {
				t.Fatalf("got %v, want %s", got, tc.want)
			}
		})
	}
}

func TestDeposedChangeDoesNotSupplyUnknownMarkers(t *testing.T) {
	data := modifiedFunctionFixture(t, func(plan map[string]any) {
		changes := plan["resource_changes"].([]any)
		deposed := map[string]any{
			"address": distributionAddress,
			"deposed": "00000001",
			"change":  map[string]any{"after_unknown": map[string]any{}},
		}
		plan["resource_changes"] = append([]any{deposed}, changes...)
	})
	if _, err := Convert(data, Options{References: functionHints()}); err != nil {
		t.Fatalf("deposed change replaced the planned unknown markers: %v", err)
	}
}
