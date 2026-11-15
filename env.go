package ruleengine

import (
	"fmt"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/overloads"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// NewDefaultEnv creates a CEL environment pre-configured with "globals" (DynType),
// the provided variable names (DynType), and standard "timestamp" and "now" functions.
func NewDefaultEnv(variableNames ...string) (*cel.Env, error) {
	seen := map[string]bool{"globals": true}
	envOpts := make([]cel.EnvOption, 0, len(variableNames)+3)
	envOpts = append(envOpts, cel.Variable("globals", cel.DynType))

	for _, name := range variableNames {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		envOpts = append(envOpts, cel.Variable(name, cel.DynType))
	}

	envOpts = append(envOpts,
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

	env, err := cel.NewEnv(envOpts...)
	if err != nil {
		return nil, fmt.Errorf("creating default CEL env: %w", err)
	}
	return env, nil
}
