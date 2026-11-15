package ruleengine

import (
	"encoding/json"
	"time"
)

// RuleResult represents the outcome of a single rule evaluation.
type RuleResult struct {
	// RuleName is the name of the evaluated rule.
	RuleName string
	// Passed indicates whether the rule (and its full ancestor chain) evaluated to true.
	Passed bool
	// Error is the business-facing failure message (custom or default) when Passed is false.
	Error error
	// EvalError is set when the rule could not be evaluated (e.g. missing field, type error,
	// non-boolean result). A rule with EvalError is never Passed, and NONE rulesets fail closed on it.
	EvalError error
	// Duration is the time taken to evaluate the rule.
	Duration time.Duration
}

// MarshalJSON serializes RuleResult with errors rendered as strings.
func (r RuleResult) MarshalJSON() ([]byte, error) {
	type ruleResultJSON struct {
		RuleName  string        `json:"rule_name"`
		Passed    bool          `json:"passed"`
		Error     string        `json:"error,omitempty"`
		EvalError string        `json:"eval_error,omitempty"`
		Duration  time.Duration `json:"duration_ns"`
	}
	out := ruleResultJSON{
		RuleName: r.RuleName,
		Passed:   r.Passed,
		Duration: r.Duration,
	}
	if r.Error != nil {
		out.Error = r.Error.Error()
	}
	if r.EvalError != nil {
		out.EvalError = r.EvalError.Error()
	}
	return json.Marshal(out)
}

// RulesetResult represents the outcome of a ruleset evaluation.
type RulesetResult struct {
	// RulesetName is the name of the evaluated ruleset.
	RulesetName string
	// Passed indicates whether the ruleset evaluation was successful.
	Passed bool
	// RuleResults contains the results of individual rule evaluations within the ruleset.
	RuleResults map[string]RuleResult
	// Error contains the reason for ruleset not passing, if any.
	Error error
	// Duration is the time taken to evaluate the ruleset.
	Duration time.Duration
}

// MarshalJSON serializes RulesetResult with Error rendered as a string.
func (r RulesetResult) MarshalJSON() ([]byte, error) {
	type rulesetResultJSON struct {
		RulesetName string                `json:"ruleset_name"`
		Passed      bool                  `json:"passed"`
		RuleResults map[string]RuleResult `json:"rule_results"`
		Error       string                `json:"error,omitempty"`
		Duration    time.Duration         `json:"duration_ns"`
	}
	out := rulesetResultJSON{
		RulesetName: r.RulesetName,
		Passed:      r.Passed,
		RuleResults: r.RuleResults,
		Duration:    r.Duration,
	}
	if r.Error != nil {
		out.Error = r.Error.Error()
	}
	return json.Marshal(out)
}
