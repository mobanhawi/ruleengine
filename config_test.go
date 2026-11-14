package ruleengine

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

const (
	testRulesYAMLPath       = "./testdata/rules.yml"
	testRulesJSONPath       = "./testdata/rules.json"
	testNonExistentYAMLPath = "./testdata/nonexistent.yml"

	envDevelopment = "development"
	envProduction  = "production"
	envLocal       = "local"

	policyDefault    = "default_policy"
	policyFailFast   = "fail_fast"
	policyCollectAll = "collect_all"
	policyUnknown    = "unknown_policy"
	policyBadTime    = "bad_time"

	defaultMinAge     = 21
	devMinAge         = 13
	prodMinAge        = 18
	defaultMaxRetries = 5
	defaultTimeout    = 5 * time.Second
)

func TestNewRulesetConfig(t *testing.T) {
	tests := []struct {
		name       string
		configPath string
		want       *RulesetConfig
		wantErr    bool
	}{
		{
			name:       "GivenNonExistentPath_WhenNewRulesetConfig_ThenReturnsError",
			configPath: testNonExistentYAMLPath,
			want:       nil,
			wantErr:    true,
		},
		{
			name:       "GivenInvalidYAMLFile_WhenNewRulesetConfig_ThenReturnsError",
			configPath: testRulesJSONPath,
			want:       nil,
			wantErr:    true,
		},
		{
			name:       "GivenValidYAMLFile_WhenNewRulesetConfig_ThenReturnsParsedConfig",
			configPath: testRulesYAMLPath,
			want: &RulesetConfig{
				APIVersion: "v1",
				Kind:       "RulesetConfig",
				Metadata: Metadata{
					Name:        "cel-rulesets-example",
					Description: "Examples of CEL rule combinations and patterns",
				},
				Globals: map[string]any{
					"min_age":         devMinAge,
					"max_retries":     defaultMaxRetries,
					"allowed_domains": []any{"example.com", "test.org"},
				},
				Rules: map[string]Rule{
					"age_validation": {
						Name:        "Age Validation",
						Description: "Validates user age requirements",
						Expression:  "user.age >= globals.min_age",
					},
					"email_format": {
						Name:        "Email Format Check",
						Description: "Validates email format using regex",
						Expression:  "user.email.matches(\"^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\\\.[a-zA-Z]{2,}$\")\n",
					},
					"business_hours": {
						Name:        "Business Hours Check",
						Description: "Validates if current time is within business hours",
						Expression:  "timestamp(request.time).getHours() >= globals.business_hours_start && \ntimestamp(request.time).getHours() < globals.business_hours_end\n",
					},
					"rate_limiting": {
						Name:        "Rate Limiting",
						Description: "Checks request rate limits",
						Expression:  "request.attempt <= globals.max_retries",
					},
					"user_status": {
						Name:        "User Status Check",
						Description: "Validates user account status",
						Expression:  "user.status == 'active' && !user.suspended",
					},
					"user_tier": {
						Name:        "User Tier Check",
						Description: "Validates user account tier",
						Expression:  "user.tier == 'premium' || user.tier == 'enterprise'",
					},
					"email_whitelist": {
						Name:        "Domain Whitelist Check",
						Description: "Validates if email domain is in the allowed list",
						Expression:  "globals.allowed_domains.exists(domain, user.email.endsWith('@' + domain))\n",
						Extends:     "email_format",
					},
					"test_user": {
						Name:        "Test user Check",
						Description: "Checks if email is from test accounts",
						Expression:  "user.email.startsWith('test')",
						Extends:     "email_whitelist",
					},
				},
				Rulesets: map[string]Ruleset{
					"user_registration": {
						Name:        "User Registration Validation",
						Description: "All rules must pass for successful registration",
						Selector:    SelectorAnd,
						Rules: []string{
							"age_validation",
							"email_format",
							"user_status",
						},
					},
					"request_throttling": {
						Name:        "Request Throttling Check",
						Description: "At least one rule must pass to allow request",
						Selector:    SelectorOr,
						Rules: []string{
							"rate_limiting",
							"user_tier",
						},
					},
					"domain_whitelist": {
						Name:        "Domain Whitelist Check",
						Description: "Validates if request domain is in the allowed list",
						Rules: []string{
							"email_whitelist",
						},
					},
				},
				ExecutionPolicies: map[string]ExecutionPolicy{
					policyFailFast: {
						Name:             "Fail Fast Execution",
						Description:      "Stop execution on first rule failure",
						StopOnFailure:    true,
						MaxExecutionTime: "1ns",
					},
					policyCollectAll: {
						Name:             "Collect All Results",
						Description:      "Execute all rules regardless of failures",
						StopOnFailure:    false,
						MaxExecutionTime: "",
					},
				},
				ErrorHandling: ErrorHandling{
					ExecutionPolicy: policyCollectAll,
					CustomErrorMessages: map[string]string{
						"age_validation":     "user must be at least 18 years old",
						"email_format":       "please provide a valid email address",
						"domain_whitelist":   "email domain is not allowed",
						"business_hours":     "service only available during business hours (9 AM - 5 PM)",
						"request_throttling": "too many requests, please try again later",
					},
				},
				Environments: map[string]Environment{
					envDevelopment: {
						Globals: map[string]any{
							"min_age": devMinAge,
						},
						ErrorHandling: ErrorHandling{
							ExecutionPolicy: "",
						},
					},
					envProduction: {
						Globals: map[string]any{
							"min_age": prodMinAge,
						},
						ErrorHandling: ErrorHandling{
							ExecutionPolicy: policyFailFast,
						},
					},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewRulesetConfig(tt.configPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewRulesetConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if diff := cmp.Diff(got, tt.want); diff != "" {
				t.Errorf("NewRulesetConfig() (-got +want):\n%s", diff)
			}
		})
	}
}

func TestNewRulesetConfigFromReader(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "GivenValidYAMLReader_WhenNewRulesetConfigFromReader_ThenReturnsConfig",
			input:   "apiVersion: v1\nkind: RulesetConfig\nrules:\n  r1:\n    expression: \"true\"\n",
			wantErr: false,
		},
		{
			name:    "GivenMalformedYAMLReader_WhenNewRulesetConfigFromReader_ThenReturnsError",
			input:   ":::invalid_yaml\n\t- [",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewRulesetConfigFromReader(strings.NewReader(tt.input))
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewRulesetConfigFromReader() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got == nil {
				t.Fatal("NewRulesetConfigFromReader() returned nil config without error")
			}
		})
	}
}

func TestApplyEnvironment(t *testing.T) {
	baseEnvironments := func() map[string]Environment {
		return map[string]Environment{
			envDevelopment: {
				Globals: map[string]any{
					"min_age": devMinAge,
				},
				ErrorHandling: ErrorHandling{
					ExecutionPolicy: policyCollectAll,
					CustomErrorMessages: map[string]string{
						"age_validation": "user must be at least 13 years old",
					},
				},
			},
			envProduction: {
				Globals: map[string]any{
					"min_age": prodMinAge,
				},
				ErrorHandling: ErrorHandling{
					ExecutionPolicy: policyFailFast,
					CustomErrorMessages: map[string]string{
						"age_validation": "user must be at least 18 years old",
					},
				},
			},
		}
	}

	tests := []struct {
		name        string
		initial     *RulesetConfig
		environment string
		want        *RulesetConfig
	}{
		{
			name: "GivenDevelopmentEnvironment_WhenApplyEnvironment_ThenOverridesGlobalsAndErrors",
			initial: &RulesetConfig{
				Globals: map[string]any{
					"min_age": defaultMinAge,
				},
				ErrorHandling: ErrorHandling{
					ExecutionPolicy: policyDefault,
					CustomErrorMessages: map[string]string{
						"age_validation": "user must be at least 21 years old",
						"email_format":   "please provide a valid email address",
					},
				},
				Environments: baseEnvironments(),
			},
			environment: envDevelopment,
			want: &RulesetConfig{
				Globals: map[string]any{
					"min_age": devMinAge,
				},
				ErrorHandling: ErrorHandling{
					ExecutionPolicy: policyCollectAll,
					CustomErrorMessages: map[string]string{
						"age_validation": "user must be at least 13 years old",
						"email_format":   "please provide a valid email address",
					},
				},
				Environments: baseEnvironments(),
			},
		},
		{
			name: "GivenProductionEnvironment_WhenApplyEnvironment_ThenOverridesGlobalsAndPolicy",
			initial: &RulesetConfig{
				Globals: map[string]any{
					"min_age": defaultMinAge,
				},
				ErrorHandling: ErrorHandling{
					ExecutionPolicy: policyDefault,
					CustomErrorMessages: map[string]string{
						"age_validation": "user must be at least 21 years old",
						"email_format":   "please provide a valid email address",
					},
				},
				Environments: baseEnvironments(),
			},
			environment: envProduction,
			want: &RulesetConfig{
				Globals: map[string]any{
					"min_age": prodMinAge,
				},
				ErrorHandling: ErrorHandling{
					ExecutionPolicy: policyFailFast,
					CustomErrorMessages: map[string]string{
						"age_validation": "user must be at least 18 years old",
						"email_format":   "please provide a valid email address",
					},
				},
				Environments: baseEnvironments(),
			},
		},
		{
			name: "GivenUnknownEnvironment_WhenApplyEnvironment_ThenRetainsBaseConfig",
			initial: &RulesetConfig{
				Globals: map[string]any{
					"min_age": defaultMinAge,
				},
				ErrorHandling: ErrorHandling{
					ExecutionPolicy: policyDefault,
					CustomErrorMessages: map[string]string{
						"age_validation": "user must be at least 21 years old",
						"email_format":   "please provide a valid email address",
					},
				},
				Environments: baseEnvironments(),
			},
			environment: envLocal,
			want: &RulesetConfig{
				Globals: map[string]any{
					"min_age": defaultMinAge,
				},
				ErrorHandling: ErrorHandling{
					ExecutionPolicy: policyDefault,
					CustomErrorMessages: map[string]string{
						"age_validation": "user must be at least 21 years old",
						"email_format":   "please provide a valid email address",
					},
				},
				Environments: baseEnvironments(),
			},
		},
		{
			name: "GivenNilBaseMaps_WhenApplyEnvironment_ThenInitializesMapsWithoutPanic",
			initial: &RulesetConfig{
				Globals:       nil,
				ErrorHandling: ErrorHandling{CustomErrorMessages: nil},
				Environments:  baseEnvironments(),
			},
			environment: envDevelopment,
			want: &RulesetConfig{
				Globals: map[string]any{
					"min_age": devMinAge,
				},
				ErrorHandling: ErrorHandling{
					ExecutionPolicy: policyCollectAll,
					CustomErrorMessages: map[string]string{
						"age_validation": "user must be at least 13 years old",
					},
				},
				Environments: baseEnvironments(),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.initial.ApplyEnvironment(tt.environment)
			if diff := cmp.Diff(tt.initial, tt.want); diff != "" {
				t.Errorf("ApplyEnvironment() (-got +want):\n%s", diff)
			}
		})
	}
}

func TestToExecutionPolicy(t *testing.T) {
	tests := []struct {
		name      string
		policies  map[string]ExecutionPolicy
		errorCfg  ErrorHandling
		want      Policy
		wantErr   bool
		targetErr error
	}{
		{
			name: "GivenUnknownPolicyName_WhenToExecutionPolicy_ThenReturnsErrPolicyNotFound",
			policies: map[string]ExecutionPolicy{
				policyFailFast: {
					StopOnFailure:    true,
					MaxExecutionTime: "1ns",
				},
				policyCollectAll: {
					StopOnFailure: false,
				},
			},
			errorCfg: ErrorHandling{
				ExecutionPolicy: policyUnknown,
			},
			want: Policy{
				StopOnFailure:    true,
				MaxExecutionTime: defaultTimeout,
			},
			wantErr:   true,
			targetErr: ErrPolicyNotFound,
		},
		{
			name: "GivenInvalidDurationString_WhenToExecutionPolicy_ThenReturnsParseError",
			policies: map[string]ExecutionPolicy{
				policyBadTime: {
					StopOnFailure:    true,
					MaxExecutionTime: "1nsss",
				},
			},
			errorCfg: ErrorHandling{
				ExecutionPolicy: policyBadTime,
			},
			want: Policy{
				StopOnFailure:    true,
				MaxExecutionTime: defaultTimeout,
			},
			wantErr: true,
		},
		{
			name: "GivenPolicyWithoutMaxExecutionTime_WhenToExecutionPolicy_ThenUsesDefaultTimeout",
			policies: map[string]ExecutionPolicy{
				policyDefault: {
					StopOnFailure: true,
				},
			},
			errorCfg: ErrorHandling{
				ExecutionPolicy: policyDefault,
			},
			want: Policy{
				StopOnFailure:    true,
				MaxExecutionTime: defaultTimeout,
			},
			wantErr: false,
		},
		{
			name: "GivenFailFastPolicy_WhenToExecutionPolicy_ThenReturnsConfiguredDuration",
			policies: map[string]ExecutionPolicy{
				policyFailFast: {
					StopOnFailure:    true,
					MaxExecutionTime: "1ns",
				},
			},
			errorCfg: ErrorHandling{
				ExecutionPolicy: policyFailFast,
			},
			want: Policy{
				StopOnFailure:    true,
				MaxExecutionTime: 1 * time.Nanosecond,
			},
			wantErr: false,
		},
		{
			name: "GivenCollectAllPolicy_WhenToExecutionPolicy_ThenReturnsStopOnFailureFalse",
			policies: map[string]ExecutionPolicy{
				policyCollectAll: {
					StopOnFailure: false,
				},
			},
			errorCfg: ErrorHandling{
				ExecutionPolicy: policyCollectAll,
			},
			want: Policy{
				StopOnFailure:    false,
				MaxExecutionTime: defaultTimeout,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc := &RulesetConfig{
				ExecutionPolicies: tt.policies,
				ErrorHandling:     tt.errorCfg,
			}
			got, err := rc.ToExecutionPolicy()
			if (err != nil) != tt.wantErr {
				t.Errorf("ToExecutionPolicy() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.targetErr != nil && !errors.Is(err, tt.targetErr) {
				t.Errorf("ToExecutionPolicy() error = %v, want errors.Is %v", err, tt.targetErr)
			}
			if diff := cmp.Diff(got, tt.want); diff != "" {
				t.Errorf("ToExecutionPolicy() (-got +want):\n%s", diff)
			}
		})
	}
}
