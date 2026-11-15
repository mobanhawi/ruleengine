package cel

import (
	"context"
	"errors"
	"fmt"

	celgo "github.com/google/cel-go/cel"
)

// defaultInterruptCheckFrequency is how many comprehension iterations CEL runs between
// context cancellation checks during ContextEval.
const defaultInterruptCheckFrequency = 100

// ErrNonBooleanExpression is returned when a rule expression does not evaluate to a boolean.
var ErrNonBooleanExpression = errors.New("rule expression must return a boolean")

// EvalFunc evaluates a compiled boolean expression against an activation map.
type EvalFunc = func(ctx context.Context, vars map[string]any) (bool, error)

// Compiler compiles CEL expressions into concurrent-safe boolean evaluation functions.
type Compiler struct {
	env        *celgo.Env
	optimise   bool
	exhaustive bool
}

// Option configures a CEL Compiler.
type Option func(*Compiler)

// WithOptimise enables static AST optimization when compiling CEL programs.
func WithOptimise() Option {
	return func(c *Compiler) {
		c.optimise = true
	}
}

// WithExhaustiveEval disables short-circuiting of logical operators (&&, ||) in CEL.
func WithExhaustiveEval() Option {
	return func(c *Compiler) {
		c.exhaustive = true
	}
}

// NewCompiler creates a new CEL Compiler bound to the given cel.Env.
func NewCompiler(env *celgo.Env, opts ...Option) (*Compiler, error) {
	if env == nil {
		return nil, errors.New("cel environment cannot be nil")
	}
	c := &Compiler{env: env}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// Compile parses, type-checks, and compiles a CEL expression into an EvalFunc.
// It rejects expressions whose static or dynamic return type is not boolean.
func (c *Compiler) Compile(expression string) (EvalFunc, error) {
	ast, issues := c.env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("failed to compile expression '%s': %w", expression, issues.Err())
	}

	outType := ast.OutputType()
	if outType != celgo.BoolType && outType != celgo.DynType {
		return nil, fmt.Errorf("expression %q returns %s: %w", expression, outType, ErrNonBooleanExpression)
	}

	var evalOpts celgo.EvalOption
	if c.exhaustive {
		evalOpts |= celgo.OptExhaustiveEval
	}
	if c.optimise {
		evalOpts |= celgo.OptOptimize
	}
	programOpts := []celgo.ProgramOption{celgo.InterruptCheckFrequency(defaultInterruptCheckFrequency)}
	if evalOpts != 0 {
		programOpts = append(programOpts, celgo.EvalOptions(evalOpts))
	}

	program, err := c.env.Program(ast, programOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create program for expression '%s': %w", expression, err)
	}

	return func(ctx context.Context, vars map[string]any) (bool, error) {
		out, _, evalErr := program.ContextEval(ctx, vars)
		if evalErr != nil {
			return false, evalErr
		}
		boolVal, ok := out.Value().(bool)
		if !ok {
			return false, fmt.Errorf("evaluated to %T: %w", out.Value(), ErrNonBooleanExpression)
		}
		return boolVal, nil
	}, nil
}
