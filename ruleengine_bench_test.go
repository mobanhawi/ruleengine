package ruleengine

import (
	"context"
	"testing"
	"time"

	"github.com/google/cel-go/cel"
)

const testBenchRulesYAMLPath = "./testdata/rules_bench.yml"

func setupBenchmarkEnvironment(b *testing.B) *cel.Env {
	b.Helper()
	env, err := NewDefaultEnv("user", "request")
	if err != nil {
		b.Fatalf("failed to create CEL environment: %v", err)
	}
	return env
}

func BenchmarkRuleEngineEvaluateAllRulesets(b *testing.B) {
	env := setupBenchmarkEnvironment(b)
	engine, err := NewRuleEngine(testBenchRulesYAMLPath, envDevelopment, env)
	if err != nil {
		b.Fatalf("failed to create rules engine: %v", err)
	}
	input := map[string]any{
		"user": map[string]any{
			"age":       testUserAgeTeen,
			"email":     "test@example.com",
			"status":    "active",
			"suspended": false,
			"tier":      "free",
		},
		"request": map[string]any{
			"time":    time.Now().Format(time.RFC3339),
			"attempt": testAttemptLow,
		},
	}
	ctx := context.Background()
	for b.Loop() {
		_, _ = engine.EvaluateAllRulesets(ctx, input)
	}
}

func BenchmarkNewRuleEngine(b *testing.B) {
	celEnv := setupBenchmarkEnvironment(b)
	for b.Loop() {
		_, _ = NewRuleEngine(testBenchRulesYAMLPath, "env", celEnv)
	}
}

func BenchmarkRuleEngineOptimiseEvaluateAllRulesets(b *testing.B) {
	env := setupBenchmarkEnvironment(b)
	engine, err := NewRuleEngine(testBenchRulesYAMLPath, envDevelopment, env, WithOptimise())
	if err != nil {
		b.Fatalf("failed to create rules engine: %v", err)
	}
	input := map[string]any{
		"user": map[string]any{
			"age":       testUserAgeTeen,
			"email":     "test@example.com",
			"status":    "active",
			"suspended": false,
			"tier":      "free",
		},
		"request": map[string]any{
			"time":    time.Now().Format(time.RFC3339),
			"attempt": testAttemptLow,
		},
	}
	ctx := context.Background()
	for b.Loop() {
		_, _ = engine.EvaluateAllRulesets(ctx, input)
	}
}

func BenchmarkNewRuleEngineOptimise(b *testing.B) {
	celEnv := setupBenchmarkEnvironment(b)
	for b.Loop() {
		_, _ = NewRuleEngine(testBenchRulesYAMLPath, "env", celEnv, WithOptimise())
	}
}
