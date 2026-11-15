package cel

import (
	"context"
	"errors"
	"testing"
)

func TestNewCompiler(t *testing.T) {
	t.Run("GivenNilEnv_WhenNewCompiler_ThenReturnsError", func(t *testing.T) {
		if _, err := NewCompiler(nil); err == nil {
			t.Fatal("NewCompiler(nil) expected error, got nil")
		}
	})

	t.Run("GivenValidEnvAndOptions_WhenCompileAndEvaluate_ThenEvaluatesBooleanExpression", func(t *testing.T) {
		env, err := NewDefaultEnv("user")
		if err != nil {
			t.Fatalf("NewDefaultEnv() error = %v", err)
		}
		compiler, err := NewCompiler(env, WithOptimise(), WithExhaustiveEval())
		if err != nil {
			t.Fatalf("NewCompiler() error = %v", err)
		}

		evalFn, err := compiler.Compile("user.age >= globals.min_age && now() >= timestamp('2020-01-01T00:00:00Z')")
		if err != nil {
			t.Fatalf("Compile() error = %v", err)
		}

		passed, err := evalFn(context.Background(), map[string]any{
			"user":    map[string]any{"age": 21},
			"globals": map[string]any{"min_age": 18},
		})
		if err != nil || !passed {
			t.Fatalf("evalFn() = (%v, %v), want (true, nil)", passed, err)
		}
	})

	t.Run("GivenStaticNonBooleanExpression_WhenCompile_ThenReturnsErrNonBooleanExpression", func(t *testing.T) {
		env, err := NewDefaultEnv("user")
		if err != nil {
			t.Fatalf("NewDefaultEnv() error = %v", err)
		}
		compiler, err := NewCompiler(env)
		if err != nil {
			t.Fatalf("NewCompiler() error = %v", err)
		}

		if _, err := compiler.Compile("1 + 2"); !errors.Is(err, ErrNonBooleanExpression) {
			t.Fatalf("Compile('1 + 2') error = %v, want errors.Is ErrNonBooleanExpression", err)
		}
	})

	t.Run("GivenDynamicNonBooleanValueAtRuntime_WhenEvaluated_ThenReturnsErrNonBooleanExpression", func(t *testing.T) {
		env, err := NewDefaultEnv("user")
		if err != nil {
			t.Fatalf("NewDefaultEnv() error = %v", err)
		}
		compiler, err := NewCompiler(env)
		if err != nil {
			t.Fatalf("NewCompiler() error = %v", err)
		}

		evalFn, err := compiler.Compile("user.name")
		if err != nil {
			t.Fatalf("Compile('user.name') error = %v", err)
		}

		_, err = evalFn(context.Background(), map[string]any{
			"user":    map[string]any{"name": "alice"},
			"globals": map[string]any{},
		})
		if !errors.Is(err, ErrNonBooleanExpression) {
			t.Fatalf("evalFn() error = %v, want errors.Is ErrNonBooleanExpression", err)
		}
	})
}
