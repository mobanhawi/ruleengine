package ruleengine

import (
	"errors"
	"testing"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/overloads"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
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
				engine, err := NewRuleEngine("./testdata/rules.yml", "development", env)
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
				engine, err := NewRuleEngine("./testdata/rules.yml", "production", env)
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
				engine, err := NewRuleEngine("./testdata/rules.yml", "production", env)
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
			want:    RuleResult{},
			wantErr: true,
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
			diff := cmp.Diff(got, tt.want,
				cmpopts.IgnoreFields(RuleResult{}, "Duration"),
				cmp.Comparer(func(x, y error) bool {
					return (x == nil && y == nil) || (x != nil && y != nil && x.Error() == y.Error())
				}),
			)
			if diff != "" {
				t.Errorf("EvaluateRule() (-got +want):\n%s", diff)
			}
		})
	}
}

func TestRuleEngine_EvaluateRuleset(t *testing.T) {
	type args struct {
		rulesetName string
		context     map[string]interface{}
	}
	tests := []struct {
		name       string
		ruleengine func(*testing.T) *RuleEngine
		args       args
		want       RulesetResult
		wantErr    bool
	}{
		{
			name: "success - user_registration(AND) - dev",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine("./testdata/rules.yml", "development", env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				rulesetName: "user_registration",
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
			want: RulesetResult{
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
			wantErr: false,
		},
		{
			name: "success - extension logic - dev",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine("./testdata/rules.yml", "development", env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				rulesetName: "domain_whitelist",
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
			want: RulesetResult{
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
			wantErr: false,
		},
		{
			name: "fail - extension logic - fail main rule - dev",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine("./testdata/rules.yml", "development", env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				rulesetName: "domain_whitelist",
				context: map[string]interface{}{
					"user": map[string]interface{}{
						"age":       15,
						"email":     "test.example.com",
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
			want: RulesetResult{
				RulesetName: "domain_whitelist",
				Passed:      false,
				RuleResults: map[string]RuleResult{
					"email_format": {
						RuleName: "email_format",
						Passed:   false,
						Error:    errors.New("please provide a valid email address"),
						Duration: 0,
					},
					"ruleset.domain_whitelist": {
						RuleName: "ruleset.domain_whitelist",
						Passed:   false,
						Duration: 0,
					},
				},
				Error:    errors.New("email domain is not allowed"),
				Duration: 0,
			},
			wantErr: false,
		},
		{
			name: "fail - extension logic - fail expression - dev",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine("./testdata/rules.yml", "development", env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				rulesetName: "domain_whitelist",
				context: map[string]interface{}{
					"user": map[string]interface{}{
						"age":       15,
						"email":     "test@badexample.com",
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
			want: RulesetResult{
				RulesetName: "domain_whitelist",
				Passed:      false,
				RuleResults: map[string]RuleResult{
					"email_format": {
						RuleName: "email_format",
						Passed:   true,
						Error:    nil,
						Duration: 0,
					},
					"ruleset.domain_whitelist": {
						RuleName: "ruleset.domain_whitelist",
						Passed:   false,
						Duration: 0,
					},
				},
				Error:    errors.New("email domain is not allowed"),
				Duration: 0,
			},
			wantErr: false,
		},
		{
			name: "fail(collect_all) - user_registration(AND) - age_validation - dev",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine("./testdata/rules.yml", "development", env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				rulesetName: "user_registration",
				context: map[string]interface{}{
					"user": map[string]interface{}{
						"age":       10,
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
			want: RulesetResult{
				RulesetName: "user_registration",
				Passed:      false,
				RuleResults: map[string]RuleResult{
					"age_validation": {
						RuleName: "age_validation",
						Passed:   false,
						Error:    errors.New("user must be at least 18 years old"),
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
			wantErr: false,
		},
		{
			name: "fail(fast) - user_registration(AND) - age_validation - prod",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine("./testdata/rules.yml", "production", env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				rulesetName: "user_registration",
				context: map[string]interface{}{
					"user": map[string]interface{}{
						"age":       16,
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
			want: RulesetResult{
				RulesetName: "user_registration",
				Passed:      false,
				RuleResults: map[string]RuleResult{
					"age_validation": {
						RuleName: "age_validation",
						Passed:   false,
						Error:    errors.New("user must be at least 18 years old"),
						Duration: 0,
					},
				},
				Error:    nil,
				Duration: 0,
			},
			wantErr: false,
		},
		{
			name: "success - request_throttling(OR) - rate_limiting - prod",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine("./testdata/rules.yml", "production", env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				rulesetName: "request_throttling",
				context: map[string]interface{}{
					"user": map[string]interface{}{
						"age":       16,
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
			want: RulesetResult{
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
			wantErr: false,
		},
		{
			name: "success - request_throttling(OR) - user_tier - dev",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine("./testdata/rules.yml", "development", env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				rulesetName: "request_throttling",
				context: map[string]interface{}{
					"user": map[string]interface{}{
						"age":       16,
						"email":     "test@example.com",
						"status":    "active",
						"suspended": false,
						"tier":      "premium",
					},
					"request": map[string]interface{}{
						"time":    time.Now().Format(time.RFC3339),
						"attempt": 25,
					},
				},
			},
			want: RulesetResult{
				RulesetName: "request_throttling",
				Passed:      true,
				RuleResults: map[string]RuleResult{
					"rate_limiting": {
						RuleName: "rate_limiting",
						Passed:   false,
						Error:    nil,
						Duration: 0,
					},
					"user_tier": {
						RuleName: "user_tier",
						Passed:   true,
						Error:    nil,
						Duration: 0,
					},
				},
				Error:    nil,
				Duration: 0,
			},
			wantErr: false,
		},
		{
			name: "fail - request_throttling(OR) - dev",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine("./testdata/rules.yml", "development", env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				rulesetName: "request_throttling",
				context: map[string]interface{}{
					"user": map[string]interface{}{
						"age":       16,
						"email":     "test@example.com",
						"status":    "active",
						"suspended": false,
						"tier":      "free",
					},
					"request": map[string]interface{}{
						"time":    time.Now().Format(time.RFC3339),
						"attempt": 25,
					},
				},
			},
			want: RulesetResult{
				RulesetName: "request_throttling",
				Passed:      false,
				RuleResults: map[string]RuleResult{
					"rate_limiting": {
						RuleName: "rate_limiting",
						Passed:   false,
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
				Error:    errors.New("too many requests, please try again later"),
				Duration: 0,
			},
			wantErr: false,
		},
		{
			name: "fail - unknown_ruleset",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine("./testdata/rules.yml", "development", env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				rulesetName: "unknown_ruleset",
				context: map[string]interface{}{
					"user": map[string]interface{}{
						"age":       16,
						"email":     "test@example.com",
						"status":    "active",
						"suspended": false,
						"tier":      "free",
					},
					"request": map[string]interface{}{
						"time":    time.Now().Format(time.RFC3339),
						"attempt": 25,
					},
				},
			},
			want:    RulesetResult{},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re := tt.ruleengine(t)
			re.SetContext(tt.args.context)
			got, err := re.EvaluateRuleset(tt.args.rulesetName)
			if (err != nil) != tt.wantErr {
				t.Errorf("EvaluateRuleset() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			diff := cmp.Diff(got, tt.want,
				cmpopts.IgnoreFields(RuleResult{}, "Duration"),
				cmpopts.IgnoreFields(RulesetResult{}, "Duration"),
				cmp.Comparer(func(x, y error) bool {
					return (x == nil && y == nil) || (x != nil && y != nil && x.Error() == y.Error())
				}),
			)
			if diff != "" {
				t.Errorf("EvaluateRuleset() (-got +want):\n%s", diff)
			}
		})
	}
}

func TestRuleEngine_EvaluateAllRulesets(t *testing.T) {
	type args struct {
		context map[string]interface{}
	}
	tests := []struct {
		name       string
		ruleengine func(*testing.T) *RuleEngine
		args       args
		want       map[string]RulesetResult
		wantErr    bool
	}{
		{
			name: "success",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine("./testdata/rules.yml", "development", env)
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
		{
			name: "fail - timeout - 1ns",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine("./testdata/rules.yml", "production", env)
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
			want:    map[string]RulesetResult{},
			wantErr: true,
		},
		{
			name: "fail - one ruleset fails",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine("./testdata/rules.yml", "development", env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				context: map[string]interface{}{
					"user": map[string]interface{}{
						"age":       5,
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
					Passed:      false,
					RuleResults: map[string]RuleResult{
						"age_validation": {
							RuleName: "age_validation",
							Passed:   false,
							Error:    errors.New("user must be at least 18 years old"),
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
		t.Run(tt.name, func(t *testing.T) {
			re := tt.ruleengine(t)
			re.SetContext(tt.args.context)
			got, err := re.EvaluateAllRulesets()
			if (err != nil) != tt.wantErr {
				t.Errorf("EvaluateAllRulesets() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			diff := cmp.Diff(got, tt.want,
				cmpopts.IgnoreFields(RuleResult{}, "Duration"),
				cmpopts.IgnoreFields(RulesetResult{}, "Duration"),
				cmp.Comparer(func(x, y error) bool {
					return (x == nil && y == nil) || (x != nil && y != nil && x.Error() == y.Error())
				}),
			)
			if diff != "" {
				t.Errorf("EvaluateAllRulesets() (-got +want):\n%s", diff)
			}
		})
	}
}

func TestNewRuleEngine(t *testing.T) {
	type args struct {
		configPath  string
		environment string
		envProvider func(*testing.T) *cel.Env
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "fail - bad path",
			args: args{
				configPath:  "test.yml",
				envProvider: setupEnvironment(),
			},
			wantErr: true,
		},
		{
			name: "fail - bad cel env",
			args: args{
				configPath: "./testdata/rules.yml",
				envProvider: func(t *testing.T) *cel.Env {
					return nil
				},
			},
			wantErr: true,
		},
		{
			name: "fail - bad policy",
			args: args{
				configPath:  "./testdata/bad_policy.yml",
				envProvider: setupEnvironment(),
			},
			wantErr: true,
		},
		{
			name: "fail - bad rules",
			args: args{
				configPath:  "./testdata/bad_rules.yml",
				envProvider: setupEnvironment(),
			},
			wantErr: true,
		},
		{
			name: "success",
			args: args{
				configPath:  "./testdata/rules.yml",
				envProvider: setupEnvironment(),
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewRuleEngine(tt.args.configPath, tt.args.environment, tt.args.envProvider(t))
			if (err != nil) != tt.wantErr {
				t.Errorf("NewRuleEngine() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}
