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
	config, err := loadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Apply environment-specific overrides
	if envConfig, exists := config.Environments[environment]; exists {
		// Apply environment-specific globals
		if envConfig.Globals != nil {
			for k, v := range envConfig.Globals {
				config.Globals[k] = v
			}
		}
		// Apply environment-specific error handling execution policy
		if envConfig.ErrorHandling.ExecutionPolicy != "" {
			config.ErrorHandling.ExecutionPolicy = envConfig.ErrorHandling.ExecutionPolicy
		}
		// Apply environment-specific custom error messages
		if envConfig.ErrorHandling.CustomErrorMessages != nil {
			for k, v := range envConfig.ErrorHandling.CustomErrorMessages {
				config.ErrorHandling.CustomErrorMessages[k] = v
			}
		}
	}

	// Set up defaults execution policy
	policy := Policy{
		StopOnFailure:    true,
		MaxExecutionTime: 5 * time.Second,
	}

	if configPolicy, ok := config.ExecutionPolicies[config.ErrorHandling.ExecutionPolicy]; ok {
		if configPolicy.MaxExecutionTime != "" {
			dur, err := time.ParseDuration(configPolicy.MaxExecutionTime)
			if err != nil {
				return nil, fmt.Errorf("invalid max_execution_time in execution policy: %w", err)
			}
			policy.MaxExecutionTime = dur
		}
		policy.StopOnFailure = configPolicy.StopOnFailure
	} else {
		return nil, fmt.Errorf("execution policy '%s' not found in config", config.ErrorHandling.ExecutionPolicy)
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

	result := RulesetResult{
		RulesetName: rulesetName,
		RuleResults: make(map[string]RuleResult),
	}

	// Handle rule inheritance
	var allRules []string
	if ruleset.Extends != "" {
		baseRuleset, bRuleOk := re.config.Rulesets[ruleset.Extends]
		if bRuleOk {
			allRules = append(allRules, baseRuleset.Rules...)
		}
	}
	allRules = append(allRules, ruleset.Rules...)

	// Evaluate individual rules
	for _, ruleRef := range allRules {
		ruleResult, err := re.EvaluateRule(ruleRef)
		if err != nil {
			result.Error = err
			result.Duration = time.Since(start)
			return result, nil
		}
		result.RuleResults[ruleRef] = ruleResult
	}

	// Evaluate custom rules within the ruleset
	if ruleset.CustomRules != nil {
		for customRuleName := range ruleset.CustomRules {
			fullName := fmt.Sprintf("%s.%s", rulesetName, customRuleName)
			program, pOk := re.programs[fullName]
			if !pOk {
				continue
			}

			out, _, err := program.Eval(re.context)
			if err != nil {
				result.RuleResults[customRuleName] = RuleResult{
					RuleName: customRuleName,
					Passed:   false,
					Error:    err,
					Duration: time.Since(start),
				}
				continue
			}

			value := out.Value()
			passed := false
			if boolVal, ok := value.(bool); ok {
				passed = boolVal
			}

			result.RuleResults[customRuleName] = RuleResult{
				RuleName: customRuleName,
				Passed:   passed,
				Duration: time.Since(start),
			}
		}
	}

	// Evaluate based on combination type
	switch ruleset.CombinationType {
	case combinationTypeAnd:
		result.Passed = true
		for _, ruleResult := range result.RuleResults {
			if !ruleResult.Passed {
				result.Passed = false
				if re.policy.StopOnFailure {
					break
				}
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
				if re.policy.StopOnFailure {
					break
				}
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

			re.programs[fmt.Sprintf("ruleset.%s", rulesetName)] = program
		}
	}

	return nil
}
