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
	config   *RulesetConfig
	env      *cel.Env
	programs map[string]cel.Program
	policy   Policy
	context  map[string]interface{}
	optimise bool
}

type Policy struct {
	StopOnFailure    bool
	MaxExecutionTime time.Duration
}

// Option defines a function that configures a RuleEngine
type Option func(*RuleEngine)

// WithOptimise enables optimization for rule evaluation
func WithOptimise(optimise bool) Option {
	return func(re *RuleEngine) {
		re.optimise = optimise
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

	program, pExists := re.programs[ruleName]
	if !pExists {
		return RuleResult{}, fmt.Errorf("compiled program for rule '%s' not found", ruleName)
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
	passed := false
	if boolVal, ok := value.(bool); ok {
		passed = boolVal
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

	// Handle rule inheritance
	var allRules []string
	if ruleset.Extends != "" {
		_, bRuleOk := re.config.Rules[ruleset.Extends]
		if bRuleOk {
			allRules = append(allRules, ruleset.Extends)
		}
	}

	// Handle custom rules
	if ruleset.CustomRules != nil {
		for customRuleName := range ruleset.CustomRules {
			fullName := fmt.Sprintf("%s.%s", rulesetName, customRuleName)
			_, pOk := re.programs[fullName]
			if pOk {
				allRules = append(allRules, fullName)
			}
		}
	}

	// Handle main ruleset expression
	if ruleset.Expression != "" {
		exprRuleName := fmt.Sprintf("ruleset.%s", rulesetName)
		_, pOk := re.programs[exprRuleName]
		if pOk {
			allRules = append(allRules, exprRuleName)
		}
	}

	// Add explicitly listed rules
	allRules = append(allRules, ruleset.Rules...)
	result := RulesetResult{
		RulesetName: rulesetName,
		RuleResults: make(map[string]RuleResult, len(allRules)),
	}

	// Evaluate individual rules
	for _, ruleRef := range allRules {
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
	}

	// Compile ruleset expressions and custom rules
	for rulesetName, ruleset := range re.config.Rulesets {
		// Compile custom rules within rulesets
		if ruleset.CustomRules != nil {
			for customRuleName, customRule := range ruleset.CustomRules {
				fullName := fmt.Sprintf("%s.%s", rulesetName, customRuleName)

				program, err := re.compileExpression(customRule.Expression)
				if err != nil {
					return fmt.Errorf("failed to compile program for ruleset '%s': %w", fullName, err)
				}

				re.config.Rules[fullName] = Rule{
					Name:       fullName,
					Expression: customRule.Expression,
				}
				re.programs[fullName] = program
			}
		}

		// Compile main ruleset expression if present
		if ruleset.Expression != "" {
			program, err := re.compileExpression(ruleset.Expression)
			if err != nil {
				return fmt.Errorf("failed to compile program for ruleset '%s': %w", ruleset.Name, err)
			}
			fullName := fmt.Sprintf("ruleset.%s", rulesetName)
			re.config.Rules[fullName] = Rule{
				Name:       fullName,
				Expression: ruleset.Expression,
			}
			re.programs[fullName] = program
		}
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
