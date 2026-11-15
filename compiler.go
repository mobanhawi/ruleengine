package ruleengine

import "context"

// EvalFunc evaluates a compiled boolean rule expression against an activation map.
// Implementations must be safe for concurrent invocation by multiple goroutines.
type EvalFunc = func(ctx context.Context, vars map[string]any) (bool, error)

// Compiler compiles a rule expression string into a concurrent-safe EvalFunc.
type Compiler interface {
	Compile(expression string) (EvalFunc, error)
}
