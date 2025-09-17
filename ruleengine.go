package main

import (
	"context"
	"fmt"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/overloads"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// NewRuleEngine creates a new ruleengine instance
func NewRuleEngine(configPath string, environment string) (*RuleEngine, error) {
	config, err := loadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Apply environment-specific overrides
	if env, exists := config.Environments[environment]; exists {
		if env.Globals != nil {
			for k, v := range env.Globals {
				config.Globals[k] = v
			}
		}
	}

	// Create CEL environment with standard functions and custom variables
	// Most CEL applications will declare variables that can be referenced within expressions.
	// Declarations of variables specify a name and a type.
	// A variable's type may either be a CEL builtin type, a protocol buffer well-known type,
	// or any protobuf message type so long as its descriptor is also provided to CEL
	env, err := cel.NewEnv(
		cel.Variable("user", cel.DynType),
		cel.Variable("request", cel.DynType),
		cel.Variable("payment", cel.DynType),
		cel.Variable("globals", cel.DynType),
		cel.Variable("rules", cel.DynType),
		cel.Variable("rulesets", cel.DynType),
		// Add custom functions
		cel.Function("timestamp",
			cel.Overload(overloads.StringToTimestamp, []*cel.Type{cel.StringType}, cel.TimestampType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					str, ok := val.Value().(string)
					if !ok {
						return types.NewErr("timestamp() requires string input")
					}
					t, err := time.Parse(time.RFC3339, str)
					if err != nil {
						return types.NewErr("invalid timestamp format: %v", err)
					}
					return types.Timestamp{Time: t}
				}),
			),
		),
		cel.Function("now",
			cel.Overload("now", []*cel.Type{}, cel.TimestampType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					return types.Timestamp{Time: time.Now()}
				}),
			),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL environment: %w", err)
	}

	engine := &RuleEngine{
		config:   config,
		env:      env,
		programs: make(map[string]cel.Program),
		context:  make(map[string]interface{}),
	}

	// Pre-compile all rule expressions
	err = engine.compileRules()
	if err != nil {
		return nil, fmt.Errorf("failed to compile rules: %w", err)
	}

	return engine, nil
}

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

func (re *RuleEngine) EvaluateRule(ruleName string) (RuleResult, error) {
	start := time.Now()

	_, exists := re.config.Rules[ruleName]
	if !exists {
		return RuleResult{}, fmt.Errorf("rule '%s' not found", ruleName)
	}

	program, exists := re.programs[ruleName]
	if !exists {
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

	return RuleResult{
		RuleName: ruleName,
		Passed:   passed,
		Value:    value,
		Duration: time.Since(start),
	}, nil
}

func (re *RuleEngine) EvaluateRuleset(rulesetName string) (RulesetResult, error) {
	start := time.Now()

	ruleset, exists := re.config.Rulesets[rulesetName]
	if !exists {
		return RulesetResult{}, fmt.Errorf("ruleset '%s' not found", rulesetName)
	}

	result := RulesetResult{
		RulesetName: rulesetName,
		RuleResults: make(map[string]RuleResult),
	}

	// Handle rule inheritance
	var allRules []string
	if ruleset.Extends != "" {
		baseRuleset, exists := re.config.Rulesets[ruleset.Extends]
		if exists {
			allRules = append(allRules, baseRuleset.Rules...)
		}
	}
	allRules = append(allRules, ruleset.Rules...)
	allRules = append(allRules, ruleset.AdditionalRules...)

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
			program, exists := re.programs[fullName]
			if !exists {
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
				Value:    value,
				Duration: time.Since(start),
			}
		}
	}

	// Evaluate based on combination type
	switch ruleset.CombinationType {
	case "AND":
		result.Passed = true
		for _, ruleResult := range result.RuleResults {
			if !ruleResult.Passed {
				result.Passed = false
				break
			}
		}

	case "OR":
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
				break
			}
		}
	}

	result.Duration = time.Since(start)
	return result, nil
}

func (re *RuleEngine) EvaluateAllRulesets(ctx context.Context) (map[string]RulesetResult, error) {
	results := make(map[string]RulesetResult)

	for rulesetName := range re.config.Rulesets {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
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
