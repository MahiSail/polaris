package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type DNSMappingItem struct {
	IP      string   `json:"ip"`
	Domains []string `json:"domains"`
}

type DNSUpdatePayload struct {
	IP       string           `json:"ip,omitempty"`
	Domain   string           `json:"domain,omitempty"`
	Domains  []string         `json:"domains,omitempty"`
	Mappings []DNSMappingItem `json:"mappings,omitempty"`
}

// flushSystemDNSCache attempts to flush system resolver caches on a best-effort basis.
// Failure (e.g. non-root user lacking permission) is ignored so non-root executions never fail.
func flushSystemDNSCache(ctx context.Context) {
	if runtime.GOOS == "windows" {
		_ = exec.CommandContext(ctx, "ipconfig", "/flushdns").Run()
		return
	}
	_ = exec.CommandContext(ctx, "resolvectl", "flush-caches").Run()
	_ = exec.CommandContext(ctx, "systemd-resolve", "--flush-caches").Run()
	_ = exec.CommandContext(ctx, "systemctl", "restart", "nscd").Run()
}

var (
	protectedMasterHostMu sync.RWMutex
	protectedMasterHost   string
)

// extractDomainHost extracts a lowercase hostname from host or host:port,
// returning empty string if the input is empty or already an IP literal.
func extractDomainHost(addr string) string {
	s := strings.TrimSpace(addr)
	if s == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(s); err == nil {
		s = h
	}
	s = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), ".")
	if s == "" || net.ParseIP(s) != nil {
		return ""
	}
	return s
}

// SetProtectedMasterHost registers the master's control-plane hostname so the agent
// will never pin it into /etc/hosts (preventing permanent reconnect deadlocks if
// the master's IP rotates).
func SetProtectedMasterHost(masterAddr string) {
	host := extractDomainHost(masterAddr)
	protectedMasterHostMu.Lock()
	protectedMasterHost = host
	protectedMasterHostMu.Unlock()
}

func getProtectedMasterHost() string {
	protectedMasterHostMu.RLock()
	h := protectedMasterHost
	protectedMasterHostMu.RUnlock()
	return h
}

// PurgeHostFromHostsFile removes any /etc/hosts line referencing the given host
// (or host:port) so control-plane reconnects always resolve via live DNS.
// Returns true if /etc/hosts was modified.
func PurgeHostFromHostsFile(masterAddr string) bool {
	if runtime.GOOS == "windows" {
		return false
	}
	return purgeHostFromFile("/etc/hosts", masterAddr)
}

func purgeHostFromFile(hostsPath, masterAddr string) bool {
	host := extractDomainHost(masterAddr)
	if host == "" {
		return false
	}
	content, err := os.ReadFile(hostsPath)
	if err != nil {
		return false
	}
	lines := strings.Split(string(content), "\n")
	var kept []string
	removed := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			fields := strings.Fields(line)
			if len(fields) > 1 {
				matched := false
				for _, f := range fields[1:] {
					if strings.ToLower(strings.TrimSuffix(f, ".")) == host {
						matched = true
						break
					}
				}
				if matched {
					removed = true
					continue
				}
			}
		}
		kept = append(kept, line)
	}
	if !removed {
		return false
	}
	newContent := strings.Join(kept, "\n")
	if err := os.WriteFile(hostsPath, []byte(newContent), 0o644); err != nil {
		return false
	}
	return true
}

// syncHostsFile best-effort updates /etc/hosts with a managed polaris block.
// Returns changed=true if /etc/hosts was rewritten, alreadyUpToDate=true if already identical.
// Returns ok=true if /etc/hosts matches desired state or was written successfully.
func syncHostsFile(mappings []DNSMappingItem) (changed bool, alreadyUpToDate bool, ok bool) {
	if runtime.GOOS == "windows" {
		return false, false, false
	}
	if len(mappings) == 0 {
		return false, true, true
	}
	hostsPath := "/etc/hosts"
	content, err := os.ReadFile(hostsPath)
	if err != nil {
		return false, false, false
	}

	protectedHost := getProtectedMasterHost()
	managedDomainMap := make(map[string]bool)
	if protectedHost != "" {
		// Also strip any stale line for the master control-plane host outside the block.
		managedDomainMap[protectedHost] = true
	}
	for _, m := range mappings {
		for _, d := range m.Domains {
			d = strings.TrimSpace(strings.ToLower(strings.TrimSuffix(d, ".")))
			if d != "" {
				managedDomainMap[d] = true
			}
		}
	}

	lines := strings.Split(string(content), "\n")
	var newLines []string
	inPolarisBlock := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "# BEGIN POLARIS DDNS" {
			inPolarisBlock = true
			continue
		}
		if trimmed == "# END POLARIS DDNS" {
			inPolarisBlock = false
			continue
		}
		if inPolarisBlock {
			continue
		}
		hasDomain := false
		fields := strings.Fields(line)
		if len(fields) > 1 {
			for _, f := range fields[1:] {
				if managedDomainMap[strings.ToLower(strings.TrimSuffix(f, "."))] {
					hasDomain = true
					break
				}
			}
		}
		if !hasDomain {
			newLines = append(newLines, line)
		}
	}

	var managedBlock []string
	managedBlock = append(managedBlock, "# BEGIN POLARIS DDNS")
	for _, m := range mappings {
		ip := strings.TrimSpace(m.IP)
		for _, d := range m.Domains {
			d = strings.TrimSpace(d)
			norm := strings.ToLower(strings.TrimSuffix(d, "."))
			if d != "" && ip != "" && norm != protectedHost {
				managedBlock = append(managedBlock, fmt.Sprintf("%s %s", ip, d))
			}
		}
	}
	managedBlock = append(managedBlock, "# END POLARIS DDNS")

	newContent := strings.TrimRight(strings.Join(newLines, "\n"), "\n") + "\n" + strings.Join(managedBlock, "\n") + "\n"
	if newContent == string(content) {
		return false, true, true
	}
	if err := os.WriteFile(hostsPath, []byte(newContent), 0o644); err != nil {
		return false, false, false
	}
	return true, false, true
}

// updateSingBoxDNSOverride updates sing-box's configuration file (managed by agent)
// with predefined DNS rules mapping domain -> targetIP for all mappings.
// Returns changed=true if config was written, alreadyUpToDate=true if already up to date.
func updateSingBoxDNSOverride(mappings []DNSMappingItem, dataDir string) (bool, bool, error) {
	configPath := managedSingBoxConfig
	if _, err := os.Stat(configPath); err != nil {
		return false, false, nil
	}
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		return false, false, err
	}

	var configMap map[string]any
	if err := json.Unmarshal(configBytes, &configMap); err != nil {
		return false, false, err
	}

	dnsMap, ok := configMap["dns"].(map[string]any)
	if !ok || dnsMap == nil {
		dnsMap = make(map[string]any)
		configMap["dns"] = dnsMap
	}

	var rules []any
	if existingRules, ok := dnsMap["rules"].([]any); ok {
		rules = existingRules
	}

	type domainRule struct {
		rtype  string
		ip     string
		answer string
	}
	desiredRules := make(map[string]domainRule)
	for _, m := range mappings {
		ip := strings.TrimSpace(m.IP)
		if ip == "" {
			continue
		}
		rtype := "A"
		if parsed := net.ParseIP(ip); parsed != nil && parsed.To4() == nil {
			rtype = "AAAA"
		}
		for _, d := range m.Domains {
			d = strings.TrimSpace(strings.ToLower(strings.TrimSuffix(d, ".")))
			if d != "" {
				desiredRules[d] = domainRule{
					rtype:  rtype,
					ip:     ip,
					answer: fmt.Sprintf("%s. 60 IN %s %s", d, rtype, ip),
				}
			}
		}
	}

	if len(desiredRules) == 0 {
		return false, false, nil
	}

	// Check if all desired domains already have exact predefined rules matching desired answer
	allUpToDate := true
	for d, want := range desiredRules {
		found := false
		for _, r := range rules {
			rMap, ok := r.(map[string]any)
			if !ok || rMap["action"] != "predefined" {
				continue
			}
			if dList, ok := rMap["domain"].([]any); ok {
				for _, dItem := range dList {
					if dStr, ok := dItem.(string); ok && strings.ToLower(strings.TrimSuffix(dStr, ".")) == d {
						if aList, ok := rMap["answer"].([]any); ok && len(aList) == 1 {
							if aStr, ok := aList[0].(string); ok && aStr == want.answer {
								found = true
							}
						}
						break
					}
				}
			}
			if found {
				break
			}
		}
		if !found {
			allUpToDate = false
			break
		}
	}

	if allUpToDate {
		return false, true, nil
	}

	// Filter out existing predefined rules for all desired domains
	var filteredRules []any
	for _, r := range rules {
		rMap, ok := r.(map[string]any)
		if !ok {
			filteredRules = append(filteredRules, r)
			continue
		}
		if rMap["action"] == "predefined" {
			match := false
			if dList, ok := rMap["domain"].([]any); ok {
				for _, dItem := range dList {
					if dStr, ok := dItem.(string); ok && desiredRules[strings.ToLower(strings.TrimSuffix(dStr, "."))].answer != "" {
						match = true
						break
					}
				}
			}
			if match {
				continue
			}
		}
		filteredRules = append(filteredRules, r)
	}

	// Append updated predefined rules (sort domains for deterministic output)
	sortedDomains := make([]string, 0, len(desiredRules))
	for d := range desiredRules {
		sortedDomains = append(sortedDomains, d)
	}
	sort.Strings(sortedDomains)

	for _, d := range sortedDomains {
		rule := desiredRules[d]
		filteredRules = append(filteredRules, map[string]any{
			"domain": []any{d},
			"action": "predefined",
			"answer": []any{rule.answer},
		})
	}
	dnsMap["rules"] = filteredRules

	encoded, err := json.MarshalIndent(configMap, "", "  ")
	if err != nil {
		return false, false, err
	}

	configDir := filepath.Dir(configPath)
	tempFile, err := os.CreateTemp(configDir, ".singbox-ddns-*.json")
	if err != nil {
		return false, false, err
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	if _, err := tempFile.Write(encoded); err != nil {
		tempFile.Close()
		return false, false, err
	}
	if err := tempFile.Chmod(0o640); err != nil {
		tempFile.Close()
		return false, false, err
	}
	if err := tempFile.Close(); err != nil {
		return false, false, err
	}

	if err := os.Rename(tempPath, configPath); err != nil {
		return false, false, err
	}

	if dataDir != "" {
		_ = updateEffectiveSingBoxConfigurationState(dataDir)
	}
	return true, false, nil
}

// closeStaleClashConnections selectively terminates only the active connections targeting
// the affected domain(s), stale destination IP, or through the affected chain outbound(s) via DELETE /connections/:id.
// This guarantees that all other unrelated user connections, inbounds, and direct routes
// on sing-box remain completely unaffected and uninterrupted.
func closeStaleClashConnections(ctx context.Context, domains []string, targetIP string) error {
	if len(domains) == 0 {
		return nil
	}
	domainMap := make(map[string]bool, len(domains))
	for _, d := range domains {
		d = strings.TrimSpace(strings.ToLower(strings.TrimSuffix(d, ".")))
		if d != "" {
			domainMap[d] = true
		}
	}

	getReq, err := http.NewRequestWithContext(ctx, http.MethodGet, clashAPIBase+"/connections", nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(getReq)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var payload struct {
		Connections []struct {
			ID       string   `json:"id"`
			Chains   []string `json:"chains"`
			Metadata struct {
				Host          string `json:"host"`
				DestinationIP string `json:"destinationIP"`
			} `json:"metadata"`
		} `json:"connections"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8*1024*1024)).Decode(&payload); err != nil {
		return nil
	}

	// Read config to find outbound tags matching any of the affected domains
	affectedOutboundTags := make(map[string]bool)
	if configBytes, err := os.ReadFile(managedSingBoxConfig); err == nil {
		var cfg struct {
			Outbounds []struct {
				Tag    string `json:"tag"`
				Server string `json:"server"`
				TLS    struct {
					ServerName string `json:"server_name"`
				} `json:"tls"`
			} `json:"outbounds"`
		}
		if json.Unmarshal(configBytes, &cfg) == nil {
			for _, ob := range cfg.Outbounds {
				srv := strings.TrimSpace(strings.ToLower(strings.TrimSuffix(ob.Server, ".")))
				sni := strings.TrimSpace(strings.ToLower(strings.TrimSuffix(ob.TLS.ServerName, ".")))
				if domainMap[srv] || domainMap[sni] {
					if ob.Tag != "" {
						affectedOutboundTags[ob.Tag] = true
					}
				}
			}
		}
	}

	client := &http.Client{Timeout: 2 * time.Second}
	for _, conn := range payload.Connections {
		match := false
		host := strings.TrimSpace(strings.ToLower(strings.TrimSuffix(conn.Metadata.Host, ".")))
		if host != "" && domainMap[host] {
			match = true
		}
		if !match && targetIP != "" && conn.Metadata.DestinationIP != "" && conn.Metadata.DestinationIP != targetIP {
			for d := range domainMap {
				if strings.Contains(host, d) {
					match = true
					break
				}
			}
		}
		if !match {
			for _, ch := range conn.Chains {
				if affectedOutboundTags[ch] {
					match = true
					break
				}
			}
		}
		if match && conn.ID != "" {
			delReq, _ := http.NewRequestWithContext(ctx, http.MethodDelete, clashAPIBase+"/connections/"+conn.ID, nil)
			if delResp, err := client.Do(delReq); err == nil {
				_ = delResp.Body.Close()
			}
		}
	}
	return nil
}

// reloadSingBoxState restarts sing-box to invalidate dialer caches and reset outbound states.
func reloadSingBoxState(ctx context.Context) error {
	// Sing-box does NOT support SIGHUP configuration reload or dialer cache flush.
	// To reliably terminate stale QUIC/TCP session pools and apply the new target IP,
	// sing-box must be restarted. Restarting sing-box takes ~30ms.
	if singBoxServiceActive(ctx) {
		res := restartSingBox(ctx)
		if res.Status == "succeeded" {
			return nil
		}
		return errors.New(res.Summary)
	}
	if commandExists("pkill") {
		_ = exec.CommandContext(ctx, "pkill", "-HUP", "-f", "sing-box").Run()
	}
	return nil
}

// verifyDomainResolution actively probes local DNS resolution to verify whether the domain
// resolves to expectedIP. It polls for up to timeout to allow upstream DNS propagation to arrive.
func verifyDomainResolution(ctx context.Context, domain, expectedIP string, timeout time.Duration) ([]string, bool) {
	expectedIP = strings.TrimSpace(expectedIP)
	deadline := time.Now().Add(timeout)
	for {
		lookupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		ips, err := net.DefaultResolver.LookupIP(lookupCtx, "ip", domain)
		cancel()
		if err == nil {
			var ipStrs []string
			for _, ip := range ips {
				ipStr := ip.String()
				ipStrs = append(ipStrs, ipStr)
				if ipStr == expectedIP {
					return ipStrs, true
				}
			}
			if len(ipStrs) > 0 && (expectedIP == "" || time.Now().After(deadline)) {
				return ipStrs, expectedIP == ""
			}
		}
		if time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return nil, false
		case <-time.After(500 * time.Millisecond):
		}
	}
	return nil, false
}

var (
	dnsRestartMu       sync.Mutex
	lastDNSRestartAt   time.Time
	dnsPendingTimer    *time.Timer
	dnsRestartCooldown = 5 * time.Second
)

func updateDNS(ctx context.Context, task Task, dataDirOpts ...string) TaskResult {
	var payload DNSUpdatePayload
	if err := json.Unmarshal([]byte(task.Payload), &payload); err != nil {
		return TaskResult{Status: "failed", Summary: "invalid dns.update payload: " + err.Error()}
	}

	var mappings []DNSMappingItem
	if len(payload.Mappings) > 0 {
		mappings = append(mappings, payload.Mappings...)
	}
	if payload.IP != "" {
		var singleDomains []string
		if payload.Domain != "" {
			singleDomains = append(singleDomains, payload.Domain)
		}
		singleDomains = append(singleDomains, payload.Domains...)
		singleDomains = deduplicateStrings(singleDomains)
		if len(singleDomains) > 0 {
			exists := false
			for _, m := range mappings {
				if m.IP == payload.IP {
					exists = true
					break
				}
			}
			if !exists {
				mappings = append(mappings, DNSMappingItem{
					IP:      payload.IP,
					Domains: singleDomains,
				})
			}
		}
	}

	var cleanMappings []DNSMappingItem
	var allDomains []string
	for _, m := range mappings {
		ip := strings.TrimSpace(m.IP)
		domains := deduplicateStrings(m.Domains)
		if ip != "" && len(domains) > 0 {
			cleanMappings = append(cleanMappings, DNSMappingItem{
				IP:      ip,
				Domains: domains,
			})
			allDomains = append(allDomains, domains...)
		}
	}
	if len(cleanMappings) == 0 {
		return TaskResult{Status: "failed", Summary: "missing target domains/mappings in dns.update payload"}
	}
	allDomains = deduplicateStrings(allDomains)

	var dataDir string
	if len(dataDirOpts) > 0 && dataDirOpts[0] != "" {
		dataDir = dataDirOpts[0]
	}

	// 1. Best-effort /etc/hosts update
	hostsChanged, hostsUpToDate, hostsOK := syncHostsFile(cleanMappings)

	// 2. Update sing-box configuration with predefined DNS rules
	singboxChanged, singboxUpToDate, singboxErr := updateSingBoxDNSOverride(cleanMappings, dataDir)
	if singboxErr != nil {
		return TaskResult{Status: "failed", Summary: "更新 sing-box 静态解析失败: " + singboxErr.Error()}
	}

	// ZERO-OP IDEMPOTENCY CHECK:
	// If at least one target was verified to already contain the desired records and neither needed modifications,
	// execute zero commands, write zero files, flush zero caches, restart zero services.
	if !hostsChanged && !singboxChanged && (hostsUpToDate || singboxUpToDate) {
		return TaskResult{
			Status:  "succeeded",
			Summary: "所有域名解析与静态规则均已是最新，无需修改",
		}
	}

	var notes []string
	if hostsChanged && hostsOK {
		notes = append(notes, "已同步 /etc/hosts")
	}

	// Flush system DNS cache only when actual updates occurred
	flushSystemDNSCache(ctx)

	if singboxChanged {
		notes = append(notes, "已写入 sing-box 静态解析规则")
	}

	// Selectively terminate stale connections targeting the affected NAT domains/IPs
	_ = closeStaleClashConnections(ctx, allDomains, "")

	// Restart sing-box to flush persistent pools only if configuration actually changed
	if singboxChanged && singBoxServiceActive(ctx) {
		dnsRestartMu.Lock()
		now := time.Now()
		shouldRestart := now.Sub(lastDNSRestartAt) >= dnsRestartCooldown
		if shouldRestart {
			lastDNSRestartAt = now
			if dnsPendingTimer != nil {
				dnsPendingTimer.Stop()
				dnsPendingTimer = nil
			}
		} else {
			if dnsPendingTimer != nil {
				dnsPendingTimer.Stop()
			}
			dnsPendingTimer = time.AfterFunc(dnsRestartCooldown, func() {
				dnsRestartMu.Lock()
				dnsPendingTimer = nil
				lastDNSRestartAt = time.Now()
				dnsRestartMu.Unlock()
				if singBoxServiceActive(context.Background()) {
					_ = restartSingBox(context.Background())
				}
			})
		}
		dnsRestartMu.Unlock()

		if shouldRestart {
			if res := restartSingBox(ctx); res.Status == "succeeded" {
				notes = append(notes, "已重启 sing-box 重置出站连接池")
			} else {
				notes = append(notes, "sing-box 重启提示: "+res.Summary)
			}
		} else {
			notes = append(notes, "静态解析已更新，处于冷却期内延迟合并重启")
		}
	} else if !singBoxServiceActive(ctx) && commandExists("pkill") {
		_ = exec.CommandContext(ctx, "pkill", "-HUP", "-f", "sing-box").Run()
	}

	var summary string
	if len(cleanMappings) == 1 {
		summary = fmt.Sprintf("%s → %s", strings.Join(cleanMappings[0].Domains, ", "), cleanMappings[0].IP)
	} else {
		summary = fmt.Sprintf("已合并更新 %d 个节点的域名解析与静态规则", len(cleanMappings))
	}
	if len(notes) > 0 {
		summary += "（" + strings.Join(notes, "，") + "）"
	}
	return TaskResult{
		Status:  "succeeded",
		Summary: summary,
	}
}

func deduplicateStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, v := range values {
		if v != "" && !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}

