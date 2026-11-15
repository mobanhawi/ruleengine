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

func mustMarshalRequest(t *testing.T, req EvaluateRequest) string {
	t.Helper()
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return string(b)
}
