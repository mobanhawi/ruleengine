package ruleengine

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Configuration structures matching the YAML file
type RulesetConfig struct {
	APIVersion        string                     `yaml:"apiVersion"`
	Kind              string                     `yaml:"kind"`
	Metadata          Metadata                   `yaml:"metadata"`
	Globals           map[string]interface{}     `yaml:"globals"`
	Rules             map[string]Rule            `yaml:"rules"`
	Rulesets          map[string]Ruleset         `yaml:"rulesets"`
	ExecutionPolicies map[string]ExecutionPolicy `yaml:"execution_policies"`
	ErrorHandling     ErrorHandling              `yaml:"error_handling"`
	Environments      map[string]Environment     `yaml:"environments"`
}

type Rule struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Expression  string `yaml:"expression"`
}

type Ruleset struct {
	Name            string          `yaml:"name"`
	Description     string          `yaml:"description"`
	CombinationType string          `yaml:"combination_type"`
	Rules           []string        `yaml:"rules"`
	CustomRules     map[string]Rule `yaml:"custom_rules"`
	Expression      string          `yaml:"expression"`
	Extends         string          `yaml:"extends"`
	AdditionalRules []string        `yaml:"additional_rules"`
}

type Metadata struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

type ExecutionPolicy struct {
	Name             string `yaml:"name"`
	Description      string `yaml:"description"`
	StopOnFailure    bool   `yaml:"stop_on_failure"`
	CollectErrors    bool   `yaml:"collect_errors"`
	MaxExecutionTime string `yaml:"max_execution_time"`
	TimeoutBehavior  string `yaml:"timeout_behavior"`
}

type ErrorHandling struct {
	DefaultPolicy       string            `yaml:"default_policy"`
	LogLevel            string            `yaml:"log_level"`
	CustomErrorMessages map[string]string `yaml:"custom_error_messages"`
}

type Environment struct {
	Globals           map[string]interface{}   `yaml:"globals"`
	ExecutionPolicies ExecutionPolicies        `yaml:"execution_policies"`
	ErrorHandling     EnvironmentErrorHandling `yaml:"error_handling"`
}

type ExecutionPolicies struct {
	Default string `yaml:"default"`
}

type EnvironmentErrorHandling struct {
	Default string `yaml:"default"`
}

func loadConfig(configPath string) (*RulesetConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var config RulesetConfig
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, err
	}

	return &config, nil
}
