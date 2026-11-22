package control

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/liyuwei007036/polaris/internal/wire"
)

func TestCloseConnectionAPI(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	server, err := NewServer(store, false)
	if err != nil {
		t.Fatal(err)
	}

	handler := server.Handler()

	// 1. 未登录调用返回 401
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/nodes/node-1/connections/conn-1", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 unauthorized, got %d", w.Code)
	}

	// 创建已登录的 operator
	admin, _, err := store.EnsureDefaultAdmin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// 将 MustChangePassword 改为 false 便于测试
	_ = store.ChangeOwnPassword(context.Background(), admin.ID, DefaultAdminPassword, "NewAdminPass123!", "sess-test")
	sessData, err := store.createSession(context.Background(), admin.ID, admin.Role, false)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := sessData.Token
	csrf := sessData.CSRFToken

	// 2. 节点离线时调用返回错误
	req2 := httptest.NewRequest(http.MethodDelete, "/api/v1/nodes/node-offline/connections/conn-1", nil)
	req2.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionID})
	req2.Header.Set("X-CSRF-Token", csrf)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusBadRequest && w2.Code != http.StatusInternalServerError {
		t.Fatalf("expected error for offline node, got %d", w2.Code)
	}

	// 3. 模拟在线节点，测试关闭指定连接
	nodeID := "node-online"
	questions := make(chan wire.Task, 4)
	sess := &controlSession{
		done:      make(chan struct{}),
		questions: questions,
	}
	server.controlMu.Lock()
	server.controls[nodeID] = sess
	server.controlMu.Unlock()

	go func() {
		select {
		case q := <-questions:
			time.Sleep(10 * time.Millisecond)
			server.deliverTaskAnswer(wire.TaskResult{
				TaskID:  q.ID,
				Status:  "succeeded",
				Summary: "连接已关闭",
			})
		case <-time.After(2 * time.Second):
		}
	}()

	req3 := httptest.NewRequest(http.MethodDelete, "/api/v1/nodes/node-online/connections/conn-123", nil)
	req3.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionID})
	req3.Header.Set("X-CSRF-Token", csrf)
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, req3)
	if w3.Code != http.StatusNoContent {
		t.Fatalf("expected 204 no content for close connection, got %d: %s", w3.Code, w3.Body.String())
	}

	// 4. 测试关闭所有连接
	go func() {
		select {
		case q := <-questions:
			time.Sleep(10 * time.Millisecond)
			server.deliverTaskAnswer(wire.TaskResult{
				TaskID:  q.ID,
				Status:  "succeeded",
				Summary: "全部连接已关闭",
			})
		case <-time.After(2 * time.Second):
		}
	}()

	req4 := httptest.NewRequest(http.MethodDelete, "/api/v1/connections", nil)
	req4.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionID})
	req4.Header.Set("X-CSRF-Token", csrf)
	w4 := httptest.NewRecorder()
	handler.ServeHTTP(w4, req4)
	if w4.Code != http.StatusNoContent {
		t.Fatalf("expected 204 no content for close all connections, got %d: %s", w4.Code, w4.Body.String())
	}
}
