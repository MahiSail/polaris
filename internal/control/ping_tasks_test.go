package control

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPingTasksPublicAndPerNodeReconcile(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := t.Context()
	op, _, err := store.EnsureDefaultAdmin(ctx)
	if err != nil {
		t.Fatal(err)
	}

	server, err := NewServer(store, false)
	if err != nil {
		t.Fatal(err)
	}

	// Create two nodes
	_, err = store.db.ExecContext(ctx, `INSERT INTO nodes (id, name, public_key, created_at) VALUES ('node-a', 'Node A', 'pk-a', 1700000000), ('node-b', 'Node B', 'pk-b', 1700000000)`)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Save a public probe
	pubProbe := PingTask{
		Name:     "Cloudflare",
		Target:   "1.1.1.1:443",
		Interval: 60,
		AutoJoin: true,
	}
	if err := store.SavePingTask(ctx, &pubProbe); err != nil {
		t.Fatalf("SavePingTask public: %v", err)
	}

	// 2. Sync dedicated probes for node-a
	if err := store.SyncNodePingTasks(ctx, "node-a", []PingTask{
		{Name: "NodeA-CT", Target: "202.103.224.68:53", Interval: 30},
	}); err != nil {
		t.Fatalf("SyncNodePingTasks node-a: %v", err)
	}

	// 3. Reconcile and verify tasks dispatched to node-a vs node-b
	if err := server.reconcilePingTasks(ctx, op.ID); err != nil {
		t.Fatalf("reconcilePingTasks: %v", err)
	}

	tasksA, err := store.PendingTasks(ctx, "node-a")
	if err != nil || len(tasksA) == 0 {
		t.Fatalf("expected pending probe.configure task for node-a, got %v (err=%v)", tasksA, err)
	}
	var payloadA []map[string]string
	if err := json.Unmarshal([]byte(tasksA[len(tasksA)-1].Payload), &payloadA); err != nil {
		t.Fatal(err)
	}
	if len(payloadA) != 2 {
		t.Fatalf("expected node-a to receive 2 probes (1 public + 1 custom), got %d: %#v", len(payloadA), payloadA)
	}

	tasksB, err := store.PendingTasks(ctx, "node-b")
	if err != nil || len(tasksB) == 0 {
		t.Fatalf("expected pending probe.configure task for node-b, got %v (err=%v)", tasksB, err)
	}
	var payloadB []map[string]string
	if err := json.Unmarshal([]byte(tasksB[len(tasksB)-1].Payload), &payloadB); err != nil {
		t.Fatal(err)
	}
	if len(payloadB) != 1 || payloadB[0]["name"] != "Cloudflare" {
		t.Fatalf("expected node-b to receive only 1 public probe, got %#v", payloadB)
	}

	// 4. Attempt to modify public probe in node-a specific configuration must fail
	if err := store.SyncNodePingTasks(ctx, "node-a", []PingTask{
		{ID: pubProbe.ID, Name: "TamperedPublic", Target: "8.8.8.8:53", Interval: 10},
	}); err == nil || !strings.Contains(err.Error(), "禁止在服务器单独配置中修改公共探针") {
		t.Fatalf("expected error containing '禁止在服务器单独配置中修改公共探针', got %v", err)
	}

	// 5. Clear dedicated probes on node-a and verify public probe remains intact
	if err := store.SyncNodePingTasks(ctx, "node-a", []PingTask{}); err != nil {
		t.Fatalf("SyncNodePingTasks empty node-a: %v", err)
	}
	allTasks, err := store.ListPingTasks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(allTasks) != 1 || !allTasks[0].AutoJoin || allTasks[0].Name != "Cloudflare" {
		t.Fatalf("expected only public probe to remain, got %#v", allTasks)
	}
}
