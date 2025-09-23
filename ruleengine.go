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
	// combinationTypeAnd is logical AND combination of rulesets
	combinationTypeAnd RulesetCombinationType = "AND"
	// combinationTypeOr is logical OR combination of rulesets
	combinationTypeOr RulesetCombinationType = "OR"
)

// RuleEngine holds the configuration and compiled programs for rule evaluation
type RuleEngine struct {
	config   *RulesetConfig
	env      *cel.Env
	programs map[string]cel.Program
	policy   Policy
	context  map[string]interface{}
}

type Policy struct {
	StopOnFailure    bool
	MaxExecutionTime time.Duration
}

// NewRuleEngine creates a new ruleengine instance
func NewRuleEngine(configPath string, environment string, env *cel.Env) (*RuleEngine, error) {
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
func (re *RuleEngine) EvaluateRule(ruleName string) (RuleResult, error) {
	start := time.Now()

	_, rExists := re.config.Rules[ruleName]
	if !rExists {
		return RuleResult{}, fmt.Errorf("rule '%s' not found", ruleName)
	}

	program, pExists := re.programs[ruleName]
	if !pExists {
		return RuleResult{}, fmt.Errorf("compiled program for rule '%s' not found", ruleName)
	}

	out, _, err := program.Eval(re.context)
	if err != nil {
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
	var customError error
	if !passed {
		if msg, ok := re.config.ErrorHandling.CustomErrorMessages[ruleName]; ok {
			customError = errors.New(msg)
		}
	}
	return RuleResult{
		RuleName: ruleName,
		Passed:   passed,
		Error:    customError,
		Duration: time.Since(start),
	}, nil
}

// EvaluateRuleset evaluates a ruleset by name, handling rule inheritance and combination logic
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
		if err != nil {
			result.Error = err
			result.Duration = time.Since(start)
			return result, nil
		}
		// fail-fast policy
		if ruleset.CombinationType != combinationTypeOr && !ruleResult.Passed && re.policy.StopOnFailure {
			result.RuleResults[ruleRef] = ruleResult
			result.Passed = false
			result.Duration = time.Since(start)
			return result, nil
		}
		result.RuleResults[ruleRef] = ruleResult
	}

	// Evaluate based on combination type
	switch ruleset.CombinationType {
	case combinationTypeAnd:
		result.Passed = true
		for _, ruleResult := range result.RuleResults {
			if !ruleResult.Passed {
				result.Passed = false
				break
			}
		}

	case combinationTypeOr:
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

	var customError error
	if !result.Passed {
		if msg, ok := re.config.ErrorHandling.CustomErrorMessages[rulesetName]; ok {
			customError = errors.New(msg)
		}
	}

	result.Duration = time.Since(start)
	result.Error = customError
	return result, nil
}

// EvaluateAllRulesets evaluates all rulesets defined in the configuration
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
		if err != nil {
			return results, err
		}
		results[rulesetName] = result
	}

	return results, nil
}

// compileRules parses, checks and compiles all rule expressions into `cel.Program`
func (re *RuleEngine) compileRules() error {
	// Compile individual rules
	for name, rule := range re.config.Rules {
		ast, issues := re.env.Compile(rule.Expression)
		if issues != nil && issues.Err() != nil {
			return fmt.Errorf("failed to compile rule '%s': %w", name, issues.Err())
		}

		program, err := re.env.Program(ast)
		if err != nil {
			return fmt.Errorf("failed to create program for rule '%s': %w", name, err)
		}

		re.programs[name] = program
	}

	// Compile ruleset expressions and custom rules
	for rulesetName, ruleset := range re.config.Rulesets {
		// Compile custom rules within rulesets
		if ruleset.CustomRules != nil {
			for customRuleName, customRule := range ruleset.CustomRules {
				fullName := fmt.Sprintf("%s.%s", rulesetName, customRuleName)

				ast, issues := re.env.Compile(customRule.Expression)
				if issues != nil && issues.Err() != nil {
					return fmt.Errorf("failed to compile custom rule '%s': %w", fullName, issues.Err())
				}

				program, err := re.env.Program(ast)
				if err != nil {
					return fmt.Errorf("failed to create program for custom rule '%s': %w", fullName, err)
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
			ast, issues := re.env.Compile(ruleset.Expression)
			if issues != nil && issues.Err() != nil {
				return fmt.Errorf("failed to compile ruleset expression '%s': %w", rulesetName, issues.Err())
			}

			program, err := re.env.Program(ast)
			if err != nil {
				return fmt.Errorf("failed to create program for ruleset '%s': %w", rulesetName, err)
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
