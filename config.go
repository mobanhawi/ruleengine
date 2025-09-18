package ruleengine

import (
	"os"

	"gopkg.in/yaml.v3"
)

// RulesetConfig is the top-level configuration structure
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

// Rule represents an individual rule with its properties
type Rule struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Expression  string `yaml:"expression"`
}

// Ruleset represents a collection of rules and their evaluation logic
type Ruleset struct {
	Name            string                 `yaml:"name"`
	Description     string                 `yaml:"description"`
	CombinationType RulesetCombinationType `yaml:"combination_type"`
	Rules           []string               `yaml:"rules"`
	CustomRules     map[string]Rule        `yaml:"custom_rules"`
	Expression      string                 `yaml:"expression"`
	Extends         string                 `yaml:"extends"`
}

type RulesetCombinationType string

// Metadata contains basic information about the ruleset configuration
type Metadata struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// ExecutionPolicy defines how rulesets should be executed
type ExecutionPolicy struct {
	Name             string `yaml:"name"`
	Description      string `yaml:"description"`
	StopOnFailure    bool   `yaml:"stop_on_failure"`
	MaxExecutionTime string `yaml:"max_execution_time"`
}

// ErrorHandling defines error handling settings for the rule engine
type ErrorHandling struct {
	ExecutionPolicy     string            `yaml:"execution_policy"`
	CustomErrorMessages map[string]string `yaml:"custom_error_messages"`
}

// Environment defines settings for different execution environments
type Environment struct {
	Globals         map[string]interface{} `yaml:"globals"`
	ExecutionPolicy string                 `yaml:"execution_policy"`
	ErrorHandling   ErrorHandling          `yaml:"error_handling"`
}

// loadConfig reads and parses the YAML configuration file
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
