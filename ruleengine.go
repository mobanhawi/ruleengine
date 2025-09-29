package ruleengine

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

const (
	// selectorAnd is logical AND combination of rulesets
	selectorAnd selectorType = "AND"
	// selectorOr is logical OR combination of rulesets
	selectorOr selectorType = "OR"
)

// RuleEngine holds the configuration and compiled programs for rule evaluation
type RuleEngine struct {
	// config is the loaded ruleset configuration
	config *RulesetConfig
	// env is the CEL environment used for compiling and evaluating expressions
	env *cel.Env
	// programs is a map of rule names to their compiled CEL programs
	programs map[string]cel.Program
	// parents is a map of rule names to their parent rules for inheritance
	parents map[string][]string
	// policy is the execution policy applied during rule evaluation
	policy Policy
	// context is the evaluation context containing requests variables, functions & globals
	context map[string]interface{}
	// optimise indicates whether to optimise rule evaluation
	optimise bool
}

type Policy struct {
	StopOnFailure    bool
	MaxExecutionTime time.Duration
}

// Option defines a function that configures a RuleEngine
type Option func(*RuleEngine)

// WithOptimise enables optimization for rule evaluation
func WithOptimise() Option {
	return func(re *RuleEngine) {
		re.optimise = true
	}
}

// NewRuleEngine creates a new ruleengine instance
func NewRuleEngine(configPath string, environment string, env *cel.Env, opts ...Option) (*RuleEngine, error) {
	config, err := NewRulesetConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	config.ApplyEnvironment(environment)

	policy, err := config.ToExecutionPolicy()
	if err != nil {
		return nil, fmt.Errorf("failed to get execution policy: %w", err)
	}

	if env == nil {
		return nil, fmt.Errorf("cel env is nil")
	}

	engine := &RuleEngine{
		config:   config,
		env:      env,
		policy:   policy,
		programs: make(map[string]cel.Program),
		context:  make(map[string]interface{}),
		parents:  make(map[string][]string),
		optimise: false,
	}

	// Apply all provided options
	for _, opt := range opts {
		opt(engine)
	}

	// Pre-compile all rule expressions into `cel.Program`
	err = engine.compileRules()
	if err != nil {
		return nil, fmt.Errorf("failed to compile rules: %w", err)
	}

	return engine, nil
}

// SetContext sets the evaluation context for the rule engine
func (re *RuleEngine) SetContext(ctx map[string]interface{}) {
	re.context = ctx
	// Always include globals in context
	re.context["globals"] = re.config.Globals
	// Add current timestamp
	re.context["now"] = func() ref.Val {
		return types.Timestamp{Time: time.Now()}
	}
	re.context["timestamp"] = func(s string) ref.Val {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return types.NewErr("invalid timestamp format")
		}
		return types.Timestamp{Time: t}
	}
}

// EvaluateRule evaluates a single rule `cel.Program` by name
//
//	Errors are returned if the rule is not found or if there is an issue during evaluation
//	If the rule evaluates to false, a RuleResult with Passed=false is returned and nil error
func (re *RuleEngine) EvaluateRule(ruleName string) (RuleResult, error) {
	start := time.Now()

	rule, rExists := re.config.Rules[ruleName]
	if !rExists {
		return RuleResult{}, fmt.Errorf("rule '%s' not found", ruleName)
	}

	allRules := append(re.parents[ruleName], ruleName)

	passed := false
	for _, r := range allRules {
		program, pExists := re.programs[r]
		if !pExists {
			return RuleResult{}, fmt.Errorf("program for rule '%s' not found", rule)
		}
		out, _, err := program.Eval(re.context)
		if err != nil {
			// An unsuccessful evaluation is typically the result of a series of incompatible `EnvOption`
			// or `ProgramOption` values used in the creation of the evaluation environment or executable
			// program.
			// We don't want to overwrite CEL evaluation errors with custom error messages
			// Instead, we return a failed RuleResult with the error.
			// The caller can decide how to handle it based on the policy.
			return RuleResult{
				RuleName: ruleName,
				Passed:   false,
				Error:    err,
				Duration: time.Since(start),
			}, nil
		}
		// Convert CEL value to Go value
		value := out.Value()
		if boolVal, ok := value.(bool); ok {
			passed = boolVal
		}
		// If any rule in the chain fails, the overall result is false
		if !passed {
			break
		}
	}

	// handle custom error messages
	var errorMessage error
	if !passed {
		errorMessage = fmt.Errorf("rule '%s' did not pass evaluation", ruleName)
		if msg, ok := re.config.ErrorHandling.CustomErrorMessages[ruleName]; ok {
			errorMessage = errors.New(msg)
		}
	}
	return RuleResult{
		RuleName: ruleName,
		Passed:   passed,
		Error:    errorMessage,
		Duration: time.Since(start),
	}, nil
}

// EvaluateRuleset evaluates a ruleset by name, handling rule inheritance and selector logic
//
//		Errors are returned if the ruleset is not found
//		If the rule evaluates to false, a RuleResult with Passed=false is returned and nil error
//	    If the rule evaluates to true, a RuleResult with Passed=true is returned and nil error
func (re *RuleEngine) EvaluateRuleset(rulesetName string) (RulesetResult, error) {
	start := time.Now()

	ruleset, rOk := re.config.Rulesets[rulesetName]
	if !rOk {
		return RulesetResult{}, fmt.Errorf("ruleset '%s' not found", rulesetName)
	}

	result := RulesetResult{
		RulesetName: rulesetName,
		RuleResults: make(map[string]RuleResult, len(ruleset.Rules)),
	}

	// Evaluate individual rules
	for _, ruleRef := range ruleset.Rules {
		ruleResult, err := re.EvaluateRule(ruleRef)
		result.RuleResults[ruleRef] = ruleResult
		// fail-fast policy
		if ruleset.Selector != selectorOr && (!ruleResult.Passed || err != nil) && re.policy.StopOnFailure {
			break
		}
	}

	// Evaluate based on selector type
	switch ruleset.Selector {
	case selectorAnd:
		result.Passed = true
		for _, ruleResult := range result.RuleResults {
			if !ruleResult.Passed {
				result.Passed = false
				break
			}
		}

	case selectorOr:
		result.Passed = false
		for _, ruleResult := range result.RuleResults {
			if ruleResult.Passed {
				result.Passed = true
				break
			}
		}

	default:
		// Default to AND logic
		result.Passed = true
		for _, ruleResult := range result.RuleResults {
			if !ruleResult.Passed {
				result.Passed = false
			}
		}
	}

	var errorMessage error
	if !result.Passed {
		errorMessage = fmt.Errorf("ruleset '%s' did not pass evaluation", rulesetName)
		if msg, ok := re.config.ErrorHandling.CustomErrorMessages[rulesetName]; ok {
			errorMessage = errors.New(msg)
		}
	}

	result.Duration = time.Since(start)
	result.Error = errorMessage
	return result, nil
}

// EvaluateAllRulesets evaluates all rulesets defined in the configuration
// Returns a map of ruleset names to their evaluation results
//
//		Errors are returned if the ruleset is not found, or if there is a timeout,
//		execution will be halted in these cases
//		If the rule evaluates to false, a RuleResult with Passed=false is returned and nil error
//	    If the rule evaluates to true, a RuleResult with Passed=true is returned and nil error
func (re *RuleEngine) EvaluateAllRulesets() (map[string]RulesetResult, error) {
	results := make(map[string]RulesetResult)
	ticker := time.NewTicker(re.policy.MaxExecutionTime)
	defer ticker.Stop()
	for rulesetName := range re.config.Rulesets {
		select {
		case <-ticker.C:
			return results, fmt.Errorf("timed out waiting for ruleset %s", rulesetName)
		default:
		}

		result, err := re.EvaluateRuleset(rulesetName)
		results[rulesetName] = result
		// This is only expected to happen if the ruleset name is missing
		if err != nil {
			return results, err
		}
	}

	return results, nil
}

// compileRules parses, checks and compiles all rule expressions into `cel.Program`
func (re *RuleEngine) compileRules() error {
	// Compile individual rules
	for name, rule := range re.config.Rules {
		program, err := re.compileExpression(rule.Expression)
		if err != nil {
			return fmt.Errorf("failed to compile program for rule '%s': %w", name, err)
		}
		re.programs[name] = program
		parents, err := re.getRuleParents(rule)
		if err != nil {
			return fmt.Errorf("failed to find parent rules for rule '%s': %w", name, err)
		}
		re.parents[name] = parents
	}

	return nil
}

// func compileExpression parses, checks and compiles a single CEL expression into `cel.Program`
func (re *RuleEngine) compileExpression(expression string) (cel.Program, error) {
	ast, issues := re.env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("failed to compile expression '%s': %w", expression, issues.Err())
	}
	evalOpts := cel.OptExhaustiveEval
	if re.optimise {
		evalOpts = cel.OptOptimize
	}
	program, err := re.env.Program(ast, cel.EvalOptions(evalOpts))
	if err != nil {
		return nil, fmt.Errorf("failed to create program for expression '%s': %w", expression, err)
	}
	return program, nil
}

// getRuleParents retrieves the parent rules for a given rule by following the Extends chain
// It returns a slice of parent rule names in order from immediate parent to the topmost ancestor
// If a circular dependency is detected, an error is returned or if an extended rule is not found
func (re *RuleEngine) getRuleParents(rule Rule) ([]string, error) {
	current := rule
	parents := make([]string, 0)
	visited := make(map[string]bool, 0)
	for current.Extends != "" {
		if visited[current.Extends] {
			return nil, fmt.Errorf("circular dependency detected in rule inheritance for rule '%s'", rule.Name)
		}
		visited[current.Extends] = true

		parent, exists := re.config.Rules[current.Extends]
		if !exists {
			return nil, fmt.Errorf("extended rule '%s' not found for rule '%s'", current.Extends, rule.Name)
		}
		parents = append(parents, current.Extends)
		current = parent
	}
	return parents, nil
}
