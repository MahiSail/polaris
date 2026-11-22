package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestCloseConnectionTask(t *testing.T) {
	var mu sync.Mutex
	deletedIDs := make([]string, 0)
	deletedAll := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodDelete {
			if r.URL.Path == "/connections" {
				deletedAll = true
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if len(r.URL.Path) > len("/connections/") {
				id := r.URL.Path[len("/connections/"):]
				deletedIDs = append(deletedIDs, id)
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	orig := clashAPIBase
	clashAPIBase = server.URL
	defer func() { clashAPIBase = orig }()

	// 1. 测试关闭指定连接
	taskSingle := Task{
		ID:      "t-1",
		Kind:    "connection.close",
		Payload: `{"id":"conn-123"}`,
	}
	resSingle := closeConnection(context.Background(), taskSingle)
	if resSingle.Status != "succeeded" {
		t.Fatalf("expected succeeded, got %s: %s", resSingle.Status, resSingle.Summary)
	}

	mu.Lock()
	if len(deletedIDs) != 1 || deletedIDs[0] != "conn-123" {
		t.Fatalf("expected conn-123 to be deleted, got %v", deletedIDs)
	}
	mu.Unlock()

	// 2. 测试关闭所有连接
	taskAll := Task{
		ID:      "t-2",
		Kind:    "connection.close",
		Payload: `{"id":"all"}`,
	}
	resAll := closeConnection(context.Background(), taskAll)
	if resAll.Status != "succeeded" {
		t.Fatalf("expected succeeded, got %s: %s", resAll.Status, resAll.Summary)
	}

	mu.Lock()
	if !deletedAll {
		t.Fatalf("expected deletedAll to be true")
	}
	mu.Unlock()
}
