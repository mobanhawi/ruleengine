package ruleengine

import (
	"time"
)

// RuleResult represents the outcome of a single rule evaluation
type RuleResult struct {
	// RuleName is the name of the evaluated rule
	RuleName string
	// Passed indicates whether the rule evaluation was successful
	Passed bool
	// Error contains the reason for rule not passing, if any, evaluation errors are not returned here
	Error error
	// Duration is the time taken to evaluate the rule
	Duration time.Duration
}

// RulesetResult represents the outcome of a ruleset evaluation
type RulesetResult struct {
	// RulesetName is the name of the evaluated ruleset
	RulesetName string
	// Passed indicates whether the ruleset evaluation was successful
	Passed bool
	// RuleResults contains the results of individual rule evaluations within the ruleset
	RuleResults map[string]RuleResult
	// Error contains the reason for ruleset not passing, if any, evaluation errors are not returned here
	Error error
	// Duration is the time taken to evaluate the ruleset
	Duration time.Duration
}
