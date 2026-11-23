package control_test

import (
	"strings"
	"testing"

	"github.com/MahiSail/polaris/internal/control"
	gopkgyaml "gopkg.in/yaml.v3"
)

// TestMihomoClientRuleFormAndTextSynchronization verifies that Table mode and Text mode
// operate on a single source of truth:
// 1. Creating/updating via table populates and formats RawRules.
// 2. Creating/updating via text parses and populates Rules.
// 3. Subscription YAML generates identical rules regardless of RuleMode.
// 4. Missing fields in historical data are automatically backfilled on read.
func TestMihomoClientRuleFormAndTextSynchronization(t *testing.T) {
	store, groupID := newMihomoDNSFixture(t)

	// Case 1: Table mode creation
	tableCfg := control.MihomoClientConfig{
		Name:          "表格分流配置",
		ProxyGroupIDs: []string{groupID},
		RuleMode:      "table",
		Rules: []control.MihomoRule{
			{Type: "DOMAIN-SUFFIX", Value: "google.com", Action: "DNS 分组"},
			{Type: "IP-CIDR", Value: "1.1.1.0/24", Action: "REJECT", NoResolve: true},
			{Type: "MATCH", Action: "DIRECT"},
		},
	}
	createdTable, err := store.CreateMihomoClientConfig(t.Context(), tableCfg)
	if err != nil {
		t.Fatalf("create table client config: %v", err)
	}

	// Verify RawRules was automatically formatted
	expectedRaw := "DOMAIN-SUFFIX,google.com,DNS 分组\nIP-CIDR,1.1.1.0/24,REJECT,no-resolve\nMATCH,DIRECT"
	if createdTable.RawRules != expectedRaw {
		t.Fatalf("expected RawRules:\n%s\ngot:\n%s", expectedRaw, createdTable.RawRules)
	}
	if len(createdTable.Rules) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(createdTable.Rules))
	}

	// Case 2: Text mode creation
	textCfg := control.MihomoClientConfig{
		Name:          "文本分流配置",
		ProxyGroupIDs: []string{groupID},
		RuleMode:      "text",
		RawRules:      "# 包含注释\n- DOMAIN-SUFFIX,google.com,DNS 分组\nIP-CIDR,1.1.1.0/24,REJECT,no-resolve\nMATCH,DIRECT",
	}
	createdText, err := store.CreateMihomoClientConfig(t.Context(), textCfg)
	if err != nil {
		t.Fatalf("create text client config: %v", err)
	}

	// Verify Rules was automatically parsed and populated
	if len(createdText.Rules) != 3 {
		t.Fatalf("expected 3 parsed rules, got %d", len(createdText.Rules))
	}
	if createdText.Rules[0].Type != "DOMAIN-SUFFIX" || createdText.Rules[0].Value != "google.com" || createdText.Rules[0].Action != "DNS 分组" {
		t.Fatalf("unexpected rule 0: %#v", createdText.Rules[0])
	}
	if createdText.Rules[1].Type != "IP-CIDR" || !createdText.Rules[1].NoResolve || createdText.Rules[1].Action != "REJECT" {
		t.Fatalf("unexpected rule 1: %#v", createdText.Rules[1])
	}
	if createdText.Rules[2].Type != "MATCH" || createdText.Rules[2].Action != "DIRECT" {
		t.Fatalf("unexpected rule 2: %#v", createdText.Rules[2])
	}

	// Case 3: Subscription YAML output must be identical for both configs
	_, tableYAML, err := store.GenerateStoredMihomoYAML(t.Context(), createdTable.ID)
	if err != nil {
		t.Fatalf("generate table YAML: %v", err)
	}
	_, textYAML, err := store.GenerateStoredMihomoYAML(t.Context(), createdText.ID)
	if err != nil {
		t.Fatalf("generate text YAML: %v", err)
	}

	var tableDoc, textDoc struct {
		Rules []string `yaml:"rules"`
	}
	if err := gopkgyaml.Unmarshal([]byte(tableYAML), &tableDoc); err != nil {
		t.Fatalf("parse table YAML: %v", err)
	}
	if err := gopkgyaml.Unmarshal([]byte(textYAML), &textDoc); err != nil {
		t.Fatalf("parse text YAML: %v", err)
	}

	if len(tableDoc.Rules) != 3 || len(textDoc.Rules) != 3 {
		t.Fatalf("expected 3 rules in subscription, table=%d, text=%d", len(tableDoc.Rules), len(textDoc.Rules))
	}
	for i := range tableDoc.Rules {
		if tableDoc.Rules[i] != textDoc.Rules[i] {
			t.Fatalf("rule %d mismatch: table=%q, text=%q", i, tableDoc.Rules[i], textDoc.Rules[i])
		}
	}

	// Case 4: Switching mode from text to table without rules array (fallback)
	createdText.RuleMode = "table"
	createdText.Rules = nil // simulated frontend leaving rules empty
	updated, err := store.UpdateMihomoClientConfig(t.Context(), createdText)
	if err != nil {
		t.Fatalf("update mode to table with empty rules: %v", err)
	}
	if len(updated.Rules) != 3 {
		t.Fatalf("expected 3 rules preserved after mode switch, got %d", len(updated.Rules))
	}
	if !strings.Contains(updated.RawRules, "DOMAIN-SUFFIX,google.com,DNS 分组") {
		t.Fatalf("expected RawRules preserved, got:\n%s", updated.RawRules)
	}
}
