package ruleengine

import (
	"time"
)

// RuleResult represents the outcome of a single rule evaluation
type RuleResult struct {
	RuleName string
	Passed   bool
	Value    interface{}
	Error    error
	Duration time.Duration
}

// RulesetResult represents the outcome of a ruleset evaluation
type RulesetResult struct {
	RulesetName string
	Passed      bool
	RuleResults map[string]RuleResult
	Error       error
	Duration    time.Duration
}
