package ruleengine

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/overloads"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// setupEnvironment cel.Env helper
func setupEnvironment() func(*testing.T) *cel.Env {
	return func(t *testing.T) *cel.Env {
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
		return env
	}
}

// Create rule engine
//engine, err := NewRuleEngine("rules.yml", "development", env)
//if err != nil {
//	t.Fatalf("failed to create rules engine: %v", err)
//}

//// Set up test context data
//testContext := map[string]interface{}{
//	"user": map[string]interface{}{
//		"age":       25,
//		"email":     "test@example.com",
//		"status":    "active",
//		"suspended": false,
//	},
//	"request": map[string]interface{}{
//		"time":    time.Now().Format(time.RFC3339),
//		"attempt": 2,
//	},
//}
//engine.SetContext(testContext)

func TestRuleEngine_EvaluateRule(t *testing.T) {
	type fields struct {
		config   *RulesetConfig
		env      *cel.Env
		programs map[string]cel.Program
		policy   Policy
		context  map[string]interface{}
	}
	type args struct {
		ruleName string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    RuleResult
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re := &RuleEngine{
				config:   tt.fields.config,
				env:      tt.fields.env,
				programs: tt.fields.programs,
				policy:   tt.fields.policy,
				context:  tt.fields.context,
			}
			got, err := re.EvaluateRule(tt.args.ruleName)
			if (err != nil) != tt.wantErr {
				t.Errorf("EvaluateRule() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("EvaluateRule() got = %v, want %v", got, tt.want)
			}
		})
	}
}
