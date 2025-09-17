package ruleengine

import (
	"time"

	"github.com/google/cel-go/cel"
)

// RuleEngine
type RuleEngine struct {
	config   *RulesetConfig
	env      *cel.Env
	programs map[string]cel.Program
	context  map[string]interface{}
}

type RuleResult struct {
	RuleName string
	Passed   bool
	Value    interface{}
	Error    error
	Duration time.Duration
}

type RulesetResult struct {
	RulesetName string
	Passed      bool
	RuleResults map[string]RuleResult
	Error       error
	Duration    time.Duration
}
