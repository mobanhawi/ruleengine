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
	type args struct {
		context map[string]interface{}
	}
	tests := []struct {
		name       string
		ruleengine func(*testing.B) *RuleEngine
		args       args
		want       map[string]RulesetResult
		wantErr    bool
	}{
		{
			name: "success",
			ruleengine: func(t *testing.B) *RuleEngine {
				env := setupBenchmarkEnvironment()(t)
				engine, err := NewRuleEngine("./testdata/rules_bench.yml", "development", env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				context: map[string]interface{}{
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
				},
			},
			want: map[string]RulesetResult{
				"user_registration": {
					RulesetName: "user_registration",
					Passed:      true,
					RuleResults: map[string]RuleResult{
						"age_validation": {
							RuleName: "age_validation",
							Passed:   true,
							Error:    nil,
							Duration: 0,
						},
						"email_format": {
							RuleName: "email_format",
							Passed:   true,
							Error:    nil,
							Duration: 0,
						},
						"user_status": {
							RuleName: "user_status",
							Passed:   true,
							Error:    nil,
							Duration: 0,
						},
					},
					Error:    nil,
					Duration: 0,
				},
				"request_throttling": {
					RulesetName: "request_throttling",
					Passed:      true,
					RuleResults: map[string]RuleResult{
						"rate_limiting": {
							RuleName: "rate_limiting",
							Passed:   true,
							Error:    nil,
							Duration: 0,
						},
						"user_tier": {
							RuleName: "user_tier",
							Passed:   false,
							Error:    nil,
							Duration: 0,
						},
					},
					Error:    nil,
					Duration: 0,
				},
				"domain_whitelist": {
					RulesetName: "domain_whitelist",
					Passed:      true,
					RuleResults: map[string]RuleResult{
						"email_format": {
							RuleName: "email_format",
							Passed:   true,
							Error:    nil,
							Duration: 0,
						},
						"ruleset.domain_whitelist": {
							RuleName: "ruleset.domain_whitelist",
							Passed:   true,
							Duration: 0,
						},
					},
					Error:    nil,
					Duration: 0,
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		b.Run(tt.name, func(t *testing.B) {
			re := tt.ruleengine(t)
			t.ResetTimer()
			re.SetContext(tt.args.context)
			_, _ = re.EvaluateAllRulesets()
		})
	}
}

func BenchmarkNewRuleEngine(t *testing.B) {
	type args struct {
		configPath  string
		environment string
		envProvider func(*testing.B) *cel.Env
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "success",
			args: args{
				configPath:  "./testdata/rules_bench.yml",
				envProvider: setupBenchmarkEnvironment(),
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.B) {
			_, err := NewRuleEngine(tt.args.configPath, tt.args.environment, tt.args.envProvider(t))
			if (err != nil) != tt.wantErr {
				t.Errorf("NewRuleEngine() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func BenchmarkRuleEngineOptimise_EvaluateAllRulesets(b *testing.B) {
	type args struct {
		context map[string]interface{}
	}
	tests := []struct {
		name       string
		ruleengine func(*testing.B) *RuleEngine
		args       args
		want       map[string]RulesetResult
		wantErr    bool
	}{
		{
			name: "success",
			ruleengine: func(t *testing.B) *RuleEngine {
				env := setupBenchmarkEnvironment()(t)
				engine, err := NewRuleEngine("./testdata/rules_bench.yml", "development", env, WithOptimise(true))
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				context: map[string]interface{}{
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
				},
			},
			want: map[string]RulesetResult{
				"user_registration": {
					RulesetName: "user_registration",
					Passed:      true,
					RuleResults: map[string]RuleResult{
						"age_validation": {
							RuleName: "age_validation",
							Passed:   true,
							Error:    nil,
							Duration: 0,
						},
						"email_format": {
							RuleName: "email_format",
							Passed:   true,
							Error:    nil,
							Duration: 0,
						},
						"user_status": {
							RuleName: "user_status",
							Passed:   true,
							Error:    nil,
							Duration: 0,
						},
					},
					Error:    nil,
					Duration: 0,
				},
				"request_throttling": {
					RulesetName: "request_throttling",
					Passed:      true,
					RuleResults: map[string]RuleResult{
						"rate_limiting": {
							RuleName: "rate_limiting",
							Passed:   true,
							Error:    nil,
							Duration: 0,
						},
						"user_tier": {
							RuleName: "user_tier",
							Passed:   false,
							Error:    nil,
							Duration: 0,
						},
					},
					Error:    nil,
					Duration: 0,
				},
				"domain_whitelist": {
					RulesetName: "domain_whitelist",
					Passed:      true,
					RuleResults: map[string]RuleResult{
						"email_format": {
							RuleName: "email_format",
							Passed:   true,
							Error:    nil,
							Duration: 0,
						},
						"ruleset.domain_whitelist": {
							RuleName: "ruleset.domain_whitelist",
							Passed:   true,
							Duration: 0,
						},
					},
					Error:    nil,
					Duration: 0,
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		b.Run(tt.name, func(t *testing.B) {
			re := tt.ruleengine(t)
			t.ResetTimer()
			re.SetContext(tt.args.context)
			_, _ = re.EvaluateAllRulesets()
		})
	}
}

func BenchmarkNewRuleEngineOptimise(t *testing.B) {
	type args struct {
		configPath  string
		environment string
		envProvider func(*testing.B) *cel.Env
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "success",
			args: args{
				configPath:  "./testdata/rules_bench.yml",
				envProvider: setupBenchmarkEnvironment(),
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.B) {
			_, err := NewRuleEngine(tt.args.configPath, tt.args.environment, tt.args.envProvider(t), WithOptimise(true))
			if (err != nil) != tt.wantErr {
				t.Errorf("NewRuleEngine() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}
