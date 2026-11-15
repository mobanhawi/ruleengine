package ruleengine

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

const (
	testBadPolicyYAMLPath   = "./testdata/bad_policy.yml"
	testBadRulesYAMLPath    = "./testdata/bad_rules.yml"
	testBadParentsYAMLPath  = "./testdata/bad_parents.yml"
	testBadCircularYAMLPath = "./testdata/bad_parent_circular.yml"

	testUserAgeTeen    = 15
	testUserAgeSixteen = 16
	testUserAgeChild   = 10
	testUserAgeFive    = 5
	testAttemptLow     = 2
	testAttemptHigh    = 25
)

func setupEnvironment() func(*testing.T) *cel.Env {
	return func(t *testing.T) *cel.Env {
		t.Helper()
		env, err := NewDefaultEnv("user", "request")
		if err != nil {
			t.Fatalf("failed to create CEL environment: %v", err)
		}
		return env
	}
}

func newOneNanosecondTimeoutEngine(t *testing.T) *RuleEngine {
	t.Helper()
	env := setupEnvironment()(t)
	cfg, err := NewRulesetConfig(testRulesYAMLPath)
	if err != nil {
		t.Fatalf("NewRulesetConfig() error = %v", err)
	}
	cfg.ExecutionPolicies[policyFailFast] = ExecutionPolicy{
		Name:             "Strict 1ns Timeout",
		StopOnFailure:    true,
		MaxExecutionTime: "1ns",
	}
	engine, err := NewRuleEngineFromConfig(cfg, envProduction, env)
	if err != nil {
		t.Fatalf("NewRuleEngineFromConfig() error = %v", err)
	}
	return engine
}

func TestNewDefaultEnv(t *testing.T) {
	tests := []struct {
		name      string
		variables []string
		expr      string
		wantErr   bool
	}{
		{
			name:      "GivenCustomVariables_WhenNewDefaultEnv_ThenCompilesBuiltinFunctionsAndVariables",
			variables: []string{"user", "request"},
			expr:      "user.age >= globals.min_age && timestamp(request.time) <= now()",
			wantErr:   false,
		},
		{
			name:      "GivenUndeclaredVariableInExpression_WhenCompileWithDefaultEnv_ThenReturnsCompileError",
			variables: []string{"user"},
			expr:      "undeclared_var.field == 1",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, err := NewDefaultEnv(tt.variables...)
			if err != nil {
				t.Fatalf("NewDefaultEnv() unexpected error = %v", err)
			}
			_, issues := env.Compile(tt.expr)
			gotErr := issues != nil && issues.Err() != nil
			if gotErr != tt.wantErr {
				t.Errorf("env.Compile(%q) error = %v, wantErr %v", tt.expr, issues.Err(), tt.wantErr)
			}
		})
	}
}

func TestEvaluateRule(t *testing.T) {
	type args struct {
		ruleName string
		input    map[string]any
	}
	tests := []struct {
		name       string
		ruleengine func(*testing.T) *RuleEngine
		ctx        func() (context.Context, context.CancelFunc)
		args       args
		want       RuleResult
		wantErr    bool
	}{
		{
			name: "GivenValidTeenUserInDev_WhenEvaluateAgeValidation_ThenPasses",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine(testRulesYAMLPath, envDevelopment, env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				ruleName: "age_validation",
				input: map[string]any{
					"user": map[string]any{
						"age":       testUserAgeTeen,
						"email":     "test@example.com",
						"status":    "active",
						"suspended": false,
					},
					"request": map[string]any{
						"time":    time.Now().Format(time.RFC3339),
						"attempt": testAttemptLow,
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
			name: "GivenWhitelistedEmail_WhenEvaluateRuleWithSingleParent_ThenPasses",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine(testRulesYAMLPath, envDevelopment, env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				ruleName: "email_whitelist",
				input: map[string]any{
					"user": map[string]any{
						"age":       testUserAgeTeen,
						"email":     "test@example.com",
						"status":    "active",
						"suspended": false,
					},
					"request": map[string]any{
						"time":    time.Now().Format(time.RFC3339),
						"attempt": testAttemptLow,
					},
				},
			},
			want: RuleResult{
				RuleName: "email_whitelist",
				Passed:   true,
				Error:    nil,
			},
			wantErr: false,
		},
		{
			name: "GivenValidTestUserEmail_WhenEvaluateRuleWithTwoParents_ThenPasses",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine(testRulesYAMLPath, envDevelopment, env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				ruleName: "test_user",
				input: map[string]any{
					"user": map[string]any{
						"age":       testUserAgeTeen,
						"email":     "test@test.org",
						"status":    "active",
						"suspended": false,
					},
					"request": map[string]any{
						"time":    time.Now().Format(time.RFC3339),
						"attempt": testAttemptLow,
					},
				},
			},
			want: RuleResult{
				RuleName: "test_user",
				Passed:   true,
				Error:    nil,
			},
			wantErr: false,
		},
		{
			name: "GivenOptimisedEngine_WhenEvaluateAgeValidation_ThenPasses",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine(testRulesYAMLPath, envDevelopment, env, WithOptimise())
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				ruleName: "age_validation",
				input: map[string]any{
					"user": map[string]any{
						"age":       testUserAgeTeen,
						"email":     "test@example.com",
						"status":    "active",
						"suspended": false,
					},
					"request": map[string]any{
						"time":    time.Now().Format(time.RFC3339),
						"attempt": testAttemptLow,
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
			name: "GivenTeenUserInProd_WhenEvaluateAgeValidation_ThenFailsWithCustomMessage",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine(testRulesYAMLPath, envProduction, env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				ruleName: "age_validation",
				input: map[string]any{
					"user": map[string]any{
						"age":       testUserAgeTeen,
						"email":     "test@example.com",
						"status":    "active",
						"suspended": false,
					},
					"request": map[string]any{
						"time":    time.Now().Format(time.RFC3339),
						"attempt": testAttemptLow,
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
			name: "GivenNonExistentRuleName_WhenEvaluateRule_ThenReturnsError",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine(testRulesYAMLPath, envProduction, env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				ruleName: "height_validation",
				input:    map[string]any{},
			},
			want:    RuleResult{},
			wantErr: true,
		},
		{
			name: "GivenMalformedEmail_WhenEvaluateRuleExtendingEmailFormat_ThenFailsParentCheck",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine(testRulesYAMLPath, envDevelopment, env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				ruleName: "email_whitelist",
				input: map[string]any{
					"user": map[string]any{
						"age":       testUserAgeTeen,
						"email":     "not an email",
						"status":    "active",
						"suspended": false,
					},
					"request": map[string]any{
						"time":    time.Now().Format(time.RFC3339),
						"attempt": testAttemptLow,
					},
				},
			},
			want: RuleResult{
				RuleName: "email_whitelist",
				Passed:   false,
				Error:    errors.New("rule 'email_whitelist' did not pass evaluation"),
			},
			wantErr: false,
		},
		{
			name: "GivenDisallowedDomain_WhenEvaluateRuleExtendingEmailFormat_ThenFailsChildCheck",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine(testRulesYAMLPath, envDevelopment, env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				ruleName: "email_whitelist",
				input: map[string]any{
					"user": map[string]any{
						"age":       testUserAgeTeen,
						"email":     "me@me.com",
						"status":    "active",
						"suspended": false,
					},
					"request": map[string]any{
						"time":    time.Now().Format(time.RFC3339),
						"attempt": testAttemptLow,
					},
				},
			},
			want: RuleResult{
				RuleName: "email_whitelist",
				Passed:   false,
				Error:    errors.New("rule 'email_whitelist' did not pass evaluation"),
			},
			wantErr: false,
		},
		{
			name: "GivenRootAncestorFailure_WhenEvaluateTwoParentRule_ThenFails",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine(testRulesYAMLPath, envDevelopment, env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				ruleName: "test_user",
				input: map[string]any{
					"user": map[string]any{
						"age":       testUserAgeTeen,
						"email":     "test@bad email",
						"status":    "active",
						"suspended": false,
					},
					"request": map[string]any{
						"time":    time.Now().Format(time.RFC3339),
						"attempt": testAttemptLow,
					},
				},
			},
			want: RuleResult{
				RuleName: "test_user",
				Passed:   false,
				Error:    errors.New("rule 'test_user' did not pass evaluation"),
			},
			wantErr: false,
		},
		{
			name: "GivenImmediateParentFailure_WhenEvaluateTwoParentRule_ThenFails",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine(testRulesYAMLPath, envDevelopment, env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				ruleName: "test_user",
				input: map[string]any{
					"user": map[string]any{
						"age":       testUserAgeTeen,
						"email":     "test@gmail.com",
						"status":    "active",
						"suspended": false,
					},
					"request": map[string]any{
						"time":    time.Now().Format(time.RFC3339),
						"attempt": testAttemptLow,
					},
				},
			},
			want: RuleResult{
				RuleName: "test_user",
				Passed:   false,
				Error:    errors.New("rule 'test_user' did not pass evaluation"),
			},
			wantErr: false,
		},
		{
			name: "GivenChildExpressionFailure_WhenEvaluateTwoParentRule_ThenFails",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine(testRulesYAMLPath, envDevelopment, env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				ruleName: "test_user",
				input: map[string]any{
					"user": map[string]any{
						"age":       testUserAgeTeen,
						"email":     "admin@test.org",
						"status":    "active",
						"suspended": false,
					},
					"request": map[string]any{
						"time":    time.Now().Format(time.RFC3339),
						"attempt": testAttemptLow,
					},
				},
			},
			want: RuleResult{
				RuleName: "test_user",
				Passed:   false,
				Error:    errors.New("rule 'test_user' did not pass evaluation"),
			},
			wantErr: false,
		},
		{
			name: "GivenCancelledContext_WhenEvaluateRule_ThenReturnsContextError",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine(testRulesYAMLPath, envDevelopment, env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			ctx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, func() {}
			},
			args: args{
				ruleName: "age_validation",
				input:    map[string]any{},
			},
			want:    RuleResult{},
			wantErr: true,
		},
		{
			name:       "GivenOneNanosecondPolicyTimeout_WhenEvaluateRule_ThenReturnsTimeoutError",
			ruleengine: newOneNanosecondTimeoutEngine,
			args: args{
				ruleName: "age_validation",
				input: map[string]any{
					"user": map[string]any{"age": testUserAgeTeen},
				},
			},
			want:    RuleResult{},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re := tt.ruleengine(t)
			ctx := context.Background()
			if tt.ctx != nil {
				var cancel context.CancelFunc
				ctx, cancel = tt.ctx()
				defer cancel()
			}
			got, err := re.EvaluateRule(ctx, tt.args.ruleName, tt.args.input)
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

func TestEvaluateRuleset(t *testing.T) {
	type args struct {
		rulesetName string
		input       map[string]any
	}
	tests := []struct {
		name       string
		ruleengine func(*testing.T) *RuleEngine
		args       args
		want       RulesetResult
		wantErr    bool
	}{
		{
			name: "GivenAllRulesPassing_WhenEvaluateAndRulesetInDev_ThenPasses",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine(testRulesYAMLPath, envDevelopment, env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				rulesetName: "user_registration",
				input: map[string]any{
					"user": map[string]any{
						"age":       testUserAgeTeen,
						"email":     "test@example.com",
						"status":    "active",
						"suspended": false,
						"tier":      "free",
					},
					"request": map[string]any{
						"time":    time.Now().Format(time.RFC3339),
						"attempt": testAttemptLow,
					},
				},
			},
			want: RulesetResult{
				RulesetName: "user_registration",
				Passed:      true,
				RuleResults: map[string]RuleResult{
					"age_validation": {RuleName: "age_validation", Passed: true},
					"email_format":   {RuleName: "email_format", Passed: true},
					"user_status":    {RuleName: "user_status", Passed: true},
				},
			},
			wantErr: false,
		},
		{
			name: "GivenOptimisedEngine_WhenEvaluateAndRulesetInDev_ThenPasses",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine(testRulesYAMLPath, envDevelopment, env, WithOptimise())
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				rulesetName: "user_registration",
				input: map[string]any{
					"user": map[string]any{
						"age":       testUserAgeTeen,
						"email":     "test@example.com",
						"status":    "active",
						"suspended": false,
						"tier":      "free",
					},
					"request": map[string]any{
						"time":    time.Now().Format(time.RFC3339),
						"attempt": testAttemptLow,
					},
				},
			},
			want: RulesetResult{
				RulesetName: "user_registration",
				Passed:      true,
				RuleResults: map[string]RuleResult{
					"age_validation": {RuleName: "age_validation", Passed: true},
					"email_format":   {RuleName: "email_format", Passed: true},
					"user_status":    {RuleName: "user_status", Passed: true},
				},
			},
			wantErr: false,
		},
		{
			name: "GivenExtendedRulePassing_WhenEvaluateDomainWhitelistRuleset_ThenPasses",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine(testRulesYAMLPath, envDevelopment, env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				rulesetName: "domain_whitelist",
				input: map[string]any{
					"user": map[string]any{
						"age":       testUserAgeTeen,
						"email":     "test@example.com",
						"status":    "active",
						"suspended": false,
						"tier":      "free",
					},
					"request": map[string]any{
						"time":    time.Now().Format(time.RFC3339),
						"attempt": testAttemptLow,
					},
				},
			},
			want: RulesetResult{
				RulesetName: "domain_whitelist",
				Passed:      true,
				RuleResults: map[string]RuleResult{
					"email_whitelist": {RuleName: "email_whitelist", Passed: true},
				},
			},
			wantErr: false,
		},
		{
			name: "GivenInvalidEmailFormat_WhenEvaluateDomainWhitelistRuleset_ThenFailsWithCustomRulesetMessage",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine(testRulesYAMLPath, envDevelopment, env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				rulesetName: "domain_whitelist",
				input: map[string]any{
					"user": map[string]any{
						"age":       testUserAgeTeen,
						"email":     "test.example.com",
						"status":    "active",
						"suspended": false,
						"tier":      "free",
					},
					"request": map[string]any{
						"time":    time.Now().Format(time.RFC3339),
						"attempt": testAttemptLow,
					},
				},
			},
			want: RulesetResult{
				RulesetName: "domain_whitelist",
				Passed:      false,
				RuleResults: map[string]RuleResult{
					"email_whitelist": {
						RuleName: "email_whitelist",
						Passed:   false,
						Error:    errors.New("rule 'email_whitelist' did not pass evaluation"),
					},
				},
				Error: errors.New("email domain is not allowed"),
			},
			wantErr: false,
		},
		{
			name: "GivenUnderageUserAndCollectAllPolicy_WhenEvaluateUserRegistration_ThenEvaluatesAllRulesAndFails",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine(testRulesYAMLPath, envDevelopment, env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				rulesetName: "user_registration",
				input: map[string]any{
					"user": map[string]any{
						"age":       testUserAgeChild,
						"email":     "test@example.com",
						"status":    "active",
						"suspended": false,
						"tier":      "free",
					},
					"request": map[string]any{
						"time":    time.Now().Format(time.RFC3339),
						"attempt": testAttemptLow,
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
					},
					"email_format": {RuleName: "email_format", Passed: true},
					"user_status":  {RuleName: "user_status", Passed: true},
				},
				Error: errors.New("ruleset 'user_registration' did not pass evaluation"),
			},
			wantErr: false,
		},
		{
			name: "GivenUnderageUserAndFailFastPolicy_WhenEvaluateUserRegistration_ThenStopsAtFirstFailedRule",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine(testRulesYAMLPath, envProduction, env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				rulesetName: "user_registration",
				input: map[string]any{
					"user": map[string]any{
						"age":       testUserAgeSixteen,
						"email":     "test@example.com",
						"status":    "active",
						"suspended": false,
						"tier":      "free",
					},
					"request": map[string]any{
						"time":    time.Now().Format(time.RFC3339),
						"attempt": testAttemptLow,
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
					},
				},
				Error: errors.New("ruleset 'user_registration' did not pass evaluation"),
			},
			wantErr: false,
		},
		{
			name: "GivenFirstRulePassesInOrRuleset_WhenEvaluateRequestThrottlingInProd_ThenShortCircuitsAndPasses",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine(testRulesYAMLPath, envProduction, env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				rulesetName: "request_throttling",
				input: map[string]any{
					"user": map[string]any{
						"age":       testUserAgeSixteen,
						"email":     "test@example.com",
						"status":    "active",
						"suspended": false,
						"tier":      "free",
					},
					"request": map[string]any{
						"time":    time.Now().Format(time.RFC3339),
						"attempt": testAttemptLow,
					},
				},
			},
			want: RulesetResult{
				RulesetName: "request_throttling",
				Passed:      true,
				RuleResults: map[string]RuleResult{
					"rate_limiting": {RuleName: "rate_limiting", Passed: true},
				},
			},
			wantErr: false,
		},
		{
			name:       "GivenOneNanosecondPolicyTimeout_WhenEvaluateRuleset_ThenReturnsTimeoutError",
			ruleengine: newOneNanosecondTimeoutEngine,
			args: args{
				rulesetName: "user_registration",
				input: map[string]any{
					"user": map[string]any{"age": testUserAgeTeen},
				},
			},
			want:    RulesetResult{},
			wantErr: true,
		},
		{
			name: "GivenSecondRulePassesInOrRuleset_WhenEvaluateRequestThrottlingInDev_ThenPasses",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine(testRulesYAMLPath, envDevelopment, env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				rulesetName: "request_throttling",
				input: map[string]any{
					"user": map[string]any{
						"age":       testUserAgeSixteen,
						"email":     "test@example.com",
						"status":    "active",
						"suspended": false,
						"tier":      "premium",
					},
					"request": map[string]any{
						"time":    time.Now().Format(time.RFC3339),
						"attempt": testAttemptHigh,
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
						Error:    errors.New("rule 'rate_limiting' did not pass evaluation"),
					},
					"user_tier": {RuleName: "user_tier", Passed: true},
				},
			},
			wantErr: false,
		},
		{
			name: "GivenAllRulesFailInOrRuleset_WhenEvaluateRequestThrottling_ThenFailsWithCustomMessage",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine(testRulesYAMLPath, envDevelopment, env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				rulesetName: "request_throttling",
				input: map[string]any{
					"user": map[string]any{
						"age":       testUserAgeSixteen,
						"email":     "test@example.com",
						"status":    "active",
						"suspended": false,
						"tier":      "free",
					},
					"request": map[string]any{
						"time":    time.Now().Format(time.RFC3339),
						"attempt": testAttemptHigh,
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
						Error:    errors.New("rule 'rate_limiting' did not pass evaluation"),
					},
					"user_tier": {
						RuleName: "user_tier",
						Passed:   false,
						Error:    errors.New("rule 'user_tier' did not pass evaluation"),
					},
				},
				Error: errors.New("too many requests, please try again later"),
			},
			wantErr: false,
		},
		{
			name: "GivenNoneSelectorAndAllRulesFalse_WhenEvaluateRuleset_ThenPasses",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine(testRulesYAMLPath, envDevelopment, env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				if err := engine.UpsertRuleset("no_privilege_or_quota", Ruleset{
					Name:     "No Privilege Or Quota",
					Selector: SelectorNone,
					Rules:    []string{"rate_limiting", "user_tier"},
				}); err != nil {
					t.Fatalf("failed to upsert ruleset: %v", err)
				}
				return engine
			},
			args: args{
				rulesetName: "no_privilege_or_quota",
				input: map[string]any{
					"user":    map[string]any{"tier": "free"},
					"request": map[string]any{"attempt": testAttemptHigh},
				},
			},
			want: RulesetResult{
				RulesetName: "no_privilege_or_quota",
				Passed:      true,
				RuleResults: map[string]RuleResult{
					"rate_limiting": {
						RuleName: "rate_limiting",
						Passed:   false,
						Error:    errors.New("rule 'rate_limiting' did not pass evaluation"),
					},
					"user_tier": {
						RuleName: "user_tier",
						Passed:   false,
						Error:    errors.New("rule 'user_tier' did not pass evaluation"),
					},
				},
			},
			wantErr: false,
		},
		{
			name: "GivenMinPassedThresholdTwo_WhenTwoOfThreeRulesPass_ThenPasses",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine(testRulesYAMLPath, envDevelopment, env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				if err := engine.UpsertRuleset("two_of_three", Ruleset{
					Name:      "Two Of Three",
					MinPassed: 2,
					Rules:     []string{"age_validation", "email_format", "user_tier"},
				}); err != nil {
					t.Fatalf("failed to upsert ruleset: %v", err)
				}
				return engine
			},
			args: args{
				rulesetName: "two_of_three",
				input: map[string]any{
					"user": map[string]any{
						"age":   testUserAgeTeen,
						"email": "test@example.com",
						"tier":  "free",
					},
				},
			},
			want: RulesetResult{
				RulesetName: "two_of_three",
				Passed:      true,
				RuleResults: map[string]RuleResult{
					"age_validation": {RuleName: "age_validation", Passed: true},
					"email_format":   {RuleName: "email_format", Passed: true},
					"user_tier": {
						RuleName: "user_tier",
						Passed:   false,
						Error:    errors.New("rule 'user_tier' did not pass evaluation"),
					},
				},
			},
			wantErr: false,
		},
		{
			name: "GivenUnknownRulesetName_WhenEvaluateRuleset_ThenReturnsError",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine(testRulesYAMLPath, envDevelopment, env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			args: args{
				rulesetName: "unknown_ruleset",
				input:       map[string]any{},
			},
			want:    RulesetResult{},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re := tt.ruleengine(t)
			got, err := re.EvaluateRuleset(context.Background(), tt.args.rulesetName, tt.args.input)
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

func TestEvaluateAllRulesets(t *testing.T) {
	tests := []struct {
		name       string
		ruleengine func(*testing.T) *RuleEngine
		input      map[string]any
		want       map[string]RulesetResult
		wantErr    bool
	}{
		{
			name: "GivenValidInputInDev_WhenEvaluateAllRulesets_ThenReturnsAllRulesetResults",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine(testRulesYAMLPath, envDevelopment, env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			input: map[string]any{
				"user": map[string]any{
					"age":       testUserAgeTeen,
					"email":     "test@example.com",
					"status":    "active",
					"suspended": false,
					"tier":      "free",
				},
				"request": map[string]any{
					"time":    time.Now().Format(time.RFC3339),
					"attempt": testAttemptLow,
				},
			},
			want: map[string]RulesetResult{
				"user_registration": {
					RulesetName: "user_registration",
					Passed:      true,
					RuleResults: map[string]RuleResult{
						"age_validation": {RuleName: "age_validation", Passed: true},
						"email_format":   {RuleName: "email_format", Passed: true},
						"user_status":    {RuleName: "user_status", Passed: true},
					},
				},
				"request_throttling": {
					RulesetName: "request_throttling",
					Passed:      true,
					RuleResults: map[string]RuleResult{
						"rate_limiting": {RuleName: "rate_limiting", Passed: true},
						"user_tier": {
							RuleName: "user_tier",
							Passed:   false,
							Error:    errors.New("rule 'user_tier' did not pass evaluation"),
						},
					},
				},
				"domain_whitelist": {
					RulesetName: "domain_whitelist",
					Passed:      true,
					RuleResults: map[string]RuleResult{
						"email_whitelist": {RuleName: "email_whitelist", Passed: true},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "GivenOptimisedEngineInDev_WhenEvaluateAllRulesets_ThenReturnsAllRulesetResults",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine(testRulesYAMLPath, envDevelopment, env, WithOptimise())
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			input: map[string]any{
				"user": map[string]any{
					"age":       testUserAgeTeen,
					"email":     "test@example.com",
					"status":    "active",
					"suspended": false,
					"tier":      "free",
				},
				"request": map[string]any{
					"time":    time.Now().Format(time.RFC3339),
					"attempt": testAttemptLow,
				},
			},
			want: map[string]RulesetResult{
				"user_registration": {
					RulesetName: "user_registration",
					Passed:      true,
					RuleResults: map[string]RuleResult{
						"age_validation": {RuleName: "age_validation", Passed: true},
						"email_format":   {RuleName: "email_format", Passed: true},
						"user_status":    {RuleName: "user_status", Passed: true},
					},
				},
				"request_throttling": {
					RulesetName: "request_throttling",
					Passed:      true,
					RuleResults: map[string]RuleResult{
						"rate_limiting": {RuleName: "rate_limiting", Passed: true},
						"user_tier": {
							RuleName: "user_tier",
							Passed:   false,
							Error:    errors.New("rule 'user_tier' did not pass evaluation"),
						},
					},
				},
				"domain_whitelist": {
					RulesetName: "domain_whitelist",
					Passed:      true,
					RuleResults: map[string]RuleResult{
						"email_whitelist": {RuleName: "email_whitelist", Passed: true},
					},
				},
			},
			wantErr: false,
		},
		{
			name:       "GivenOneNanosecondPolicyTimeout_WhenEvaluateAllRulesets_ThenReturnsTimeoutError",
			ruleengine: newOneNanosecondTimeoutEngine,
			input: map[string]any{
				"user": map[string]any{
					"age":       testUserAgeTeen,
					"email":     "test@example.com",
					"status":    "active",
					"suspended": false,
					"tier":      "free",
				},
				"request": map[string]any{
					"time":    time.Now().Format(time.RFC3339),
					"attempt": testAttemptLow,
				},
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "GivenUnderageUserInDev_WhenEvaluateAllRulesets_ThenReturnsFailedRegistrationAndPassingOthers",
			ruleengine: func(t *testing.T) *RuleEngine {
				env := setupEnvironment()(t)
				engine, err := NewRuleEngine(testRulesYAMLPath, envDevelopment, env)
				if err != nil {
					t.Fatalf("failed to create rules engine: %v", err)
				}
				return engine
			},
			input: map[string]any{
				"user": map[string]any{
					"age":       testUserAgeFive,
					"email":     "test@example.com",
					"status":    "active",
					"suspended": false,
					"tier":      "free",
				},
				"request": map[string]any{
					"time":    time.Now().Format(time.RFC3339),
					"attempt": testAttemptLow,
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
						},
						"email_format": {RuleName: "email_format", Passed: true},
						"user_status":  {RuleName: "user_status", Passed: true},
					},
					Error: errors.New("ruleset 'user_registration' did not pass evaluation"),
				},
				"request_throttling": {
					RulesetName: "request_throttling",
					Passed:      true,
					RuleResults: map[string]RuleResult{
						"rate_limiting": {RuleName: "rate_limiting", Passed: true},
						"user_tier": {
							RuleName: "user_tier",
							Passed:   false,
							Error:    errors.New("rule 'user_tier' did not pass evaluation"),
						},
					},
				},
				"domain_whitelist": {
					RulesetName: "domain_whitelist",
					Passed:      true,
					RuleResults: map[string]RuleResult{
						"email_whitelist": {RuleName: "email_whitelist", Passed: true},
					},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re := tt.ruleengine(t)
			got, err := re.EvaluateAllRulesets(context.Background(), tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("EvaluateAllRulesets() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
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
	tests := []struct {
		name        string
		configPath  string
		environment string
		envProvider func(*testing.T) *cel.Env
		opts        []Option
		wantErr     bool
	}{
		{
			name:        "GivenNonExistentConfigFile_WhenNewRuleEngine_ThenReturnsError",
			configPath:  "test.yml",
			envProvider: setupEnvironment(),
			wantErr:     true,
		},
		{
			name:       "GivenNilCELEnvironment_WhenNewRuleEngine_ThenReturnsError",
			configPath: testRulesYAMLPath,
			envProvider: func(*testing.T) *cel.Env {
				return nil
			},
			wantErr: true,
		},
		{
			name:        "GivenInvalidExecutionPolicy_WhenNewRuleEngine_ThenReturnsError",
			configPath:  testBadPolicyYAMLPath,
			envProvider: setupEnvironment(),
			wantErr:     true,
		},
		{
			name:        "GivenInvalidCELExpression_WhenNewRuleEngine_ThenReturnsError",
			configPath:  testBadRulesYAMLPath,
			envProvider: setupEnvironment(),
			wantErr:     true,
		},
		{
			name:        "GivenMissingParentRule_WhenNewRuleEngine_ThenReturnsError",
			configPath:  testBadParentsYAMLPath,
			envProvider: setupEnvironment(),
			wantErr:     true,
		},
		{
			name:        "GivenCircularParentDependency_WhenNewRuleEngine_ThenReturnsError",
			configPath:  testBadCircularYAMLPath,
			envProvider: setupEnvironment(),
			wantErr:     true,
		},
		{
			name:        "GivenValidConfigAndEnv_WhenNewRuleEngine_ThenSucceeds",
			configPath:  testRulesYAMLPath,
			envProvider: setupEnvironment(),
			wantErr:     false,
		},
		{
			name:        "GivenValidConfigWithOptimiseOption_WhenNewRuleEngine_ThenSucceeds",
			configPath:  testRulesYAMLPath,
			envProvider: setupEnvironment(),
			opts:        []Option{WithOptimise()},
			wantErr:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewRuleEngine(tt.configPath, tt.environment, tt.envProvider(t), tt.opts...)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewRuleEngine() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestProgrammaticRuleManagement(t *testing.T) {
	t.Run("GivenProgrammaticConfig_WhenUpsertRemoveAndInspect_ThenUpdatesEngineSafely", func(t *testing.T) {
		env := setupEnvironment()(t)
		cfg := &RulesetConfig{
			Globals: map[string]any{"min_age": prodMinAge},
			Rules: map[string]Rule{
				"is_adult": {
					Name:       "Adult Check",
					Expression: "user.age >= globals.min_age",
				},
			},
			Rulesets: map[string]Ruleset{
				"onboarding": {
					Name:     "Onboarding",
					Selector: SelectorAnd,
					Rules:    []string{"is_adult"},
				},
			},
		}

		engine, err := NewRuleEngineFromConfig(cfg, "", env)
		if err != nil {
			t.Fatalf("NewRuleEngineFromConfig() unexpected error: %v", err)
		}

		// Mutate caller's cfg after engine creation to verify deep-copy ownership isolation.
		cfg.Globals["min_age"] = 999
		cfg.Rules["is_adult"] = Rule{Expression: "false"}
		cfg.Rulesets["onboarding"].Rules[0] = "mutated_rule"

		initialCheck, err := engine.EvaluateRuleset(context.Background(), "onboarding", map[string]any{
			"user": map[string]any{"age": defaultMinAge},
		})
		if err != nil || !initialCheck.Passed {
			t.Fatalf("EvaluateRuleset(onboarding) after external config mutation got (%+v, %v), want Passed=true", initialCheck, err)
		}

		// Reject unsupported ruleset selector
		if err := engine.UpsertRuleset("invalid_selector", Ruleset{
			Name:     "Invalid Selector",
			Selector: SelectorType("XOR"),
			Rules:    []string{"is_adult"},
		}); !errors.Is(err, ErrInvalidSelector) {
			t.Errorf("UpsertRuleset(XOR) error = %v, want errors.Is ErrInvalidSelector", err)
		}

		// Reject out-of-range MinPassed threshold
		if err := engine.UpsertRuleset("invalid_min_passed", Ruleset{
			Name:      "Invalid MinPassed",
			MinPassed: 5,
			Rules:     []string{"is_adult"},
		}); !errors.Is(err, ErrInvalidSelector) {
			t.Errorf("UpsertRuleset(min_passed=5) error = %v, want errors.Is ErrInvalidSelector", err)
		}

		if err := engine.ValidateExpression("user.age > 0"); err != nil {
			t.Errorf("ValidateExpression(valid) unexpected error: %v", err)
		}
		if err := engine.ValidateExpression("user.age + 10"); err == nil {
			t.Error("ValidateExpression(non-bool) expected error, got nil")
		}

		if err := engine.UpsertRule("verified_adult", Rule{
			Name:       "Verified Adult",
			Extends:    "is_adult",
			Expression: "user.verified == true",
		}); err != nil {
			t.Fatalf("UpsertRule() unexpected error: %v", err)
		}

		if err := engine.UpsertRuleset("verified_onboarding", Ruleset{
			Name:     "Verified Onboarding",
			Selector: SelectorAnd,
			Rules:    []string{"verified_adult"},
		}); err != nil {
			t.Fatalf("UpsertRuleset() unexpected error: %v", err)
		}

		res, err := engine.EvaluateRuleset(context.Background(), "verified_onboarding", map[string]any{
			"user": map[string]any{"age": defaultMinAge, "verified": true},
		})
		if err != nil || !res.Passed {
			t.Fatalf("EvaluateRuleset(verified_onboarding) got (%+v, %v), want Passed=true", res, err)
		}

		// Removing a rule still referenced by a child rule or ruleset must fail
		if err := engine.RemoveRule("is_adult"); err == nil {
			t.Error("RemoveRule(is_adult) expected error while extended by verified_adult, got nil")
		}

		if len(engine.ListRules()) != 2 {
			t.Errorf("ListRules() count = %d, want 2", len(engine.ListRules()))
		}
		if len(engine.ListRulesets()) != 2 {
			t.Errorf("ListRulesets() count = %d, want 2", len(engine.ListRulesets()))
		}

		// Verify JSON marshaling of RulesetResult
		payload, err := json.Marshal(res)
		if err != nil || len(payload) == 0 {
			t.Errorf("json.Marshal(RulesetResult) err = %v, payload = %s", err, string(payload))
		}
	})
}

func TestConcurrentEvaluations(t *testing.T) {
	t.Run("GivenConcurrentEvaluatorsAndMutators_WhenRunningUnderRaceDetector_ThenNoDataRacesOccur", func(t *testing.T) {
		env := setupEnvironment()(t)
		engine, err := NewRuleEngine(testRulesYAMLPath, envDevelopment, env)
		if err != nil {
			t.Fatalf("NewRuleEngine() error = %v", err)
		}

		const workers = 16
		var wg sync.WaitGroup
		wg.Add(workers)

		for i := range workers {
			go func(id int) {
				defer wg.Done()
				input := map[string]any{
					"user": map[string]any{
						"age":       devMinAge + id,
						"email":     "test@example.com",
						"status":    "active",
						"suspended": false,
						"tier":      "premium",
					},
					"request": map[string]any{
						"time":    time.Now().Format(time.RFC3339),
						"attempt": testAttemptLow,
					},
				}
				if id%4 == 0 {
					_ = engine.UpsertRule("dynamic_rule", Rule{
						Name:       "Dynamic Rule",
						Expression: "user.age >= globals.min_age",
					})
				}
				_, _ = engine.EvaluateAllRulesets(context.Background(), input)
				_, _ = engine.EvaluateRule(context.Background(), "test_user", input)
			}(i)
		}
		wg.Wait()
	})
}
