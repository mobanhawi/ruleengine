package ruleengine

import (
	"errors"
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

func compareRuleResults(a, b RuleResult) bool {
	if a.RuleName != b.RuleName {
		return false
	}
	if a.Passed != b.Passed {
		return false
	}
	if (a.Error == nil) != (b.Error == nil) {
		return false
	}
	if a.Error != nil {
		if a.Error.Error() != b.Error.Error() {
			return false
		}
	}
	// We don't compare Duration as it can vary
	return true
}

func TestRuleEngine_EvaluateRule(t *testing.T) {
	type args struct {
		ruleName string
		context  map[string]interface{}
	}
	tests := []struct {
		name       string
		ruleengine func(*testing.T) *RuleEngine
		args       args
		want       RuleResult
		wantErr    bool
	}{
		{
			name: "success - age_validation - dev",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine("rules.yml", "development", env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				ruleName: "age_validation",
				context: map[string]interface{}{
					"user": map[string]interface{}{
						"age":       15,
						"email":     "test@example.com",
						"status":    "active",
						"suspended": false,
					},
					"request": map[string]interface{}{
						"time":    time.Now().Format(time.RFC3339),
						"attempt": 2,
					},
				},
			},
			want: RuleResult{
				RuleName: "age_validation",
				Passed:   true,
				Error:    nil,
			},
			wantErr: false,
		},
		{
			name: "fail - age_validation - prod",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine("rules.yml", "production", env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				ruleName: "age_validation",
				context: map[string]interface{}{
					"user": map[string]interface{}{
						"age":       15,
						"email":     "test@example.com",
						"status":    "active",
						"suspended": false,
					},
					"request": map[string]interface{}{
						"time":    time.Now().Format(time.RFC3339),
						"attempt": 2,
					},
				},
			},
			want: RuleResult{
				RuleName: "age_validation",
				Passed:   false,
				Error:    errors.New("user must be at least 18 years old"),
			},
			wantErr: false,
		},
		{
			name: "fail - age_validation - prod - missing name",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine("rules.yml", "production", env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				ruleName: "height_validation",
				context: map[string]interface{}{
					"user": map[string]interface{}{
						"age":       15,
						"email":     "test@example.com",
						"status":    "active",
						"suspended": false,
					},
					"request": map[string]interface{}{
						"time":    time.Now().Format(time.RFC3339),
						"attempt": 2,
					},
				},
			},
			want: RuleResult{
				RuleName: "age_validation",
				Passed:   false,
				Error:    errors.New("rule 'height_validation' not found"),
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re := tt.ruleengine(t)
			re.SetContext(tt.args.context)
			got, err := re.EvaluateRule(tt.args.ruleName)
			if (err != nil) != tt.wantErr {
				t.Errorf("EvaluateRule() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !compareRuleResults(got, tt.want) {
				t.Errorf("EvaluateRule() got = %v, want %v", got, tt.want)
			}
		})
	}
}
