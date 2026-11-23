package control

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	openClashMirrorPrefix       = "/openclash-mirror/"
	defaultOpenClashMihomoRepo  = "MahiSail/mihomo"
	openClashReleasesCacheTTL   = 2 * time.Minute
	openClashMaxMetaResponseLen = 2 << 20   // 2 MiB for text/atom/version feeds
	openClashMaxBinaryLen       = 128 << 20 // 128 MiB for core tarballs & geo assets
)

var (
	openClashCoreVersionRe  = regexp.MustCompile(`^/vernesong/OpenClash/(core/[A-Za-z0-9._-]+|([0-9a-f]{40})/[A-Za-z0-9._-]+)/core_version$`)
	openClashCoreAtomFeedRe = regexp.MustCompile(`^/vernesong/OpenClash/commits/core/[A-Za-z0-9._-]+/core_version\.atom$`)
	openClashCoreTarballRe  = regexp.MustCompile(`^/vernesong/OpenClash/(?:core|([0-9a-f]{40}))/[A-Za-z0-9._-]+/meta/clash-([A-Za-z0-9._-]+)\.tar\.gz$`)

	openClashAllowedHosts = map[string]struct{}{
		"github.com":                         {},
		"raw.githubusercontent.com":          {},
		"objects.githubusercontent.com":      {},
		"release-assets.githubusercontent.com": {},
		"codeload.github.com":                {},
	}
)

type openClashMihomoRelease struct {
	TagName     string
	PublishedAt string
	CommitSHA   string // synthetic 40-char lowercase hex ID for OpenClash Atom feed
	Assets      map[string]string
}

type openClashMirror struct {
	repo       string
	apiBaseURL string
	ghBaseURL  string
	httpClient *http.Client

	mu        sync.Mutex
	cachedAt  time.Time
	releases  []openClashMihomoRelease
	shaToRel  map[string]openClashMihomoRelease
}

func newOpenClashMirror() *openClashMirror {
	repo := strings.TrimSpace(os.Getenv("POLARIS_OPENCLASH_MIHOMO_REPO"))
	if repo == "" {
		repo = defaultOpenClashMihomoRepo
	}
	client := &http.Client{
		Timeout: 5 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 8 {
				return errors.New("too many redirects")
			}
			if !isAllowedOpenClashUpstreamURL(req.URL) {
				return fmt.Errorf("redirect to disallowed host %q", req.URL.Host)
			}
			return nil
		},
	}
	return &openClashMirror{
		repo:       repo,
		apiBaseURL: "https://api.github.com",
		ghBaseURL:  "https://github.com",
		httpClient: client,
		shaToRel:   make(map[string]openClashMihomoRelease),
	}
}

func isAllowedOpenClashUpstreamURL(u *url.URL) bool {
	if u == nil || u.Scheme != "https" || u.User != nil {
		return false
	}
	if u.Port() != "" && u.Port() != "443" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	_, ok := openClashAllowedHosts[host]
	return ok
}

func syntheticOpenClashSHA(tag string) string {
	sum := sha256.Sum256([]byte("polaris-openclash-mihomo:" + tag))
	return hex.EncodeToString(sum[:20]) // 40 lowercase hex chars
}

func mapOpenClashCPUModelToArch(cpuModel string) string {
	switch strings.ToLower(strings.TrimSpace(cpuModel)) {
	case "linux-amd64", "linux-amd64-v1", "linux-amd64-v2", "linux-amd64-v3", "linux-amd64-compatible":
		return "amd64"
	case "linux-arm64", "linux-armv8":
		return "arm64"
	default:
		return ""
	}
}

func (m *openClashMirror) fetchReleases(ctx context.Context) ([]openClashMihomoRelease, map[string]openClashMihomoRelease, error) {
	m.mu.Lock()
	if time.Since(m.cachedAt) < openClashReleasesCacheTTL && len(m.releases) > 0 {
		rels := m.releases
		bySHA := m.shaToRel
		m.mu.Unlock()
		return rels, bySHA, nil
	}
	m.mu.Unlock()

	endpoint := fmt.Sprintf("%s/repos/%s/releases?per_page=10", strings.TrimRight(m.apiBaseURL, "/"), m.repo)
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "polaris-openclash-mirror")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("github releases API returned status %d", resp.StatusCode)
	}

	var raw []struct {
		TagName     string `json:"tag_name"`
		Draft       bool   `json:"draft"`
		PublishedAt string `json:"published_at"`
		CreatedAt   string `json:"created_at"`
		Assets      []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, openClashMaxMetaResponseLen)).Decode(&raw); err != nil {
		return nil, nil, err
	}

	rels := make([]openClashMihomoRelease, 0, len(raw))
	bySHA := make(map[string]openClashMihomoRelease, len(raw))
	for _, item := range raw {
		if item.Draft || strings.TrimSpace(item.TagName) == "" {
			continue
		}
		assets := make(map[string]string, len(item.Assets))
		for _, a := range item.Assets {
			assets[a.Name] = a.BrowserDownloadURL
		}
		// Only surface releases that contain OpenClash tarballs so OpenClash
		// never attempts to download an incompatible legacy upstream tag.
		if assets["clash-linux-amd64.tar.gz"] == "" && assets["clash-linux-arm64.tar.gz"] == "" {
			continue
		}
		ts := item.PublishedAt
		if ts == "" {
			ts = item.CreatedAt
		}
		if ts == "" {
			ts = time.Now().UTC().Format(time.RFC3339)
		}
		sha := syntheticOpenClashSHA(item.TagName)
		rel := openClashMihomoRelease{
			TagName:     item.TagName,
			PublishedAt: ts,
			CommitSHA:   sha,
			Assets:      assets,
		}
		rels = append(rels, rel)
		bySHA[sha] = rel
		if len(rels) >= 5 {
			break
		}
	}
	if len(rels) == 0 {
		return nil, nil, errors.New("no OpenClash-compatible mihomo release found")
	}

	m.mu.Lock()
	m.cachedAt = time.Now()
	m.releases = rels
	m.shaToRel = bySHA
	m.mu.Unlock()
	return rels, bySHA, nil
}

func extractOpenClashTargetURL(rawPath, rawQuery string) (*url.URL, error) {
	if !strings.HasPrefix(rawPath, openClashMirrorPrefix) {
		return nil, errors.New("invalid mirror prefix")
	}
	rest := strings.TrimPrefix(rawPath, openClashMirrorPrefix)
	rest = strings.TrimLeft(rest, "/")
	// Normalize single-slash https:/ (in case an upstream proxy collapsed //).
	switch {
	case strings.HasPrefix(rest, "https://"):
	case strings.HasPrefix(rest, "https:/"):
		rest = "https://" + strings.TrimPrefix(rest, "https:/")
	default:
		return nil, errors.New("target URL must use https://")
	}
	if rawQuery != "" {
		rest += "?" + rawQuery
	}
	u, err := url.Parse(rest)
	if err != nil {
		return nil, err
	}
	if !isAllowedOpenClashUpstreamURL(u) {
		return nil, fmt.Errorf("upstream host %q is not allowed", u.Host)
	}
	return u, nil
}

func (m *openClashMirror) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	target, err := extractOpenClashTargetURL(r.URL.Path, r.URL.RawQuery)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	host := strings.ToLower(target.Hostname())

	// 1. Intercept OpenClash core_version Atom commit feed -> synthesize entries from custom mihomo releases.
	if host == "github.com" && openClashCoreAtomFeedRe.MatchString(target.Path) {
		m.serveCoreAtomFeed(w, r)
		return
	}

	// 2. Intercept OpenClash core_version plain text file -> return custom mihomo release tag.
	if host == "raw.githubusercontent.com" {
		if matches := openClashCoreVersionRe.FindStringSubmatch(target.Path); matches != nil {
			m.serveCoreVersion(w, r, matches[2])
			return
		}
		// 3. Intercept OpenClash Meta core tarball -> stream custom mihomo clash-linux-{arch}.tar.gz.
		if matches := openClashCoreTarballRe.FindStringSubmatch(target.Path); matches != nil {
			m.serveCoreTarball(w, r, matches[1], matches[2])
			return
		}
	}

	// 4. Everything else on the strict GitHub allowlist (OpenClash plugin version, GeoIP/GeoSite, etc.)
	// is proxied transparently so OpenClash's other update checks keep working normally.
	m.proxyUpstream(w, r, target.String())
}

func (m *openClashMirror) serveCoreAtomFeed(w http.ResponseWriter, r *http.Request) {
	rels, _, err := m.fetchReleases(r.Context())
	if err != nil {
		http.Error(w, "failed to load mihomo releases: "+err.Error(), http.StatusBadGateway)
		return
	}

	type atomEntry struct {
		ID      string `xml:"id"`
		Updated string `xml:"updated"`
		Title   string `xml:"title"`
	}
	type atomFeed struct {
		XMLName xml.Name    `xml:"http://www.w3.org/2005/Atom feed"`
		Entries []atomEntry `xml:"entry"`
	}
	feed := atomFeed{Entries: make([]atomEntry, 0, len(rels))}
	for _, rel := range rels {
		feed.Entries = append(feed.Entries, atomEntry{
			ID:      "tag:github.com,2008:Grit::Commit/" + rel.CommitSHA,
			Updated: rel.PublishedAt,
			Title:   "Meta Core " + rel.TagName,
		})
	}
	body, err := xml.MarshalIndent(feed, "", "  ")
	if err != nil {
		http.Error(w, "marshal atom feed failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte(xml.Header))
		_, _ = w.Write(body)
	}
}

func (m *openClashMirror) serveCoreVersion(w http.ResponseWriter, r *http.Request, sha string) {
	rels, bySHA, err := m.fetchReleases(r.Context())
	if err != nil {
		http.Error(w, "failed to load mihomo releases: "+err.Error(), http.StatusBadGateway)
		return
	}
	rel := rels[0]
	if sha != "" {
		matched, ok := bySHA[sha]
		if !ok {
			http.NotFound(w, r)
			return
		}
		rel = matched
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = io.WriteString(w, rel.TagName+"\n")
	}
}

func (m *openClashMirror) serveCoreTarball(w http.ResponseWriter, r *http.Request, sha, cpuModel string) {
	arch := mapOpenClashCPUModelToArch(cpuModel)
	if arch == "" {
		http.Error(w, "unsupported cpu model: "+cpuModel, http.StatusNotFound)
		return
	}
	rels, bySHA, err := m.fetchReleases(r.Context())
	if err != nil {
		http.Error(w, "failed to load mihomo releases: "+err.Error(), http.StatusBadGateway)
		return
	}
	rel := rels[0]
	if sha != "" {
		matched, ok := bySHA[sha]
		if !ok {
			http.NotFound(w, r)
			return
		}
		rel = matched
	}
	assetName := "clash-linux-" + arch + ".tar.gz"
	downloadURL := rel.Assets[assetName]
	if downloadURL == "" {
		downloadURL = fmt.Sprintf("%s/%s/releases/download/%s/%s", strings.TrimRight(m.ghBaseURL, "/"), m.repo, url.PathEscape(rel.TagName), assetName)
	}
	m.proxyUpstream(w, r, downloadURL)
}

func (m *openClashMirror) proxyUpstream(w http.ResponseWriter, r *http.Request, targetURL string) {
	req, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, nil)
	if err != nil {
		http.Error(w, "invalid upstream request", http.StatusBadRequest)
		return
	}
	req.Header.Set("User-Agent", "polaris-openclash-mirror")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		http.Error(w, "upstream fetch failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		w.Header().Set("Content-Length", cl)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(resp.StatusCode)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, io.LimitReader(resp.Body, openClashMaxBinaryLen))
	}
}
