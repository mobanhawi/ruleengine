package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mobanhawi/ruleengine"
)

var defaultVariables = []string{"user", "request"}

// EvaluateRequest is the JSON payload accepted by the WebAssembly playground bridge.
type EvaluateRequest struct {
	ConfigYAML    string   `json:"config_yaml"`
	Environment   string   `json:"environment,omitempty"`
	Variables     []string `json:"variables,omitempty"`
	InputJSON     string   `json:"input_json"`
	TargetRuleset string   `json:"target_ruleset,omitempty"`
}

// EvaluateResponse is the JSON payload returned by the WebAssembly playground bridge.
type EvaluateResponse struct {
	OK             bool                                `json:"ok"`
	Error          string                              `json:"error,omitempty"`
	Rules          map[string]ruleengine.Rule          `json:"rules,omitempty"`
	Rulesets       map[string]ruleengine.Ruleset       `json:"rulesets,omitempty"`
	RulesetResults map[string]ruleengine.RulesetResult `json:"ruleset_results,omitempty"`
	RuleResults    map[string]ruleengine.RuleResult    `json:"rule_results,omitempty"`
	DurationMs     float64                             `json:"duration_ms"`
}

// RunEvaluationJSON unmarshals a JSON EvaluateRequest, executes it, and returns a JSON EvaluateResponse.
func RunEvaluationJSON(ctx context.Context, rawRequest string) string {
	var req EvaluateRequest
	if err := json.Unmarshal([]byte(rawRequest), &req); err != nil {
		return marshalResponse(EvaluateResponse{
			OK:    false,
			Error: fmt.Sprintf("invalid playground request JSON: %v", err),
		})
	}
	return marshalResponse(RunEvaluation(ctx, req))
}

// RunEvaluation compiles the YAML config and evaluates rulesets and rules against the input JSON.
func RunEvaluation(ctx context.Context, req EvaluateRequest) EvaluateResponse {
	start := time.Now()

	if strings.TrimSpace(req.ConfigYAML) == "" {
		return EvaluateResponse{OK: false, Error: "rules YAML configuration cannot be empty"}
	}

	var input map[string]any
	if strings.TrimSpace(req.InputJSON) != "" {
		if err := json.Unmarshal([]byte(req.InputJSON), &input); err != nil {
			return EvaluateResponse{
				OK:    false,
				Error: fmt.Sprintf("invalid input JSON: %v", err),
			}
		}
	} else {
		input = map[string]any{}
	}

	cfg, err := ruleengine.NewRulesetConfigFromBytes([]byte(req.ConfigYAML))
	if err != nil {
		return EvaluateResponse{
			OK:    false,
			Error: fmt.Sprintf("failed to parse rules YAML: %v", err),
		}
	}

	vars := req.Variables
	if len(vars) == 0 {
		vars = defaultVariables
	}
	for k := range input {
		vars = append(vars, k)
	}

	env, err := ruleengine.NewDefaultEnv(vars...)
	if err != nil {
		return EvaluateResponse{
			OK:    false,
			Error: fmt.Sprintf("failed to create CEL environment: %v", err),
		}
	}

	engine, err := ruleengine.NewRuleEngineFromConfig(cfg, req.Environment, env, ruleengine.WithOptimise())
	if err != nil {
		return EvaluateResponse{
			OK:    false,
			Error: err.Error(),
		}
	}

	rules := engine.ListRules()
	rulesets := engine.ListRulesets()

	var rulesetResults map[string]ruleengine.RulesetResult
	if req.TargetRuleset != "" {
		res, evalErr := engine.EvaluateRuleset(ctx, req.TargetRuleset, input)
		if evalErr != nil {
			return EvaluateResponse{
				OK:    false,
				Error: evalErr.Error(),
			}
		}
		rulesetResults = map[string]ruleengine.RulesetResult{
			req.TargetRuleset: res,
		}
	} else {
		allRes, evalErr := engine.EvaluateAllRulesets(ctx, input)
		if evalErr != nil {
			return EvaluateResponse{
				OK:    false,
				Error: evalErr.Error(),
			}
		}
		rulesetResults = allRes
	}

	ruleResults := make(map[string]ruleengine.RuleResult, len(rules))
	for ruleID := range rules {
		res, evalErr := engine.EvaluateRule(ctx, ruleID, input)
		if evalErr != nil {
			return EvaluateResponse{
				OK:    false,
				Error: evalErr.Error(),
			}
		}
		ruleResults[ruleID] = res
	}

	return EvaluateResponse{
		OK:             true,
		Rules:          rules,
		Rulesets:       rulesets,
		RulesetResults: rulesetResults,
		RuleResults:    ruleResults,
		DurationMs:     float64(time.Since(start).Microseconds()) / 1000.0,
	}
}

func marshalResponse(resp EvaluateResponse) string {
	b, err := json.Marshal(resp)
	if err != nil {
		return `{"ok":false,"error":"failed to serialize response"}`
	}
	return string(b)
}
