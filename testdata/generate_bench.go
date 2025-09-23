package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/mobanhawi/ruleengine"
)

//go:generate go run generate_bench.go ./rules.yml

func main() {
	generateBenchFiles(os.Args[1])
}

func generateBenchFiles(filePath string) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return
	}

	var config ruleengine.RulesetConfig
	var configBench ruleengine.RulesetConfig
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return
	}
	err = yaml.Unmarshal(data, &configBench)
	if err != nil {
		return
	}

	for i := 0; i < 100; i++ {
		for name, rule := range config.Rules {
			configBench.Rules[fmt.Sprintf("%s_%d", name, i)] = rule
		}
		for name, rule := range config.Rulesets {
			configBench.Rulesets[fmt.Sprintf("%s_%d", name, i)] = rule
		}
	}

	fmt.Println("Total rules:", len(configBench.Rules))
	fmt.Println("Total rulesets:", len(configBench.Rulesets))

	bytes, err := yaml.Marshal(&configBench)
	if err != nil {
		return
	}

	err = os.WriteFile("./rules_bench.yml", bytes, 0644)
	if err != nil {
		return
	}
}
