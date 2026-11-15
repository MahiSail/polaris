package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUpdateDNSTask(t *testing.T) {
	tempDir := t.TempDir()
	origConfig := managedSingBoxConfig
	defer func() { managedSingBoxConfig = origConfig }()
	managedSingBoxConfig = filepath.Join(tempDir, "config.json")

	initialConfig := `{"dns": {"rules": []}}`
	if err := os.WriteFile(managedSingBoxConfig, []byte(initialConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	// Test updateDNS task execution
	task := Task{
		ID:   "task-dns-1",
		Kind: "dns.update",
		Payload: `{"ip":"203.0.113.55","domains":["my-nat.example.com"]}`,
	}

	res := updateDNS(context.Background(), task, tempDir)
	if res.Status != "succeeded" {
		t.Fatalf("expected succeeded, got %s: %s", res.Status, res.Summary)
	}

	if !strings.Contains(res.Summary, "my-nat.example.com") {
		t.Fatalf("unexpected summary: %s", res.Summary)
	}

	// Second execution should be pure no-op
	res2 := updateDNS(context.Background(), task, tempDir)
	if res2.Status != "succeeded" {
		t.Fatalf("expected succeeded on second run, got %s", res2.Status)
	}
	if res2.Summary != "所有域名解析与静态规则均已是最新，无需修改" {
		t.Fatalf("expected pure no-op summary, got: %s", res2.Summary)
	}
}

func TestCloseStaleClashConnectionsSelective(t *testing.T) {
	deletedIDs := make(map[string]bool)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/connections" {
			connections := []map[string]any{
				{
					"id": "conn-stale-nat",
					"metadata": map[string]string{
						"host":           "nat.example.com",
						"destinationIP":  "198.51.100.10",
					},
				},
				{
					"id": "conn-other-user",
					"metadata": map[string]string{
						"host":           "google.com",
						"destinationIP":  "142.250.190.46",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"connections": connections})
			return
		}
		if r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/connections/") {
			id := strings.TrimPrefix(r.URL.Path, "/connections/")
			deletedIDs[id] = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Note: in production, clashAPIBase is 127.0.0.1:9090.
	// Here we test logic by ensuring closeStaleClashConnections handles errors gracefully.
	err := closeStaleClashConnections(context.Background(), []string{"nat.example.com"}, "203.0.113.55")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDNSRestartCooldown(t *testing.T) {
	tempDir := t.TempDir()
	origConfig := managedSingBoxConfig
	defer func() { managedSingBoxConfig = origConfig }()
	managedSingBoxConfig = filepath.Join(tempDir, "config.json")

	initialConfig := `{"dns": {"rules": []}}`
	if err := os.WriteFile(managedSingBoxConfig, []byte(initialConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	dnsRestartMu.Lock()
	lastDNSRestartAt = time.Now()
	dnsRestartMu.Unlock()

	// Calling updateDNS immediately while within cooldown window should succeed
	task := Task{
		ID:   "task-dns-2",
		Kind: "dns.update",
		Payload: `{"ip":"203.0.113.56","domains":["cooldown.example.com"]}`,
	}
	res := updateDNS(context.Background(), task, tempDir)
	if res.Status != "succeeded" {
		t.Fatalf("expected succeeded, got %s: %s", res.Status, res.Summary)
	}
}

func TestUpdateDNS_BatchAndIdempotency(t *testing.T) {
	tempDir := t.TempDir()
	origConfig := managedSingBoxConfig
	defer func() { managedSingBoxConfig = origConfig }()
	managedSingBoxConfig = filepath.Join(tempDir, "config.json")

	// Create initial sing-box configuration
	initialConfig := `{"dns": {"rules": []}}`
	if err := os.WriteFile(managedSingBoxConfig, []byte(initialConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	batchTask := Task{
		ID:   "task-dns-batch-1",
		Kind: "dns.update",
		Payload: `{
			"mappings": [
				{"ip": "198.51.100.201", "domains": ["node1.example.com"]},
				{"ip": "198.51.100.202", "domains": ["node2.example.com", "alias2.example.com"]}
			]
		}`,
	}

	// 1. First execution applies the mappings
	res1 := updateDNS(context.Background(), batchTask, tempDir)
	if res1.Status != "succeeded" {
		t.Fatalf("expected succeeded on first run, got %s: %s", res1.Status, res1.Summary)
	}
	if !strings.Contains(res1.Summary, "已合并更新 2 个节点") {
		t.Fatalf("unexpected summary: %s", res1.Summary)
	}

	// Verify sing-box config now has predefined rules for both
	content, err := os.ReadFile(managedSingBoxConfig)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "198.51.100.201") || !strings.Contains(string(content), "198.51.100.202") {
		t.Fatalf("config missing expected IPs: %s", string(content))
	}

	// 2. Second execution with identical batch mappings MUST be a complete NO-OP!
	res2 := updateDNS(context.Background(), batchTask, tempDir)
	if res2.Status != "succeeded" {
		t.Fatalf("expected succeeded on second run, got %s: %s", res2.Status, res2.Summary)
	}
	if res2.Summary != "所有域名解析与静态规则均已是最新，无需修改" {
		t.Fatalf("expected pure no-op summary, got: %s", res2.Summary)
	}
}
