package control

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSanitizeASCIIFilename(t *testing.T) {
	tests := []struct {
		input    string
		fallback string
		want     string
	}{
		{"my-config", "sub", "my-config"},
		{"E2E 客户端", "sub", "E2E"},
		{"默认配置", "sub", "sub"},
		{"---", "sub", "sub"},
		{"a--b--c", "sub", "a-b-c"},
		{"", "default", "default"},
	}
	for _, tc := range tests {
		got := sanitizeASCIIFilename(tc.input, tc.fallback)
		if got != tc.want {
			t.Errorf("sanitizeASCIIFilename(%q, %q) = %q, want %q", tc.input, tc.fallback, got, tc.want)
		}
	}
}

func TestMihomoSubscriptionMetadataHeaders(t *testing.T) {
	ctx := t.Context()
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	nodeID := "node-sub-1"
	if _, err := store.db.Exec(`INSERT INTO nodes (id, name, public_key, client_address, revoked_at, created_at) VALUES (?, ?, ?, ?, NULL, ?)`,
		nodeID, "香港服务器", make([]byte, 32), "hk.example.com", nowUnix()); err != nil {
		t.Fatalf("insert node failed: %v", err)
	}

	listener, err := store.CreateListener(ctx, Listener{
		NodeID:     nodeID,
		Name:       "vless-in",
		Domain:     "hk.example.com",
		ListenAddr: "0.0.0.0",
		Port:       443,
		Enabled:    true,
		Spec: ProtocolSpec{
			Protocol:  "vless",
			Network:   "tcp",
			TLS:       TLSOptions{Enabled: true},
			Transport: TransportOptions{Type: "httpupgrade"},
		},
	})
	if err != nil {
		t.Fatalf("CreateListener failed: %v", err)
	}

	endpoint, err := store.CreateEndpoint(ctx, Endpoint{
		ListenerID: listener.ID,
		Name:       "user_sub_001",
		Alias:      "香港高速节点",
		Enabled:    true,
	}, EndpointCredentials{
		UUID: "a0000000-0000-0000-0000-000000000001",
	})
	if err != nil {
		t.Fatalf("CreateEndpoint failed: %v", err)
	}

	group, err := store.CreateMihomoProxyGroup(ctx, MihomoProxyGroup{
		Name:     "香港代理",
		Strategy: "select",
		Members: []MihomoGroupMember{
			{Kind: "endpoint", ID: endpoint.ID},
		},
	})
	if err != nil {
		t.Fatalf("CreateMihomoProxyGroup failed: %v", err)
	}

	expiryTime := time.Now().Add(30 * 24 * time.Hour).Truncate(time.Second)
	expiryUnix := expiryTime.Unix()
	config, err := store.CreateMihomoClientConfig(ctx, MihomoClientConfig{
		Name:            "高级香港订阅",
		ProxyGroupIDs:   []string{group.ID},
		AccessExpiresAt: expiryTime.Format(time.RFC3339),
		Rules: []MihomoRule{
			{Type: "MATCH", Action: group.Name},
		},
	})
	if err != nil {
		t.Fatalf("CreateMihomoClientConfig failed: %v", err)
	}

	server, err := NewServer(store, false)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, config.SubscriptionPath, nil)
	req.Host = "panel.polaris.example.com"
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("subscription pull returned status=%d, body=%s", rec.Code, rec.Body.String())
	}

	// 1. Profile-Title: base64 encoded UTF-8 name
	profileTitleHdr := rec.Header().Get("Profile-Title")
	wantTitle := "base64:" + base64.StdEncoding.EncodeToString([]byte(config.Name))
	if profileTitleHdr != wantTitle {
		t.Errorf("Profile-Title header = %q, want %q", profileTitleHdr, wantTitle)
	}

	// 2. Profile-Update-Interval: 24h
	updateIntervalHdr := rec.Header().Get("Profile-Update-Interval")
	if updateIntervalHdr != "24" {
		t.Errorf("Profile-Update-Interval header = %q, want 24", updateIntervalHdr)
	}

	// 3. Profile-Web-Page-Url
	webUrlHdr := rec.Header().Get("Profile-Web-Page-Url")
	if webUrlHdr != "http://panel.polaris.example.com/" {
		t.Errorf("Profile-Web-Page-Url header = %q, want http://panel.polaris.example.com/", webUrlHdr)
	}

	// 4. Subscription-Userinfo with traffic and expire timestamp
	subUserInfoHdr := rec.Header().Get("Subscription-Userinfo")
	wantUserInfo := fmt.Sprintf("upload=0; download=0; expire=%d", expiryUnix)
	if subUserInfoHdr != wantUserInfo {
		t.Errorf("Subscription-Userinfo header = %q, want %q", subUserInfoHdr, wantUserInfo)
	}

	// 5. Content-Disposition with UTF-8 and sanitized ASCII fallback
	contentDisposition := rec.Header().Get("Content-Disposition")
	if !strings.Contains(contentDisposition, `filename*=UTF-8''`) {
		t.Errorf("Content-Disposition missing UTF-8 filename: %q", contentDisposition)
	}

	// Test 304 Not Modified also includes metadata headers
	etag := rec.Header().Get("ETag")
	req304 := httptest.NewRequest(http.MethodGet, config.SubscriptionPath, nil)
	req304.Host = "panel.polaris.example.com"
	req304.Header.Set("If-None-Match", etag)
	rec304 := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec304, req304)

	if rec304.Code != http.StatusNotModified {
		t.Fatalf("subscription pull 304 returned status=%d", rec304.Code)
	}
	if rec304.Header().Get("Profile-Title") != wantTitle {
		t.Errorf("304 response missing Profile-Title header: %q", rec304.Header().Get("Profile-Title"))
	}
	if rec304.Header().Get("Subscription-Userinfo") != wantUserInfo {
		t.Errorf("304 response missing Subscription-Userinfo header: %q", rec304.Header().Get("Subscription-Userinfo"))
	}
}

func TestLegacyRuleModeAndDNSModeNormalization(t *testing.T) {
	ctx := t.Context()
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	nodeID := "node-sub-legacy"
	if _, err := store.db.Exec(`INSERT INTO nodes (id, name, public_key, client_address, revoked_at, created_at) VALUES (?, ?, ?, ?, NULL, ?)`,
		nodeID, "演示节点", make([]byte, 32), "node.example.com", nowUnix()); err != nil {
		t.Fatal(err)
	}

	listener, err := store.CreateListener(ctx, Listener{
		NodeID:     nodeID,
		Name:       "hy2-in",
		Domain:     "node.example.com",
		ListenAddr: "0.0.0.0",
		Port:       8443,
		Enabled:    true,
		Spec: ProtocolSpec{
			Protocol: "hysteria2",
			Network:  "udp",
			TLS:      TLSOptions{Enabled: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	endpoint, err := store.CreateEndpoint(ctx, Endpoint{
		ListenerID: listener.ID,
		Name:       "user_legacy",
		Alias:      "演示端点",
		Enabled:    true,
	}, EndpointCredentials{
		Password: "pass",
	})
	if err != nil {
		t.Fatal(err)
	}

	group, err := store.CreateMihomoProxyGroup(ctx, MihomoProxyGroup{
		Name:     "默认分组",
		Strategy: "select",
		Members:  []MihomoGroupMember{{Kind: "endpoint", ID: endpoint.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create with rule_mode="preset" and dns_mode="structured" (legacy/demo values)
	cfg := MihomoClientConfig{
		Name:          "兼容测试配置",
		ProxyGroupIDs: []string{group.ID},
		RuleMode:      "preset",
		DNSMode:       "structured",
		Rules: []MihomoRule{
			{Type: "MATCH", Action: group.Name},
		},
	}
	saved, err := store.CreateMihomoClientConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("CreateMihomoClientConfig failed on legacy modes: %v", err)
	}

	// Generating YAML should succeed without "rule mode must be table or text" error
	name, yaml, err := store.GenerateStoredMihomoYAML(ctx, saved.ID)
	if err != nil {
		t.Fatalf("GenerateStoredMihomoYAML failed: %v", err)
	}
	if name != "兼容测试配置" || !strings.Contains(yaml, "proxies:") {
		t.Fatalf("GenerateStoredMihomoYAML output invalid: %s", yaml)
	}
}

func TestGeneralSubscriptionMetadataHeaders(t *testing.T) {
	ctx := t.Context()
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	nodeID := "node-sub-2"
	if _, err := store.db.Exec(`INSERT INTO nodes (id, name, public_key, client_address, revoked_at, created_at) VALUES (?, ?, ?, ?, NULL, ?)`,
		nodeID, "日本服务器", make([]byte, 32), "jp.example.com", nowUnix()); err != nil {
		t.Fatalf("insert node failed: %v", err)
	}

	listener, err := store.CreateListener(ctx, Listener{
		NodeID:     nodeID,
		Name:       "vless-jp",
		Domain:     "jp.example.com",
		ListenAddr: "0.0.0.0",
		Port:       443,
		Enabled:    true,
		Spec: ProtocolSpec{
			Protocol:  "vless",
			Network:   "tcp",
			TLS:       TLSOptions{Enabled: true},
			Transport: TransportOptions{Type: "httpupgrade"},
		},
	})
	if err != nil {
		t.Fatalf("CreateListener failed: %v", err)
	}

	endpoint, err := store.CreateEndpoint(ctx, Endpoint{
		ListenerID: listener.ID,
		Name:       "user_sub_002",
		Alias:      "日本节点",
		Enabled:    true,
	}, EndpointCredentials{
		UUID: "b0000000-0000-0000-0000-000000000002",
	})
	if err != nil {
		t.Fatalf("CreateEndpoint failed: %v", err)
	}

	sub, token, err := store.CreateSubscription(ctx, SubscriptionInput{
		Kind:        "client",
		Name:        "通用测试订阅",
		EndpointIDs: []string{endpoint.ID},
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("CreateSubscription failed: %v", err)
	}

	server, err := NewServer(store, false)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/subscriptions/access/"+token, nil)
	req.Host = "sub.polaris.example.com"
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("general subscription returned code=%d body=%s", rec.Code, rec.Body.String())
	}

	title := rec.Header().Get("Profile-Title")
	wantTitle := "base64:" + base64.StdEncoding.EncodeToString([]byte(sub.Name))
	if title != wantTitle {
		t.Errorf("general subscription title = %q, want %q", title, wantTitle)
	}

	if rec.Header().Get("Profile-Update-Interval") != "24" {
		t.Errorf("general subscription update interval = %q, want 24", rec.Header().Get("Profile-Update-Interval"))
	}

	if rec.Header().Get("Profile-Web-Page-Url") != "http://sub.polaris.example.com/" {
		t.Errorf("general subscription web page url = %q, want http://sub.polaris.example.com/", rec.Header().Get("Profile-Web-Page-Url"))
	}

	if rec.Header().Get("Subscription-Userinfo") != "upload=0; download=0" {
		t.Errorf("general subscription user info = %q, want %q", rec.Header().Get("Subscription-Userinfo"), "upload=0; download=0")
	}
}

func TestSubscriptionUserInfoWithTrafficAndQuota(t *testing.T) {
	// 1. Test unit parser
	if got := parseTrafficPlanBytes("2 TB"); got != 2*1024*1024*1024*1024 {
		t.Errorf("parseTrafficPlanBytes('2 TB') = %d, want %d", got, int64(2*1024*1024*1024*1024))
	}
	if got := parseTrafficPlanBytes("500 GB"); got != 500*1024*1024*1024 {
		t.Errorf("parseTrafficPlanBytes('500 GB') = %d, want %d", got, int64(500*1024*1024*1024))
	}
	if got := parseTrafficPlanBytes("2000"); got != 2000*1024*1024*1024 {
		t.Errorf("parseTrafficPlanBytes('2000') = %d, want %d", got, int64(2000*1024*1024*1024))
	}

	// 2. Test formatter
	if got := formatSubscriptionUserInfo(100, 200, 1000, 1700000000); got != "upload=100; download=200; total=1000; expire=1700000000" {
		t.Errorf("formatSubscriptionUserInfo = %q", got)
	}
	if got := formatSubscriptionUserInfo(0, 0, 0, 0); got != "upload=0; download=0" {
		t.Errorf("formatSubscriptionUserInfo zero = %q", got)
	}

	// 3. Database integration with node hosting plan & connection_records
	ctx := t.Context()
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	nodeID := "node-traffic-1"
	if _, err := store.db.Exec(`INSERT INTO nodes (id, name, public_key, client_address, traffic_plan, revoked_at, created_at) VALUES (?, ?, ?, ?, ?, NULL, ?)`,
		nodeID, "专线服务器", make([]byte, 32), "hk-traffic.example.com", "1 TB", nowUnix()); err != nil {
		t.Fatalf("insert node failed: %v", err)
	}

	listener, err := store.CreateListener(ctx, Listener{
		NodeID:     nodeID,
		Name:       "vless-in",
		Domain:     "hk-traffic.example.com",
		ListenAddr: "0.0.0.0",
		Port:       443,
		Enabled:    true,
		Spec: ProtocolSpec{
			Protocol:  "vless",
			Network:   "tcp",
			TLS:       TLSOptions{Enabled: true},
			Transport: TransportOptions{Type: "httpupgrade"},
		},
	})
	if err != nil {
		t.Fatalf("CreateListener failed: %v", err)
	}

	endpoint, err := store.CreateEndpoint(ctx, Endpoint{
		ListenerID: listener.ID,
		Name:       "vip_user_007",
		Alias:      "香港专线节点",
		Enabled:    true,
	}, EndpointCredentials{
		UUID: "c0000000-0000-0000-0000-000000000007",
	})
	if err != nil {
		t.Fatalf("CreateEndpoint failed: %v", err)
	}

	// Record traffic under alias
	if err := store.SaveConnectionRecords(ctx, []ConnectionRecord{
		{
			ID:        "rec-1",
			NodeID:    nodeID,
			User:      "香港专线节点",
			Upload:    1048576,  // 1 MB
			Download:  10485760, // 10 MB
			StartedAt: time.Now().Add(-time.Hour).Format(time.RFC3339),
		},
		{
			ID:        "rec-2",
			NodeID:    nodeID,
			User:      "vip_user_007", // matching name
			Upload:    2097152,        // 2 MB
			Download:  20971520,       // 20 MB
			StartedAt: time.Now().Add(-30 * time.Minute).Format(time.RFC3339),
		},
	}); err != nil {
		t.Fatalf("SaveConnectionRecords failed: %v", err)
	}

	group, err := store.CreateMihomoProxyGroup(ctx, MihomoProxyGroup{
		Name:     "专线代理组",
		Strategy: "select",
		Members:  []MihomoGroupMember{{Kind: "endpoint", ID: endpoint.ID}},
	})
	if err != nil {
		t.Fatalf("CreateMihomoProxyGroup failed: %v", err)
	}

	expireTime := time.Now().Add(7 * 24 * time.Hour).Truncate(time.Second)
	config, err := store.CreateMihomoClientConfig(ctx, MihomoClientConfig{
		Name:            "专线订阅",
		ProxyGroupIDs:   []string{group.ID},
		AccessExpiresAt: expireTime.Format(time.RFC3339),
		Rules:           []MihomoRule{{Type: "MATCH", Action: group.Name}},
	})
	if err != nil {
		t.Fatalf("CreateMihomoClientConfig failed: %v", err)
	}

	server, err := NewServer(store, false)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, config.SubscriptionPath, nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("subscription pull status = %d", rec.Code)
	}

	// Total upload = 1MB + 2MB = 3145728
	// Total download = 10MB + 20MB = 31457280
	// Total quota = 1 TB = 1099511627776
	wantHeader := fmt.Sprintf("upload=3145728; download=31457280; total=%d; expire=%d",
		int64(1*1024*1024*1024*1024), expireTime.Unix())
	gotHeader := rec.Header().Get("Subscription-Userinfo")
	if gotHeader != wantHeader {
		t.Errorf("Subscription-Userinfo header = %q, want %q", gotHeader, wantHeader)
	}
}
