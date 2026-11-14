package ruleengine

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// SelectorType defines the logical combinator for rules within a ruleset.
type SelectorType string

const (
	// SelectorAnd requires all rules in the ruleset to pass.
	SelectorAnd SelectorType = "AND"
	// SelectorOr requires at least one rule in the ruleset to pass.
	SelectorOr SelectorType = "OR"
	// SelectorNone requires all rules in the ruleset to evaluate to false (logical NOR/NOT).
	SelectorNone SelectorType = "NONE"

	defaultMaxExecutionTime = 5 * time.Second
)

// ErrPolicyNotFound is returned when a referenced execution policy does not exist in config.
var ErrPolicyNotFound = errors.New("execution policy not found")

// RulesetConfig is the top-level configuration structure.
type RulesetConfig struct {
	APIVersion        string                     `yaml:"apiVersion"`
	Kind              string                     `yaml:"kind"`
	Metadata          Metadata                   `yaml:"metadata"`
	Globals           map[string]any             `yaml:"globals"`
	Rules             map[string]Rule            `yaml:"rules"`
	Rulesets          map[string]Ruleset         `yaml:"rulesets"`
	ExecutionPolicies map[string]ExecutionPolicy `yaml:"execution_policies"`
	ErrorHandling     ErrorHandling              `yaml:"error_handling"`
	Environments      map[string]Environment     `yaml:"environments"`
}

// Rule represents an individual rule with its properties.
type Rule struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Expression  string   `yaml:"expression"`
	Extends     string   `yaml:"extends"`
	Tags        []string `yaml:"tags,omitempty"`
}

// Ruleset represents a collection of rules and their evaluation logic.
type Ruleset struct {
	Name        string       `yaml:"name"`
	Description string       `yaml:"description"`
	Selector    SelectorType `yaml:"selector"`
	MinPassed   int          `yaml:"min_passed,omitempty"`
	Rules       []string     `yaml:"rules"`
	Tags        []string     `yaml:"tags,omitempty"`
}

// Metadata contains basic information about the ruleset configuration.
type Metadata struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// ExecutionPolicy defines how rulesets should be executed.
type ExecutionPolicy struct {
	Name             string `yaml:"name"`
	Description      string `yaml:"description"`
	StopOnFailure    bool   `yaml:"stop_on_failure"`
	MaxExecutionTime string `yaml:"max_execution_time"`
}

// ErrorHandling defines error handling settings for the rule engine.
type ErrorHandling struct {
	ExecutionPolicy     string            `yaml:"execution_policy"`
	CustomErrorMessages map[string]string `yaml:"custom_error_messages"`
}

// Environment defines settings for different execution environments.
type Environment struct {
	Globals       map[string]any `yaml:"globals"`
	ErrorHandling ErrorHandling  `yaml:"error_handling"`
}

// NewRulesetConfig reads and parses the YAML configuration file at configPath.
func NewRulesetConfig(configPath string) (*RulesetConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("reading config file %q: %w", configPath, err)
	}
	return NewRulesetConfigFromBytes(data)
}

// NewRulesetConfigFromReader reads and parses a YAML configuration from an io.Reader.
func NewRulesetConfigFromReader(r io.Reader) (*RulesetConfig, error) {
	if r == nil {
		return nil, errors.New("config reader is nil")
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading config stream: %w", err)
	}
	return NewRulesetConfigFromBytes(data)
}

// NewRulesetConfigFromBytes parses a YAML configuration from a byte slice.
func NewRulesetConfigFromBytes(data []byte) (*RulesetConfig, error) {
	var config RulesetConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("unmarshaling config yaml: %w", err)
	}
	return &config, nil
}

// ApplyEnvironment applies environment-specific overrides to the configuration.
func (rc *RulesetConfig) ApplyEnvironment(environment string) {
	envConfig, exists := rc.Environments[environment]
	if !exists {
		return
	}

	if len(envConfig.Globals) > 0 {
		if rc.Globals == nil {
			rc.Globals = make(map[string]any, len(envConfig.Globals))
		}
		for k, v := range envConfig.Globals {
			rc.Globals[k] = v
		}
	}

	if envConfig.ErrorHandling.ExecutionPolicy != "" {
		rc.ErrorHandling.ExecutionPolicy = envConfig.ErrorHandling.ExecutionPolicy
	}

	if len(envConfig.ErrorHandling.CustomErrorMessages) > 0 {
		if rc.ErrorHandling.CustomErrorMessages == nil {
			rc.ErrorHandling.CustomErrorMessages = make(map[string]string, len(envConfig.ErrorHandling.CustomErrorMessages))
		}
		for k, v := range envConfig.ErrorHandling.CustomErrorMessages {
			rc.ErrorHandling.CustomErrorMessages[k] = v
		}
	}
}

// ToExecutionPolicy maps the execution policy from the current configuration.
func (rc *RulesetConfig) ToExecutionPolicy() (Policy, error) {
	policy := Policy{
		StopOnFailure:    true,
		MaxExecutionTime: defaultMaxExecutionTime,
	}

	configPolicy, ok := rc.ExecutionPolicies[rc.ErrorHandling.ExecutionPolicy]
	if !ok {
		return policy, fmt.Errorf("policy %q: %w", rc.ErrorHandling.ExecutionPolicy, ErrPolicyNotFound)
	}

	if configPolicy.MaxExecutionTime != "" {
		dur, err := time.ParseDuration(configPolicy.MaxExecutionTime)
		if err != nil {
			return policy, fmt.Errorf("parsing max_execution_time %q: %w", configPolicy.MaxExecutionTime, err)
		}
		if dur <= 0 {
			return policy, fmt.Errorf("max_execution_time must be positive, got %v", dur)
		}
		policy.MaxExecutionTime = dur
	}
	policy.StopOnFailure = configPolicy.StopOnFailure

	return policy, nil
}
