package main

import (
	"context"
	"fmt"
	"log"
	"time"
)

// ruleEval tests individual rule evaluation
func ruleEval(engine *RuleEngine) {
	fmt.Println("=== Individual Rule Evaluation ===")
	ruleResult, err := engine.EvaluateRule("age_validation")
	if err != nil {
		log.Printf("Error evaluating age_validation rule: %v\n", err)
	} else {
		fmt.Printf("Rule: %s, Passed: %v, Duration: %v\n",
			ruleResult.RuleName, ruleResult.Passed, ruleResult.Duration)
	}
}

// rulesetEval tests individual ruleset evaluation
func rulesetEval(engine *RuleEngine) {
	fmt.Println("=== Ruleset Evaluation ===")
	rulesets := []string{"user_registration"}

	for _, rulesetName := range rulesets {
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
	}
}

// allRulesEval tests all rulesets
func allRulesEval(engine *RuleEngine) {
	fmt.Println("\n=== All Rulesets Evaluation ===")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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
}

// Example usage and testing
func main() {
	// Create rule engine
	engine, err := NewRuleEngine("rules.yml", "development")
	if err != nil {
		log.Fatalf("Failed to create rule engine: %v", err)
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

	ruleEval(engine)
	rulesetEval(engine)
	allRulesEval(engine)

}
