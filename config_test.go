package ruleengine

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestNewRulesetConfig(t *testing.T) {
	type args struct {
		configPath string
	}
	tests := []struct {
		name    string
		args    args
		want    *RulesetConfig
		wantErr bool
	}{
		{
			name: "fail - bad path",
			args: args{
				configPath: "./testdata/nonexistent.yml",
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "fail - bad file",
			args: args{
				configPath: "./testdata/rules.json",
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "success - valid file",
			args: args{
				configPath: "./testdata/rules.yml",
			},
			want: &RulesetConfig{
				APIVersion: "v1",
				Kind:       "RulesetConfig",
				Metadata: Metadata{
					Name:        "cel-rulesets-example",
					Description: "Examples of CEL rule combinations and patterns",
				},
				Globals: map[string]interface{}{
					"min_age":         13,
					"max_retries":     5,
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
				},

				Rulesets: map[string]Ruleset{
					"user_registration": {
						Name:        "User Registration Validation",
						Description: "All rules must pass for successful registration",
						Selector:    "AND",
						Rules: []string{
							"age_validation",
							"email_format",
							"user_status",
						},
					},
					"request_throttling": {
						Name:        "Request Throttling Check",
						Description: "At least one rule must pass to allow request",
						Selector:    "OR",
						Rules: []string{
							"rate_limiting",
							"user_tier",
						},
					},
					"domain_whitelist": {
						Name:        "Domain Whitelist Check",
						Description: "Validates if email domain is in the allowed list",
						Expression:  "globals.allowed_domains.exists(domain, user.email.endsWith('@' + domain))\n",
						Extends:     "email_format",
					},
				},
				ExecutionPolicies: map[string]ExecutionPolicy{
					"fail_fast": {
						Name:             "Fail Fast Execution",
						Description:      "Stop execution on first rule failure",
						StopOnFailure:    true,
						MaxExecutionTime: "1ns",
					},
					"collect_all": {
						Name:             "Collect All Results",
						Description:      "Execute all rules regardless of failures",
						StopOnFailure:    false,
						MaxExecutionTime: "",
					},
				},
				ErrorHandling: ErrorHandling{
					ExecutionPolicy: "collect_all",
					CustomErrorMessages: map[string]string{
						"age_validation":     "user must be at least 18 years old",
						"email_format":       "please provide a valid email address",
						"domain_whitelist":   "email domain is not allowed",
						"business_hours":     "service only available during business hours (9 AM - 5 PM)",
						"request_throttling": "too many requests, please try again later",
					},
				},
				Environments: map[string]Environment{
					"development": {
						Globals: map[string]interface{}{
							"min_age": 13,
						},
						ErrorHandling: ErrorHandling{
							ExecutionPolicy: "",
						},
					},
					"production": {
						Globals: map[string]interface{}{
							"min_age": 18,
						},
						ErrorHandling: ErrorHandling{
							ExecutionPolicy: "fail_fast",
						},
					},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewRulesetConfig(tt.args.configPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewRulesetConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			diff := cmp.Diff(got, tt.want)
			if diff != "" {
				t.Errorf("NewRulesetConfig() (-got +want):\n%s", diff)
			}
		})
	}
}

func TestRulesetConfig_ApplyEnvironment(t *testing.T) {
	type fields struct {
		APIVersion        string
		Kind              string
		Metadata          Metadata
		Globals           map[string]interface{}
		Rules             map[string]Rule
		Rulesets          map[string]Ruleset
		ExecutionPolicies map[string]ExecutionPolicy
		ErrorHandling     ErrorHandling
		Environments      map[string]Environment
	}
	type args struct {
		environment string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   *RulesetConfig
	}{
		{
			name: "success - apply development environment",
			fields: fields{
				Globals: map[string]interface{}{
					"min_age": 21,
				},
				ErrorHandling: ErrorHandling{
					ExecutionPolicy: "default_policy",
					CustomErrorMessages: map[string]string{
						"age_validation": "user must be at least 21 years old",
						"email_format":   "please provide a valid email address",
					},
				},
				Environments: map[string]Environment{
					"development": {
						Globals: map[string]interface{}{
							"min_age": 13,
						},
						ErrorHandling: ErrorHandling{
							ExecutionPolicy: "collect_all",
							CustomErrorMessages: map[string]string{
								"age_validation": "user must be at least 13 years old",
							},
						},
					},
					"production": {
						Globals: map[string]interface{}{
							"min_age": 18,
						},
						ErrorHandling: ErrorHandling{
							ExecutionPolicy: "fail_fast",
							CustomErrorMessages: map[string]string{
								"age_validation": "user must be at least 18 years old",
							},
						},
					},
				},
			},
			args: args{
				environment: "development",
			},
			want: &RulesetConfig{
				Globals: map[string]interface{}{
					"min_age": 13,
				},
				ErrorHandling: ErrorHandling{
					ExecutionPolicy: "collect_all",
					CustomErrorMessages: map[string]string{
						"age_validation": "user must be at least 13 years old",
						"email_format":   "please provide a valid email address",
					},
				},
				Environments: map[string]Environment{
					"development": {
						Globals: map[string]interface{}{
							"min_age": 13,
						},
						ErrorHandling: ErrorHandling{
							ExecutionPolicy: "collect_all",
							CustomErrorMessages: map[string]string{
								"age_validation": "user must be at least 13 years old",
							},
						},
					},
					"production": {
						Globals: map[string]interface{}{
							"min_age": 18,
						},
						ErrorHandling: ErrorHandling{
							ExecutionPolicy: "fail_fast",
							CustomErrorMessages: map[string]string{
								"age_validation": "user must be at least 18 years old",
							},
						},
					},
				},
			},
		},
		{
			name: "success - apply production environment",
			fields: fields{
				Globals: map[string]interface{}{
					"min_age": 21,
				},
				ErrorHandling: ErrorHandling{
					ExecutionPolicy: "default_policy",
					CustomErrorMessages: map[string]string{
						"age_validation": "user must be at least 21 years old",
						"email_format":   "please provide a valid email address",
					},
				},
				Environments: map[string]Environment{
					"development": {
						Globals: map[string]interface{}{
							"min_age": 13,
						},
						ErrorHandling: ErrorHandling{
							ExecutionPolicy: "collect_all",
							CustomErrorMessages: map[string]string{
								"age_validation": "user must be at least 13 years old",
							},
						},
					},
					"production": {
						Globals: map[string]interface{}{
							"min_age": 18,
						},
						ErrorHandling: ErrorHandling{
							ExecutionPolicy: "fail_fast",
							CustomErrorMessages: map[string]string{
								"age_validation": "user must be at least 18 years old",
							},
						},
					},
				},
			},
			args: args{
				environment: "production",
			},
			want: &RulesetConfig{
				Globals: map[string]interface{}{
					"min_age": 18,
				},
				ErrorHandling: ErrorHandling{
					ExecutionPolicy: "fail_fast",
					CustomErrorMessages: map[string]string{
						"age_validation": "user must be at least 18 years old",
						"email_format":   "please provide a valid email address",
					},
				},
				Environments: map[string]Environment{
					"development": {
						Globals: map[string]interface{}{
							"min_age": 13,
						},
						ErrorHandling: ErrorHandling{
							ExecutionPolicy: "collect_all",
							CustomErrorMessages: map[string]string{
								"age_validation": "user must be at least 13 years old",
							},
						},
					},
					"production": {
						Globals: map[string]interface{}{
							"min_age": 18,
						},
						ErrorHandling: ErrorHandling{
							ExecutionPolicy: "fail_fast",
							CustomErrorMessages: map[string]string{
								"age_validation": "user must be at least 18 years old",
							},
						},
					},
				},
			},
		},
		{
			name: "success - apply unknown environment",
			fields: fields{
				Globals: map[string]interface{}{
					"min_age": 21,
				},
				ErrorHandling: ErrorHandling{
					ExecutionPolicy: "default_policy",
					CustomErrorMessages: map[string]string{
						"age_validation": "user must be at least 21 years old",
						"email_format":   "please provide a valid email address",
					},
				},
				Environments: map[string]Environment{
					"development": {
						Globals: map[string]interface{}{
							"min_age": 13,
						},
						ErrorHandling: ErrorHandling{
							ExecutionPolicy: "collect_all",
							CustomErrorMessages: map[string]string{
								"age_validation": "user must be at least 13 years old",
							},
						},
					},
					"production": {
						Globals: map[string]interface{}{
							"min_age": 18,
						},
						ErrorHandling: ErrorHandling{
							ExecutionPolicy: "fail_fast",
							CustomErrorMessages: map[string]string{
								"age_validation": "user must be at least 18 years old",
							},
						},
					},
				},
			},
			args: args{
				environment: "local",
			},
			want: &RulesetConfig{
				Globals: map[string]interface{}{
					"min_age": 21,
				},
				ErrorHandling: ErrorHandling{
					ExecutionPolicy: "default_policy",
					CustomErrorMessages: map[string]string{
						"age_validation": "user must be at least 21 years old",
						"email_format":   "please provide a valid email address",
					},
				},
				Environments: map[string]Environment{
					"development": {
						Globals: map[string]interface{}{
							"min_age": 13,
						},
						ErrorHandling: ErrorHandling{
							ExecutionPolicy: "collect_all",
							CustomErrorMessages: map[string]string{
								"age_validation": "user must be at least 13 years old",
							},
						},
					},
					"production": {
						Globals: map[string]interface{}{
							"min_age": 18,
						},
						ErrorHandling: ErrorHandling{
							ExecutionPolicy: "fail_fast",
							CustomErrorMessages: map[string]string{
								"age_validation": "user must be at least 18 years old",
							},
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc := &RulesetConfig{
				APIVersion:        tt.fields.APIVersion,
				Kind:              tt.fields.Kind,
				Metadata:          tt.fields.Metadata,
				Globals:           tt.fields.Globals,
				Rules:             tt.fields.Rules,
				Rulesets:          tt.fields.Rulesets,
				ExecutionPolicies: tt.fields.ExecutionPolicies,
				ErrorHandling:     tt.fields.ErrorHandling,
				Environments:      tt.fields.Environments,
			}
			rc.ApplyEnvironment(tt.args.environment)
			if diff := cmp.Diff(rc, tt.want); diff != "" {
				t.Errorf("ApplyEnvironment() (-got +want):\n%s", diff)
			}
		})
	}
}

func TestRulesetConfig_GetExecutionPolicy(t *testing.T) {
	type fields struct {
		ExecutionPolicies map[string]ExecutionPolicy
		ErrorHandling     ErrorHandling
	}
	tests := []struct {
		name    string
		fields  fields
		want    Policy
		wantErr bool
	}{
		{
			name: "fail - invalid policy",
			fields: fields{
				ExecutionPolicies: map[string]ExecutionPolicy{
					"fail_fast": {
						StopOnFailure:    true,
						MaxExecutionTime: "1ns",
					},
					"collect_all": {
						StopOnFailure: false,
					},
				},
				ErrorHandling: ErrorHandling{
					ExecutionPolicy: "unknown_policy",
				},
			},
			want: Policy{
				StopOnFailure:    true,
				MaxExecutionTime: 5 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "fail - bad_time policy",
			fields: fields{
				ExecutionPolicies: map[string]ExecutionPolicy{
					"bad_time": {
						StopOnFailure:    true,
						MaxExecutionTime: "1nsss",
					},
					"collect_all": {
						StopOnFailure: false,
					},
				},
				ErrorHandling: ErrorHandling{
					ExecutionPolicy: "bad_time",
				},
			},
			want: Policy{
				StopOnFailure:    true,
				MaxExecutionTime: 5 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "success - default policy",
			fields: fields{
				ExecutionPolicies: map[string]ExecutionPolicy{
					"fail_fast": {
						StopOnFailure:    true,
						MaxExecutionTime: "1ns",
					},
					"collect_all": {
						StopOnFailure: false,
					},
					"default_policy": {
						StopOnFailure: true,
					},
				},
				ErrorHandling: ErrorHandling{
					ExecutionPolicy: "default_policy",
				},
			},
			want: Policy{
				StopOnFailure:    true,
				MaxExecutionTime: 5 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "success - fail_fast policy",
			fields: fields{
				ExecutionPolicies: map[string]ExecutionPolicy{
					"fail_fast": {
						StopOnFailure:    true,
						MaxExecutionTime: "1ns",
					},
					"collect_all": {
						StopOnFailure: false,
					},
					"default_policy": {
						StopOnFailure: true,
					},
				},
				ErrorHandling: ErrorHandling{
					ExecutionPolicy: "fail_fast",
				},
			},
			want: Policy{
				StopOnFailure:    true,
				MaxExecutionTime: 1 * time.Nanosecond,
			},
			wantErr: false,
		},
		{
			name: "success - collect_all policy",
			fields: fields{
				ExecutionPolicies: map[string]ExecutionPolicy{
					"fail_fast": {
						StopOnFailure:    true,
						MaxExecutionTime: "1ns",
					},
					"collect_all": {
						StopOnFailure: false,
					},
					"default_policy": {
						StopOnFailure: true,
					},
				},
				ErrorHandling: ErrorHandling{
					ExecutionPolicy: "collect_all",
				},
			},
			want: Policy{
				StopOnFailure:    false,
				MaxExecutionTime: 5 * time.Second,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc := &RulesetConfig{
				ExecutionPolicies: tt.fields.ExecutionPolicies,
				ErrorHandling:     tt.fields.ErrorHandling,
			}
			got, err := rc.ToExecutionPolicy()
			if (err != nil) != tt.wantErr {
				t.Errorf("GetExecutionPolicy() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetExecutionPolicy() got = %v, want %v", got, tt.want)
			}
		})
	}
}
