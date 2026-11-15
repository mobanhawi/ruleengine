package config

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

const (
	testRulesYAMLPath       = "../testdata/rules.yml"
	testRulesJSONPath       = "../testdata/rules.json"
	testNonExistentYAMLPath = "../testdata/nonexistent.yml"
	defaultTimeout          = 5 * time.Second
)

func TestLoad(t *testing.T) {
	t.Run("GivenNonExistentPath_WhenLoad_ThenReturnsError", func(t *testing.T) {
		if _, err := Load(testNonExistentYAMLPath); err == nil {
			t.Fatal("Load(nonexistent) expected error, got nil")
		}
	})

	t.Run("GivenInvalidYAMLFile_WhenLoad_ThenReturnsError", func(t *testing.T) {
		if _, err := Load(testRulesJSONPath); err == nil {
			t.Fatal("Load(invalid) expected error, got nil")
		}
	})

	t.Run("GivenValidYAMLFile_WhenLoad_ThenReturnsParsedConfig", func(t *testing.T) {
		cfg, err := Load(testRulesYAMLPath)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.Metadata.Name != "cel-rulesets-example" {
			t.Errorf("cfg.Metadata.Name = %q, want %q", cfg.Metadata.Name, "cel-rulesets-example")
		}
	})
}

func TestLoadFromReader(t *testing.T) {
	t.Run("GivenNilReader_WhenLoadFromReader_ThenReturnsError", func(t *testing.T) {
		if _, err := LoadFromReader(nil); err == nil {
			t.Fatal("LoadFromReader(nil) expected error, got nil")
		}
	})

	t.Run("GivenValidYAMLReader_WhenLoadFromReader_ThenReturnsConfig", func(t *testing.T) {
		cfg, err := LoadFromReader(strings.NewReader("apiVersion: v1\nkind: RulesetConfig\n"))
		if err != nil {
			t.Fatalf("LoadFromReader() error = %v", err)
		}
		if cfg.APIVersion != "v1" {
			t.Errorf("cfg.APIVersion = %q, want v1", cfg.APIVersion)
		}
	})
}

func TestClone(t *testing.T) {
	t.Run("GivenConfigWithNestedMapsInsideSlices_WhenClonedAndSourceMutated_ThenCloneIsUnmodified", func(t *testing.T) {
		nestedItem := map[string]any{"tier": "gold"}
		original := &RulesetConfig{
			Globals: map[string]any{
				"tiers": []any{nestedItem},
			},
			Rules: map[string]Rule{
				"tier_rule": {
					Name:       "Tier Rule",
					Expression: "true",
					Tags:       []string{"billing"},
				},
			},
			Rulesets: map[string]Ruleset{
				"tier_set": {
					Name:     "Tier Set",
					Selector: SelectorAnd,
					Rules:    []string{"tier_rule"},
					Tags:     []string{"billing"},
				},
			},
		}

		cloned := original.Clone()

		nestedItem["tier"] = "mutated"
		original.Rules["tier_rule"].Tags[0] = "mutated_tag"
		original.Rulesets["tier_set"].Tags[0] = "mutated_tag"

		clonedSlice := cloned.Globals["tiers"].([]any)
		clonedMap := clonedSlice[0].(map[string]any)
		if diff := cmp.Diff(clonedMap["tier"], "gold"); diff != "" {
			t.Errorf("cloned nested map tier diff (-got +want):\n%s", diff)
		}
		if got := cloned.Rules["tier_rule"].Tags[0]; got != "billing" {
			t.Errorf("cloned rule tag = %q, want billing", got)
		}
		if got := cloned.Rulesets["tier_set"].Tags[0]; got != "billing" {
			t.Errorf("cloned ruleset tag = %q, want billing", got)
		}
	})
}

func TestToExecutionPolicy(t *testing.T) {
	t.Run("GivenEmptyConfig_WhenToExecutionPolicy_ThenReturnsDefaults", func(t *testing.T) {
		rc := &RulesetConfig{}
		got, err := rc.ToExecutionPolicy()
		if err != nil {
			t.Fatalf("ToExecutionPolicy() error = %v", err)
		}
		want := Policy{StopOnFailure: true, MaxExecutionTime: defaultTimeout}
		if diff := cmp.Diff(got, want); diff != "" {
			t.Errorf("ToExecutionPolicy() (-got +want):\n%s", diff)
		}
	})

	t.Run("GivenUnknownPolicy_WhenToExecutionPolicy_ThenReturnsErrPolicyNotFound", func(t *testing.T) {
		rc := &RulesetConfig{
			ErrorHandling: ErrorHandling{ExecutionPolicy: "missing"},
		}
		if _, err := rc.ToExecutionPolicy(); !errors.Is(err, ErrPolicyNotFound) {
			t.Fatalf("ToExecutionPolicy() error = %v, want errors.Is ErrPolicyNotFound", err)
		}
	})
}
