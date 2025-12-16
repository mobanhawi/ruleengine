package ruleengine

import (
	"testing"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/overloads"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// setupBenchmarkEnvironment cel.Env helper
func setupBenchmarkEnvironment() func(*testing.B) *cel.Env {
	return func(t *testing.B) *cel.Env {
		// Create CEL environment with standard functions and custom variables
		// Most CEL applications will declare variables that can be referenced within expressions.
		// Declarations of variables specify a name and a type.
		// A variable's type may either be a CEL builtin type, a protocol buffer well-known type,
		// or any protobuf message type so long as its descriptor is also provided to CEL
		env, err := cel.NewEnv(
			cel.Variable("user", cel.DynType),
			cel.Variable("request", cel.DynType),
			cel.Variable("globals", cel.DynType),
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
		return env
	}
}

func BenchmarkRuleEngine_EvaluateAllRulesets(b *testing.B) {
	env := setupBenchmarkEnvironment()(b)
	engine, err := NewRuleEngine("./testdata/rules_bench.yml", "development", env)
	if err != nil {
		b.Fatalf("failed to create rules engine: %v", err)
	}
	context := map[string]interface{}{
		"user": map[string]interface{}{
			"age":       15,
			"email":     "test@example.com",
			"status":    "active",
			"suspended": false,
			"tier":      "free",
		},
		"request": map[string]interface{}{
			"time":    time.Now().Format(time.RFC3339),
			"attempt": 2,
		},
	}
	for b.Loop() {
		engine.SetContext(context)
		_, _ = engine.EvaluateAllRulesets()
	}
}

func BenchmarkNewRuleEngine(t *testing.B) {
	celEnv := setupBenchmarkEnvironment()(t)
	for t.Loop() {
		_, _ = NewRuleEngine("./testdata/rules_bench.yml", "env", celEnv)
	}
}

func BenchmarkRuleEngineOptimise_EvaluateAllRulesets(b *testing.B) {
	env := setupBenchmarkEnvironment()(b)
	engine, err := NewRuleEngine("./testdata/rules_bench.yml", "development", env, WithOptimise())
	if err != nil {
		b.Fatalf("failed to create rules engine: %v", err)
	}
	context := map[string]interface{}{
		"user": map[string]interface{}{
			"age":       15,
			"email":     "test@example.com",
			"status":    "active",
			"suspended": false,
			"tier":      "free",
		},
		"request": map[string]interface{}{
			"time":    time.Now().Format(time.RFC3339),
			"attempt": 2,
		},
	}
	for b.Loop() {
		engine.SetContext(context)
		_, _ = engine.EvaluateAllRulesets()
	}
}

func BenchmarkNewRuleEngineOptimise(t *testing.B) {
	celEnv := setupBenchmarkEnvironment()(t)
	for t.Loop() {
		_, _ = NewRuleEngine("./testdata/rules_bench.yml", "env", celEnv, WithOptimise())
	}
}
