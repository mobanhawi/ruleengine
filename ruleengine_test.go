package ruleengine

import (
	"context"
	"fmt"
	"log"
	"testing"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/overloads"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// ruleEval tests individual rule evaluation
func ruleEval(engine *RuleEngine) RuleResult {
	fmt.Println("=== Individual Rule Evaluation ===")
	ruleResult, err := engine.EvaluateRule("age_validation")
	if err != nil {
		log.Printf("Error evaluating age_validation rule: %v\n", err)
	} else {
		fmt.Printf("Rule: %s, Passed: %v, Duration: %v\n",
			ruleResult.RuleName, ruleResult.Passed, ruleResult.Duration)
	}
	return ruleResult
}

// rulesetEval tests individual ruleset evaluation
func rulesetEval(engine *RuleEngine) []RulesetResult {
	fmt.Println("=== Ruleset Evaluation ===")
	rulesets := []string{"user_registration"}
	results := make([]RulesetResult, len(rulesets))
	for i, rulesetName := range rulesets {
		result, err := engine.EvaluateRuleset(rulesetName)
		if err != nil {
			log.Printf("Error evaluating ruleset %s: %v\n", rulesetName, err)
			continue
		}

		fmt.Printf("\nRuleset: %s\n", result.RulesetName)
		fmt.Printf("  Passed: %v\n", result.Passed)
		fmt.Printf("  Duration: %v\n", result.Duration)

		if result.Error != nil {
			fmt.Printf("  Error: %v\n", result.Error)
		}

		fmt.Println("  Individual Rules:")
		for ruleName, ruleResult := range result.RuleResults {
			fmt.Printf("    %s: %v", ruleName, ruleResult.Passed)
			if ruleResult.Error != nil {
				fmt.Printf(" (Error: %v)", ruleResult.Error)
			}
			fmt.Println()
		}
		results[i] = result
	}
	return results
}

// allRulesEval tests all rulesets
func allRulesEval(engine *RuleEngine) map[string]RulesetResult {
	fmt.Println("\n=== All Rulesets Evaluation ===")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second) // TODO: use execution policy timeout
	defer cancel()

	allResults, err := engine.EvaluateAllRulesets(ctx)
	if err != nil {
		log.Printf("Error evaluating all rulesets: %v", err)
	} else {
		fmt.Printf("Evaluated %d rulesets successfully\n", len(allResults))
		for name, result := range allResults {
			status := "PASS"
			if !result.Passed {
				status = "FAIL"
			}
			fmt.Printf("  %s: %s (%.2fms)\n", name, status, float64(result.Duration.Nanoseconds())/1e6)
		}
	}
	return allResults
}

// Example usage and testing
func TestRuleEngine(t *testing.T) {

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
		t.Fatalf("failed to create CEL environment: %v\n", err)
	}

	// Create rule engine
	engine, err := NewRuleEngine("rules.yml", "development", env)
	if err != nil {
		t.Fatalf("failed to create rules engine: %v", err)
	}

	// Set up test context data
	testContext := map[string]interface{}{
		"user": map[string]interface{}{
			"age":       25,
			"email":     "test@example.com",
			"status":    "active",
			"suspended": false,
		},
		"request": map[string]interface{}{
			"time":    time.Now().Format(time.RFC3339),
			"attempt": 2,
		},
	}
	engine.SetContext(testContext)

	t.Run("ruleEval", func(t *testing.T) {
		r := ruleEval(engine)
		if !r.Passed {
			t.Errorf("rule evaluation failed: %v", r.Error)
		}
	})

	t.Run("rulesetEval", func(t *testing.T) {
		results := rulesetEval(engine)
		for _, r := range results {
			if !r.Passed {
				t.Errorf("rule evaluation failed: %v", r.Error)
			}
		}

	})
	t.Run("allRulesEval", func(t *testing.T) {
		results := allRulesEval(engine)
		for _, r := range results {
			if !r.Passed {
				t.Errorf("rule evaluation failed: %v", r.Error)
			}
		}
	})

}
