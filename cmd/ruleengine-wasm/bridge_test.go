package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

const (
	testPlaygroundYAML = `apiVersion: v1
kind: RulesetConfig
globals:
  min_age: 18
rules:
  is_adult:
    name: "Adult Check"
    expression: "user.age >= globals.min_age"
  is_blocked:
    name: "Blocklist Check"
    expression: "user.blocked == true"
rulesets:
  onboarding:
    name: "Onboarding"
    selector: "AND"
    rules:
      - is_adult
  not_blocked:
    name: "Not Blocked"
    selector: "NONE"
    rules:
      - is_blocked
`
	testValidInputJSON        = `{"user": {"age": 21, "blocked": false}}`
	testMissingFieldInputJSON = `{"user": {"age": 21}}`
)

func TestRunEvaluation(t *testing.T) {
	t.Run("GivenValidYAMLAndPassingInput_WhenRunEvaluation_ThenReturnsPassingRulesetsAndRules", func(t *testing.T) {
		resp := RunEvaluation(context.Background(), EvaluateRequest{
			ConfigYAML:  testPlaygroundYAML,
			Environment: "development",
			Variables:   []string{"user"},
			InputJSON:   testValidInputJSON,
		})
		if !resp.OK {
			t.Fatalf("RunEvaluation() OK = false, Error = %q", resp.Error)
		}
		if !resp.RulesetResults["onboarding"].Passed {
			t.Errorf("RulesetResults[onboarding].Passed = false, want true")
		}
		if !resp.RulesetResults["not_blocked"].Passed {
			t.Errorf("RulesetResults[not_blocked].Passed = false, want true")
		}
		if !resp.RuleResults["is_adult"].Passed {
			t.Errorf("RuleResults[is_adult].Passed = false, want true")
		}
	})

	t.Run("GivenTargetRulesetSpecified_WhenRunEvaluation_ThenEvaluatesOnlyTargetRuleset", func(t *testing.T) {
		resp := RunEvaluation(context.Background(), EvaluateRequest{
			ConfigYAML:    testPlaygroundYAML,
			Variables:     []string{"user"},
			InputJSON:     testValidInputJSON,
			TargetRuleset: "onboarding",
		})
		if !resp.OK {
			t.Fatalf("RunEvaluation() OK = false, Error = %q", resp.Error)
		}
		if len(resp.RulesetResults) != 1 {
			t.Fatalf("len(RulesetResults) = %d, want 1", len(resp.RulesetResults))
		}
		if !resp.RulesetResults["onboarding"].Passed {
			t.Errorf("RulesetResults[onboarding].Passed = false, want true")
		}
	})

	t.Run("GivenNoneSelectorWithMissingField_WhenRunEvaluation_ThenFailsClosedWithEvalError", func(t *testing.T) {
		rawOut := RunEvaluationJSON(context.Background(), mustMarshalRequest(t, EvaluateRequest{
			ConfigYAML:    testPlaygroundYAML,
			Variables:     []string{"user"},
			InputJSON:     testMissingFieldInputJSON,
			TargetRuleset: "not_blocked",
		}))

		var decoded map[string]any
		if err := json.Unmarshal([]byte(rawOut), &decoded); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		if decoded["ok"] != true {
			t.Fatalf("decoded[ok] = %v, want true (error=%v)", decoded["ok"], decoded["error"])
		}
		if !strings.Contains(rawOut, `"eval_error"`) {
			t.Errorf("expected eval_error in JSON output, got: %s", rawOut)
		}
	})

	t.Run("GivenInvalidYAMLOrInvalidJSON_WhenRunEvaluation_ThenReturnsErrorResponse", func(t *testing.T) {
		badYAMLResp := RunEvaluation(context.Background(), EvaluateRequest{
			ConfigYAML: "rules: [invalid",
			InputJSON:  testValidInputJSON,
		})
		if badYAMLResp.OK || badYAMLResp.Error == "" {
			t.Errorf("RunEvaluation(bad YAML) = %+v, want OK=false with error", badYAMLResp)
		}

		badJSONResp := RunEvaluation(context.Background(), EvaluateRequest{
			ConfigYAML: testPlaygroundYAML,
			InputJSON:  "{not-json",
		})
		if badJSONResp.OK || badJSONResp.Error == "" {
			t.Errorf("RunEvaluation(bad JSON) = %+v, want OK=false with error", badJSONResp)
		}
	})
}

func TestEvaluateArgsAndCLIEntrypoint(t *testing.T) {
	t.Run("GivenNoArguments_WhenEvaluateArgs_ThenReturnsMissingArgumentError", func(t *testing.T) {
		out := EvaluateArgs(context.Background(), nil)
		if !strings.Contains(out, "missing request JSON argument") {
			t.Errorf("EvaluateArgs(nil) = %s, want missing argument error", out)
		}
	})

	t.Run("GivenValidJSONArgument_WhenEvaluateArgs_ThenReturnsPassingResponse", func(t *testing.T) {
		rawReq := mustMarshalRequest(t, EvaluateRequest{
			ConfigYAML: testPlaygroundYAML,
			Variables:  []string{"user"},
			InputJSON:  testValidInputJSON,
		})
		out := EvaluateArgs(context.Background(), []string{rawReq})
		if !strings.Contains(out, `"ok":true`) {
			t.Errorf("EvaluateArgs(valid) = %s, want ok:true", out)
		}
	})

	t.Run("GivenValidRequestOnStdin_WhenRunCLI_ThenWritesEvaluationResponseToStdout", func(t *testing.T) {
		rawReq := mustMarshalRequest(t, EvaluateRequest{
			ConfigYAML: testPlaygroundYAML,
			Variables:  []string{"user"},
			InputJSON:  testValidInputJSON,
		})
		var out strings.Builder
		if err := runCLI(context.Background(), strings.NewReader(rawReq), &out); err != nil {
			t.Fatalf("runCLI() error = %v", err)
		}
		if !strings.Contains(out.String(), `"ok":true`) {
			t.Errorf("runCLI() stdout = %s, want ok:true", out.String())
		}
	})

	t.Run("GivenEmptyStdin_WhenRunCLI_ThenExitsCleanlyWithNoOutput", func(t *testing.T) {
		var out strings.Builder
		if err := runCLI(context.Background(), strings.NewReader("   \n"), &out); err != nil {
			t.Fatalf("runCLI(empty) error = %v", err)
		}
		if out.Len() != 0 {
			t.Errorf("runCLI(empty) stdout = %q, want empty", out.String())
		}
	})
}

func mustMarshalRequest(t *testing.T, req EvaluateRequest) string {
	t.Helper()
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return string(b)
}
