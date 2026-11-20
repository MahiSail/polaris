package agent

import (
	"archive/tar"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestNginxHTTPListenerDetection(t *testing.T) {
	for name, test := range map[string]struct {
		configuration string
		want          bool
	}{
		"default IPv4": {"server {\n listen 80 default_server;\n}", true},
		"default IPv6": {"server {\n listen [::]:80;\n}", true},
		"managed TCP":  {"server {\n listen 0.0.0.0:443;\n ssl_preread on;\n}", false},
		"comment":      {"# listen 80;", false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := nginxHasHTTPListener(test.configuration); got != test.want {
				t.Fatalf("nginxHasHTTPListener() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestRaiseNginxWorkerConnections(t *testing.T) {
	configuration := []byte("worker_processes auto;\nevents {\n\tworker_connections 768;\n}\n")
	updated, changed := raiseNginxWorkerConnections(configuration)
	if !changed || !strings.Contains(string(updated), "worker_connections 4096;") {
		t.Fatalf("worker connection limit was not raised:\n%s", updated)
	}
	updated, changed = raiseNginxWorkerConnections(updated)
	if changed || !strings.Contains(string(updated), "worker_connections 4096;") {
		t.Fatalf("adequate worker connection limit was changed:\n%s", updated)
	}
}

func TestRaiseNginxWorkerOpenFiles(t *testing.T) {
	configuration := []byte("worker_processes auto;\nevents {\n\tworker_connections 4096;\n}\n")
	updated, changed := raiseNginxWorkerOpenFiles(configuration)
	if !changed || !strings.HasPrefix(string(updated), "worker_rlimit_nofile 65535;\n") {
		t.Fatalf("worker file limit was not added:\n%s", updated)
	}
	updated, changed = raiseNginxWorkerOpenFiles(updated)
	if changed || strings.Count(string(updated), "worker_rlimit_nofile") != 1 {
		t.Fatalf("adequate worker file limit was changed:\n%s", updated)
	}

	configuration = []byte("worker_rlimit_nofile 1024;\nworker_processes auto;\n")
	updated, changed = raiseNginxWorkerOpenFiles(configuration)
	if !changed || !strings.Contains(string(updated), "worker_rlimit_nofile 65535;") {
		t.Fatalf("worker file limit was not raised:\n%s", updated)
	}
}

func TestInitialSingBoxConfigIsValidJSON(t *testing.T) {
	var configuration map[string]any
	if err := json.Unmarshal([]byte(initialSingBoxConfig), &configuration); err != nil {
		t.Fatalf("initial sing-box configuration is invalid JSON: %v", err)
	}
	if _, ok := configuration["outbounds"]; !ok {
		t.Fatal("initial sing-box configuration has no outbound")
	}
	experimental, ok := configuration["experimental"].(map[string]any)
	if !ok {
		t.Fatal("initial sing-box configuration has no local connection API")
	}
	clashAPI, ok := experimental["clash_api"].(map[string]any)
	if !ok || clashAPI["external_controller"] != "127.0.0.1:9090" {
		t.Fatalf("unexpected local connection API configuration: %#v", experimental)
	}
}

func TestOutboundHTTPProxyProbe(t *testing.T) {
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer proxyServer.Close()
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("probe bypassed configured HTTP proxy")
	}))
	defer targetServer.Close()
	originalURL := outboundProbeURL
	outboundProbeURL = targetServer.URL
	defer func() { outboundProbeURL = originalURL }()

	address := strings.TrimPrefix(proxyServer.URL, "http://")
	host, port, ok := strings.Cut(address, ":")
	if !ok {
		t.Fatalf("unexpected proxy address %q", address)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{"type": "http", "server": host, "server_port": portNumber})
	if err != nil {
		t.Fatal(err)
	}
	result := testOutbound(t.Context(), Task{Payload: string(payload)})
	if result.Status != "succeeded" || !strings.Contains(result.Summary, "HTTP 204") {
		t.Fatalf("unexpected proxy test result: %#v", result)
	}
}

func TestVerifyReleaseTaskRejectsTampering(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, releaseSigningPublicKeyFile), pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := `{"version":"1.13.12","architecture":"amd64","url":"https://releases.example.invalid/sing-box","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`
	payload, err := json.Marshal(map[string]string{
		"manifest":  manifest,
		"signature": base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, []byte(manifest))),
	})
	if err != nil {
		t.Fatal(err)
	}
	task := Task{Payload: string(payload), ExpectedHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	if _, err := VerifyReleaseTask(dataDir, task); err != nil {
		t.Fatalf("verify signed release task: %v", err)
	}
	task.ExpectedHash = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, err := VerifyReleaseTask(dataDir, task); err == nil {
		t.Fatal("accepted task with a different expected hash")
	}
	var altered map[string]string
	if err := json.Unmarshal(payload, &altered); err != nil {
		t.Fatal(err)
	}
	altered["manifest"] = `{"version":"1.13.12","architecture":"amd64","url":"https://attacker.invalid/sing-box","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`
	payload, err = json.Marshal(altered)
	if err != nil {
		t.Fatal(err)
	}
	task = Task{Payload: string(payload), ExpectedHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	if _, err := VerifyReleaseTask(dataDir, task); err == nil {
		t.Fatal("accepted task with a modified manifest")
	}
}

func TestExtractSingBoxArchive(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "sing-box.tar.gz")
	archiveFile, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(archiveFile)
	tarWriter := tar.NewWriter(gzipWriter)
	binary := "#!/bin/sh\necho sing-box\n"
	if err := tarWriter.WriteHeader(&tar.Header{Name: "sing-box-1.13.15-linux-arm64/sing-box", Mode: 0o755, Size: int64(len(binary))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write([]byte(binary)); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := archiveFile.Close(); err != nil {
		t.Fatal(err)
	}

	extracted, err := extractSingBoxArchive(archivePath, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(extracted)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(content)) != strings.TrimSpace(binary) {
		t.Fatalf("unexpected extracted binary: %q", content)
	}
}

func TestReleasingDropsDirectPortsWhenAlone(t *testing.T) {
	// When only one TCP inbound is configured on port 443, placement assigns it directly.
	// Nginx releasing routes must not include 443 so Nginx lets go of the port.
	config := `{"inbounds":[{"tag":"reality","type":"vless","listen":"::","listen_port":443}]}`
	sni := map[string]string{"reality": "images.unsplash.com"}
	plan, err := planListenerPlacement(config, sni, map[uint16]bool{}, func() (uint16, bool) { return 20000, true })
	if err != nil {
		t.Fatalf("planListenerPlacement failed: %v", err)
	}
	if !plan.directPorts[443] {
		t.Fatalf("expected port 443 in directPorts, got: %#v", plan.directPorts)
	}
	if len(plan.routes) != 0 {
		t.Fatalf("expected empty routes, got: %#v", plan.routes)
	}
}

func TestSingBoxDropInContentsWithMemLimit(t *testing.T) {
	// 1GB host: 60% of 1073741824 = 644245094 bytes -> 614MiB
	content1G := singBoxDropInContents(1024 * 1024 * 1024)
	if !strings.Contains(content1G, `Environment="GOMEMLIMIT=614MiB"`) {
		t.Errorf("expected 1GB host to have ~614MiB limit, got: %s", content1G)
	}
	if !strings.Contains(content1G, `Environment="GODEBUG=madvdontneed=1"`) {
		t.Errorf("expected GODEBUG=madvdontneed=1 in drop-in, got: %s", content1G)
	}

	// 0 / unknown host: fallback to 512MiB
	content0 := singBoxDropInContents(0)
	if !strings.Contains(content0, `Environment="GOMEMLIMIT=512MiB"`) {
		t.Errorf("expected 0 host to have 512MiB fallback limit, got: %s", content0)
	}

	// 4GB host: 75% of 4GB = 3072MiB
	content4G := singBoxDropInContents(4 * 1024 * 1024 * 1024)
	if !strings.Contains(content4G, `Environment="GOMEMLIMIT=3072MiB"`) {
		t.Errorf("expected 4GB host to have 3072MiB limit, got: %s", content4G)
	}
	if !strings.Contains(content4G, "MemoryMax=3379M") {
		t.Errorf("expected 4GB host MemoryMax 10%% above GOMEMLIMIT, got: %s", content4G)
	}
	if !strings.Contains(content1G, "MemoryMax=675M") || !strings.Contains(content1G, "MemorySwapMax=0") || !strings.Contains(content1G, "Restart=always") {
		t.Errorf("expected hard memory cap with restart on 1GB host, got: %s", content1G)
	}

	// 422MB host (the TW-HINET2 case): GOMEMLIMIT floors at 256MiB, the hard
	// cap sits just above it and stays under 90% of physical RAM.
	contentSmall := singBoxDropInContents(433064 * 1024)
	if !strings.Contains(contentSmall, `Environment="GOMEMLIMIT=256MiB"`) || !strings.Contains(contentSmall, "MemoryMax=281M") {
		t.Errorf("unexpected small-host drop-in: %s", contentSmall)
	}

	// Tiny host where 90% of RAM is below the GOMEMLIMIT floor: never cap
	// below the GC target, or the process would be killed before GC kicks in.
	contentTiny := singBoxDropInContents(256 * 1024 * 1024)
	if !strings.Contains(contentTiny, "MemoryMax=256M") {
		t.Errorf("expected tiny host MemoryMax clamped to GOMEMLIMIT, got: %s", contentTiny)
	}
}

func TestSingBoxConfigsSemanticallyMatch(t *testing.T) {
	baseConfig := `{
		"inbounds": [{"type": "vless", "listen_port": 443}],
		"outbounds": [{"type": "direct", "tag": "direct"}],
		"dns": {
			"servers": [{"tag": "dns-remote", "address": "tls://1.1.1.1"}]
		}
	}`

	// With DDNS predefined rules injected
	withDDNSRules := `{
		"inbounds": [{"type": "vless", "listen_port": 443}],
		"outbounds": [{"type": "direct", "tag": "direct"}],
		"dns": {
			"servers": [{"tag": "dns-remote", "address": "tls://1.1.1.1"}],
			"rules": [
				{
					"action": "predefined",
					"domain": ["twla.example.com"],
					"answer": ["twla.example.com. 60 IN A 198.51.100.1"]
				}
			]
		}
	}`

	// Different inbounds config
	differentConfig := `{
		"inbounds": [{"type": "vless", "listen_port": 8443}],
		"outbounds": [{"type": "direct", "tag": "direct"}]
	}`

	if !singBoxConfigsSemanticallyMatch([]byte(withDDNSRules), baseConfig) {
		t.Fatalf("expected withDDNSRules to semantically match baseConfig")
	}

	// Config with NO dns block at all vs config with only dynamic predefined DNS rules
	noDNSBaseConfig := `{
		"inbounds": [{"type": "vless", "listen_port": 443}],
		"outbounds": [{"type": "direct", "tag": "direct"}]
	}`
	onlyPredefinedDNS := `{
		"inbounds": [{"type": "vless", "listen_port": 443}],
		"outbounds": [{"type": "direct", "tag": "direct"}],
		"dns": {
			"rules": [
				{
					"action": "predefined",
					"domain": ["twla.example.com"],
					"answer": ["twla.example.com. 60 IN A 198.51.100.1"]
				}
			]
		}
	}`
	if !singBoxConfigsSemanticallyMatch([]byte(onlyPredefinedDNS), noDNSBaseConfig) {
		t.Fatalf("expected onlyPredefinedDNS to semantically match noDNSBaseConfig")
	}

	if singBoxConfigsSemanticallyMatch([]byte(differentConfig), baseConfig) {
		t.Fatalf("expected differentConfig to NOT match baseConfig")
	}
}

func TestPreserveDynamicPredefinedDNSRules(t *testing.T) {
	currentConfig := `{
		"inbounds": [{"type": "vless", "listen_port": 443}],
		"dns": {
			"rules": [
				{
					"action": "predefined",
					"domain": ["nat.example.com"],
					"answer": ["nat.example.com. 60 IN A 198.51.100.10"]
				}
			]
		}
	}`

	newDesired := `{
		"inbounds": [{"type": "vless", "listen_port": 8443}],
		"outbounds": [{"type": "direct", "tag": "direct"}]
	}`

	preserved := preserveDynamicPredefinedDNSRules([]byte(currentConfig), newDesired)
	if !strings.Contains(preserved, "nat.example.com") || !strings.Contains(preserved, "198.51.100.10") {
		t.Fatalf("expected preserved config to retain predefined rules: %s", preserved)
	}
	if !strings.Contains(preserved, "8443") {
		t.Fatalf("expected preserved config to have new inbound port: %s", preserved)
	}
}

func TestConfigurePingProbesIdempotency(t *testing.T) {
	tempDir := t.TempDir()
	task := Task{
		ID:   "task-probe-1",
		Kind: "probe.configure",
		Payload: `[
			{"id": "p1", "name": "Target 1", "target": "1.1.1.1"},
			{"id": "p2", "name": "Target 2", "target": "8.8.8.8"}
		]`,
	}

	// First execution applies targets
	res1 := configurePingProbes(task, tempDir)
	if res1.Status != "succeeded" || res1.Summary != "延迟监控已下发" {
		t.Fatalf("unexpected res1: %+v", res1)
	}

	// Second execution with identical targets MUST be pure no-op
	res2 := configurePingProbes(task, tempDir)
	if res2.Status != "succeeded" || res2.Summary != "延迟监控目标已是最新，无需修改" {
		t.Fatalf("unexpected res2: %+v", res2)
	}
}

