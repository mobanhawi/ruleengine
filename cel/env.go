package cel

import (
	"fmt"
	"time"

	celgo "github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/overloads"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// NewDefaultEnv creates a standard CEL environment pre-configured with "globals",
// the built-in "timestamp(string)" and "now()" functions, and any custom variable names
// (such as "user", "request") declared as dynamic types.
func NewDefaultEnv(customVars ...string) (*celgo.Env, error) {
	opts := make([]celgo.EnvOption, 0, len(customVars)+3)
	opts = append(opts, celgo.Variable("globals", celgo.DynType))

	seen := map[string]bool{"globals": true}
	for _, v := range customVars {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		opts = append(opts, celgo.Variable(v, celgo.DynType))
	}

	opts = append(opts,
		celgo.Function("timestamp",
			celgo.Overload(overloads.StringToTimestamp, []*celgo.Type{celgo.StringType}, celgo.TimestampType,
				celgo.UnaryBinding(func(val ref.Val) ref.Val {
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
		celgo.Function("now",
			celgo.Overload("now", []*celgo.Type{}, celgo.TimestampType,
				celgo.FunctionBinding(func(args ...ref.Val) ref.Val {
					return types.Timestamp{Time: time.Now()}
				}),
			),
		),
	)

	env, err := celgo.NewEnv(opts...)
	if err != nil {
		return nil, fmt.Errorf("creating default CEL environment: %w", err)
	}
	return env, nil
}
