package control

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenClashMirrorCoreVersionFeedAndTarball(t *testing.T) {
	tarballPayload := "FAKE-CLASH-TAR-GZ-BYTES"

	var upstream *httptest.Server
	upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/MahiSail/mihomo/releases":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"tag_name":     "v1.19.34-ttl.2",
					"draft":        false,
					"published_at": "2026-11-21T15:00:00Z",
					"assets": []map[string]any{
						{
							"name":                 "clash-linux-amd64.tar.gz",
							"browser_download_url": upstream.URL + "/download/v1.19.34-ttl.2/clash-linux-amd64.tar.gz",
						},
						{
							"name":                 "clash-linux-arm64.tar.gz",
							"browser_download_url": upstream.URL + "/download/v1.19.34-ttl.2/clash-linux-arm64.tar.gz",
						},
					},
				},
				{
					"tag_name":     "v1.19.34-ttl.1",
					"draft":        false,
					"published_at": "2026-11-21T13:30:00Z",
					"assets": []map[string]any{
						{
							"name":                 "mihomo-linux-amd64-v1.19.34-ttl.1.gz",
							"browser_download_url": upstream.URL + "/download/legacy.gz",
						},
					},
				},
			})
		case "/download/v1.19.34-ttl.2/clash-linux-amd64.tar.gz":
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write([]byte(tarballPayload))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	mirror := newOpenClashMirror()
	mirror.apiBaseURL = upstream.URL
	mirror.ghBaseURL = upstream.URL
	mirror.httpClient = upstream.Client()

	// 1. Atom commit feed used by OpenClash Web UI online version check.
	req := httptest.NewRequest(http.MethodGet, "/openclash-mirror/https://github.com/vernesong/OpenClash/commits/core/master/core_version.atom", nil)
	rec := httptest.NewRecorder()
	mirror.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("atom feed status = %d, body = %s", rec.Code, rec.Body.String())
	}
	wantSHA := syntheticOpenClashSHA("v1.19.34-ttl.2")
	if !strings.Contains(rec.Body.String(), "Grit::Commit/"+wantSHA) {
		t.Fatalf("atom feed missing expected commit SHA %s: %s", wantSHA, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "v1.19.34-ttl.1") {
		t.Fatalf("atom feed should skip release without clash-linux-*.tar.gz assets")
	}

	// 2. Latest core_version plain text file (also test collapsed single-slash https:/).
	req = httptest.NewRequest(http.MethodGet, "/openclash-mirror/https:/raw.githubusercontent.com/vernesong/OpenClash/core/master/core_version", nil)
	rec = httptest.NewRecorder()
	mirror.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "v1.19.34-ttl.2" {
		t.Fatalf("latest core_version = (%d, %q), want (200, v1.19.34-ttl.2)", rec.Code, rec.Body.String())
	}

	// 3. Per-SHA core_version lookup.
	req = httptest.NewRequest(http.MethodGet, "/openclash-mirror/https://raw.githubusercontent.com/vernesong/OpenClash/"+wantSHA+"/master/core_version", nil)
	rec = httptest.NewRecorder()
	mirror.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "v1.19.34-ttl.2" {
		t.Fatalf("sha core_version = (%d, %q), want (200, v1.19.34-ttl.2)", rec.Code, rec.Body.String())
	}

	// 4. Meta core tarball download (supports linux-amd64, linux-amd64-v3, etc.).
	req = httptest.NewRequest(http.MethodGet, "/openclash-mirror/https://raw.githubusercontent.com/vernesong/OpenClash/core/master/meta/clash-linux-amd64-v3.tar.gz", nil)
	rec = httptest.NewRecorder()
	mirror.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != tarballPayload {
		t.Fatalf("tarball download = (%d, %q), want (200, %q)", rec.Code, rec.Body.String(), tarballPayload)
	}

	// 5. Security: reject SSRF to non-GitHub hosts, http://, and POST.
	for _, bad := range []string{
		"/openclash-mirror/http://127.0.0.1:8080/secret",
		"/openclash-mirror/https://169.254.169.254/latest/meta-data/",
		"/openclash-mirror/https://evil.example.com/vernesong/OpenClash/core/master/core_version",
		"/openclash-mirror/https://user:pass@github.com/vernesong/OpenClash",
	} {
		req = httptest.NewRequest(http.MethodGet, bad, nil)
		rec = httptest.NewRecorder()
		mirror.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("SSRF guard failed for %s: got %d, want 400", bad, rec.Code)
		}
	}
	req = httptest.NewRequest(http.MethodPost, "/openclash-mirror/https://github.com/vernesong/OpenClash/commits/core/master/core_version.atom", nil)
	rec = httptest.NewRecorder()
	mirror.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want 405", rec.Code)
	}
}
