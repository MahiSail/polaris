package control

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// NotificationLocation returns the location used for alert notification timestamps: Asia/Shanghai (CST, UTC+8).
// If the TZ environment variable is set, it respects TZ.
var NotificationLocation = func() *time.Location {
	if tz := os.Getenv("TZ"); tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}
	if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		return loc
	}
	return time.FixedZone("CST", 8*3600)
}()

// NowAlertTime returns the current time in the notification timezone.
func NowAlertTime() string {
	return FormatAlertTime(time.Now())
}

// FormatAlertTime formats a time in the notification timezone as "YYYY-MM-DD HH:MM:SS".
func FormatAlertTime(t time.Time) string {
	local := t.In(NotificationLocation)
	return local.Format("2006-01-02 15:04:05")
}

type AlertEngine struct {
	store                 *Store
	bark                  *BarkClient
	telegram              *TelegramClient
	locator               *ipLocator
	hub                   *connectionsHub
	mu                    sync.Mutex
	cooldowns             map[string]time.Time
	highRateAt            map[string]time.Time
	loginFailures         map[string]int
	loginFailAt           map[string]time.Time
	offlineNodes          map[string]time.Time
	latestConns           map[string][]storedConnection
	nodeNames             map[string]string
	pendingChainFirstSeen map[string]pendingChainEntry
	fail2banMu            sync.Mutex
	fail2banBatch         map[string]*fail2banBatch
	fail2banWait          time.Duration
	ddnsMu                sync.Mutex
	ddnsBatch             *ddnsAlertBatch
	ddnsWait              time.Duration
	proxyRouteMu          sync.Mutex
	proxyRouteBatch       *proxyRouteAlertBatch
	proxyRouteWait        time.Duration
	cached                AlertSettings
	cacheTime             time.Time
}

type pendingChainEntry struct {
	firstSeen time.Time
	lastSeen  time.Time
}

type fail2banBatch struct {
	key      string
	nodeID   string
	nodeName string
	jail     string
	ips      []string
	seen     map[string]struct{}
	started  time.Time
	timer    *time.Timer
}

type ddnsUpdateEntry struct {
	nodeID   string
	nodeName string
	oldIP    string
	newIP    string
	domains  []string
}

type ddnsHealingEntry struct {
	targetNodeName string
	chainedCount   int
	domains        []string
	targetIP       string
}

type ddnsCompletedEntry struct {
	nodeID    string
	nodeName  string
	summaries []string
}

type ddnsAlertBatch struct {
	started     time.Time
	fireAt      time.Time
	timer       *time.Timer
	updates     []*ddnsUpdateEntry
	healings    []*ddnsHealingEntry
	completions []*ddnsCompletedEntry
}

type proxyRouteNodeEntry struct {
	nodeID   string
	nodeName string
	cooldown int
	issues   map[string]*routeIssue
	seenConn map[string]struct{}
}

type proxyRouteAlertBatch struct {
	started time.Time
	fireAt  time.Time
	timer   *time.Timer
	nodes   map[string]*proxyRouteNodeEntry
}

const (
	defaultFail2banWindow   = 30 * time.Second
	maxFail2banBatchSize    = 50
	defaultDDNSBatchWindow  = 15 * time.Second
	maxDDNSBatchSize        = 50
	defaultProxyRouteWindow = 15 * time.Second
	maxProxyRouteBatchNodes = 50
)

func NewAlertEngine(store *Store, locator *ipLocator) *AlertEngine {
	if locator == nil {
		locator, _ = newIPLocator()
	}
	return &AlertEngine{
		store:                 store,
		bark:                  NewBarkClient(),
		telegram:              NewTelegramClient(),
		locator:               locator,
		cooldowns:             make(map[string]time.Time),
		highRateAt:            make(map[string]time.Time),
		loginFailures:         make(map[string]int),
		loginFailAt:           make(map[string]time.Time),
		offlineNodes:          make(map[string]time.Time),
		latestConns:           make(map[string][]storedConnection),
		nodeNames:             make(map[string]string),
		pendingChainFirstSeen: make(map[string]pendingChainEntry),
		fail2banBatch:         make(map[string]*fail2banBatch),
		fail2banWait:          defaultFail2banWindow,
		ddnsWait:              defaultDDNSBatchWindow,
		proxyRouteWait:        defaultProxyRouteWindow,
	}
}

func (e *AlertEngine) SetConnectionsHub(hub *connectionsHub) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.hub = hub
}

func (e *AlertEngine) snapshotConnsState() (hub *connectionsHub, fallback map[string][]storedConnection, names map[string]string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	hub = e.hub
	if len(e.latestConns) > 0 {
		fallback = make(map[string][]storedConnection, len(e.latestConns))
		for k, v := range e.latestConns {
			fallback[k] = v
		}
	}
	if len(e.nodeNames) > 0 {
		names = make(map[string]string, len(e.nodeNames))
		for k, v := range e.nodeNames {
			names[k] = v
		}
	}
	return hub, fallback, names
}

func (e *AlertEngine) resolveNodeName(nodeID, fallback string) string {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _, names := e.snapshotConnsState()
	return lookupNodeDisplayName(ctx, e.store, names, nodeID, fallback)
}

func (e *AlertEngine) resolveChain(nodeID, nodeName string, c storedConnection) resolvedChainRoute {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	hub, fallback, names := e.snapshotConnsState()
	return resolveChainRoute(ctx, e.store, hub, fallback, names, nodeID, nodeName, c)
}

func (e *AlertEngine) enrichRelayConns(connections []storedConnection) []storedConnection {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	hub, fallback, names := e.snapshotConnsState()
	return enrichRelayConnections(ctx, e.store, hub, fallback, names, connections)
}

func (e *AlertEngine) SetIPLocator(locator *ipLocator) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.locator = locator
}

func (e *AlertEngine) formatIP(ipStr string) string {
	clean := cleanIPAddress(ipStr)
	if clean == "" {
		return "-"
	}
	if e.locator != nil {
		l := strings.TrimSpace(e.locator.Locate(clean))
		if l != "" && l != "未知" && l != "未知归属地" {
			return fmt.Sprintf("%s（%s）", clean, l)
		}
	}
	return clean
}

func (e *AlertEngine) formatIPList(raw string) string {
	parts := splitMultiItems(raw)
	if len(parts) <= 1 {
		return e.formatIP(raw)
	}
	formatted := make([]string, 0, len(parts))
	for _, part := range parts {
		formatted = append(formatted, e.formatIP(part))
	}
	return strings.Join(formatted, "、")
}

func splitMultiItems(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '、' || r == ','
	})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func formatLimitedList(items []string, limit int, unit string) string {
	if len(items) == 0 {
		return ""
	}
	if limit <= 0 || len(items) <= limit {
		return strings.Join(items, "、")
	}
	return fmt.Sprintf("%s 等 %d %s", strings.Join(items[:limit], "、"), len(items), unit)
}

func sortIPStrings(ips []string) {
	sort.Slice(ips, func(i, j int) bool {
		ipA := net.ParseIP(ips[i])
		ipB := net.ParseIP(ips[j])
		if ipA != nil && ipB != nil {
			if cmp := bytes.Compare(ipA.To16(), ipB.To16()); cmp != 0 {
				return cmp < 0
			}
		}
		return ips[i] < ips[j]
	})
}

func extendBoundedTimer(timer *time.Timer, started, fireAt, now time.Time, wait time.Duration) time.Time {
	extend := wait / 3
	if extend < time.Millisecond {
		extend = wait
	}
	maxDeadline := started.Add(wait * 2)
	if fireAt.Sub(now) < extend && fireAt.Before(maxDeadline) {
		newFire := now.Add(extend)
		if newFire.After(maxDeadline) {
			newFire = maxDeadline
		}
		if newFire.After(fireAt) {
			if timer != nil {
				timer.Reset(newFire.Sub(now))
			}
			return newFire
		}
	}
	return fireAt
}

func cleanIPAddress(ipStr string) string {
	clean := strings.TrimSpace(ipStr)
	if h, _, err := net.SplitHostPort(clean); err == nil {
		clean = h
	}
	if clean == "-" {
		return ""
	}
	return strings.TrimSpace(clean)
}

// topTrafficEndpoints returns the source IP and destination with the largest
// share of the current sampling interval. It aggregates by source IP and by
// host first, rather than picking one connection, so a source that opens many
// small connections is ranked correctly. When none of the connections has a
// measured rate yet, it falls back to cumulative bytes.
func (e *AlertEngine) topTrafficEndpoints(connections []storedConnection) (sourceIP, destination string) {
	sourceRates := make(map[string]float64)
	destRates := make(map[string]float64)
	sourceBytes := make(map[string]int64)
	destBytes := make(map[string]int64)
	hasRates := false

	for _, connection := range connections {
		source := cleanIPAddress(connection.SourceIP)
		dest := strings.TrimSpace(connection.Host)
		if dest == "" {
			dest = strings.TrimSpace(connection.Destination)
		}
		if source != "" {
			sourceBytes[source] += connection.Upload + connection.Download
		}
		if dest != "" {
			destBytes[dest] += connection.Upload + connection.Download
		}
		if !connection.HasRates {
			continue
		}
		hasRates = true
		if source != "" {
			sourceRates[source] += connection.UploadRate + connection.DownloadRate
		}
		if dest != "" {
			destRates[dest] += connection.UploadRate + connection.DownloadRate
		}
	}

	if hasRates {
		return maxRateKey(sourceRates), maxRateKey(destRates)
	}
	return maxBytesKey(sourceBytes), maxBytesKey(destBytes)
}

func maxRateKey(values map[string]float64) string {
	bestKey := ""
	bestValue := 0.0
	for key, value := range values {
		if value > bestValue || bestKey == "" {
			bestKey, bestValue = key, value
		}
	}
	return bestKey
}

func maxBytesKey(values map[string]int64) string {
	bestKey := ""
	bestValue := int64(0)
	for key, value := range values {
		if value > bestValue || bestKey == "" {
			bestKey, bestValue = key, value
		}
	}
	return bestKey
}

type countedItem struct {
	key   string
	count int
}

func topCountedItems(counts map[string]int, limit int) []countedItem {
	items := make([]countedItem, 0, len(counts))
	for k, v := range counts {
		if k != "" && v > 0 {
			items = append(items, countedItem{key: k, count: v})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].count != items[j].count {
			return items[i].count > items[j].count
		}
		return items[i].key < items[j].key
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}

func summarizeConnectionDestinations(connections []storedConnection, limit int) []string {
	destCounts := make(map[string]int)
	for _, c := range connections {
		host := strings.TrimSpace(c.Host)
		dest := strings.TrimSpace(c.Destination)
		target := host
		if target == "" {
			target = dest
		} else if dest != "" {
			if _, port, err := net.SplitHostPort(dest); err == nil && port != "80" && port != "443" && port != "" {
				target = fmt.Sprintf("%s:%s", host, port)
			}
		}
		if target == "" {
			target = "未知目标"
		} else if host == "" && c.DestinationLocation != "" && c.DestinationLocation != "未知" {
			target = fmt.Sprintf("%s（%s）", target, c.DestinationLocation)
		}
		destCounts[target]++
	}

	top := topCountedItems(destCounts, limit)
	result := make([]string, 0, len(top))
	for _, item := range top {
		result = append(result, fmt.Sprintf("%s（%d 条）", item.key, item.count))
	}
	return result
}

func isInternalUserID(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return true
	}
	lower := strings.ToLower(name)
	return strings.HasPrefix(lower, "user_") || strings.HasPrefix(lower, "user-")
}

func (e *AlertEngine) summarizeSources(connections []storedConnection, limit int) []string {
	sourceCounts := make(map[string]int)
	for _, c := range connections {
		user := strings.TrimSpace(c.User)
		if isInternalUserID(user) {
			user = ""
		}
		srcIP := strings.TrimSpace(c.SourceIP)
		if srcIP == "" && c.Source != "" {
			if sh, _, err := net.SplitHostPort(c.Source); err == nil {
				srcIP = strings.TrimSpace(sh)
			} else {
				srcIP = strings.TrimSpace(c.Source)
			}
		}
		var label string
		if c.RelayUpstreamNodeID != "" && srcIP != "" {
			upstreamName := e.resolveNodeName(c.RelayUpstreamNodeID, c.RelayUpstreamNodeID)
			if user != "" {
				label = fmt.Sprintf("%s（%s｜经 %s 中转）", srcIP, user, upstreamName)
			} else {
				label = fmt.Sprintf("中转自 %s（%s）", upstreamName, srcIP)
			}
		} else if user != "" && srcIP != "" {
			label = fmt.Sprintf("%s（%s）", srcIP, user)
		} else if user != "" {
			label = user
		} else if srcIP != "" {
			label = srcIP
		} else {
			label = "未知客户端"
		}
		srcLoc := c.SourceLocation
		if (srcLoc == "" || srcLoc == "未知") && srcIP != "" && e.locator != nil {
			srcLoc = strings.TrimSpace(e.locator.Locate(srcIP))
		}
		if srcLoc != "" && srcLoc != "未知" && srcLoc != "未知归属地" && !strings.Contains(label, srcLoc) {
			label = fmt.Sprintf("%s（%s）", label, srcLoc)
		}
		sourceCounts[label]++
	}

	top := topCountedItems(sourceCounts, limit)
	result := make([]string, 0, len(top))
	for _, item := range top {
		result = append(result, fmt.Sprintf("%s（%d 条）", item.key, item.count))
	}
	return result
}

type nodeAlertContext struct {
	Name     string
	IP       string
	Location string
	Provider string
}

func (e *AlertEngine) nodeContext(nodeID, fallbackName string) nodeAlertContext {
	info := nodeAlertContext{Name: fallbackName}
	if e.store == nil || nodeID == "" {
		return info
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	node, err := e.store.NodeByID(ctx, nodeID)
	if err != nil {
		return info
	}
	if strings.TrimSpace(node.Name) != "" {
		info.Name = node.Name
	}
	ip := getNodePublicIPv4(node)
	if ip == "" {
		ip = strings.TrimSpace(node.LastObservedIP)
	}
	if ip == "" {
		ip = cleanIPAddress(node.ClientAddress)
	}
	info.IP = ip
	if ip != "" && e.locator != nil {
		location := strings.TrimSpace(e.locator.Locate(ip))
		if location != "" && location != "未知" {
			info.Location = location
		}
	}
	info.Provider = strings.TrimSpace(node.Provider)
	return info
}

func (c nodeAlertContext) TelegramLines() []string {
	lines := make([]string, 0, 5)
	if c.Name != "" {
		lines = append(lines, tgLine("服务器名称", c.Name))
	}
	if c.IP != "" {
		if net.ParseIP(c.IP) != nil {
			lines = append(lines, tgCodeLine("公网 IP", c.IP))
		} else {
			lines = append(lines, tgCodeLine("接入地址", c.IP))
		}
	}
	if c.Location != "" {
		lines = append(lines, tgLine("归属地", c.Location))
	}
	if c.Provider != "" {
		lines = append(lines, tgLine("厂商", c.Provider))
	}
	return lines
}

func (c nodeAlertContext) BarkSummary() string {
	parts := make([]string, 0, 4)
	if c.Name != "" {
		parts = append(parts, "服务器："+c.Name)
	}
	if c.IP != "" {
		if net.ParseIP(c.IP) != nil {
			parts = append(parts, "IP："+c.IP)
		} else {
			parts = append(parts, "地址："+c.IP)
		}
	}
	if c.Provider != "" {
		parts = append(parts, "厂商："+c.Provider)
	}
	if c.Location != "" {
		parts = append(parts, "归属地："+c.Location)
	}
	return strings.Join(parts, "｜")
}

func (c nodeAlertContext) BarkCompact() string {
	if c.Name != "" && c.IP != "" {
		return fmt.Sprintf("服务器：%s（%s）", c.Name, c.IP)
	}
	if c.Name != "" {
		return "服务器：" + c.Name
	}
	if c.IP != "" {
		return "IP：" + c.IP
	}
	return ""
}

func (e *AlertEngine) withNodeInfo(ctx nodeAlertContext, lines []string) []string {
	nodeLines := ctx.TelegramLines()
	if len(nodeLines) == 0 {
		return lines
	}
	out := make([]string, 0, len(lines)+len(nodeLines)+1)
	out = append(out, lines...)
	if len(lines) > 0 {
		out = append(out, "\n<b>服务器信息</b>")
	} else {
		out = append(out, "<b>服务器信息</b>")
	}
	out = append(out, nodeLines...)
	return out
}

func (e *AlertEngine) barkWithNode(ctx nodeAlertContext, body string) string {
	if strings.TrimSpace(body) == "" {
		return ctx.BarkSummary()
	}
	footer := ctx.BarkCompact()
	if footer == "" {
		return body
	}
	return body + "\n\n" + footer
}

func (e *AlertEngine) getSettings() (AlertSettings, error) {
	e.mu.Lock()
	if !e.cacheTime.IsZero() && time.Since(e.cacheTime) < 30*time.Second {
		s := e.cached
		e.mu.Unlock()
		return s, nil
	}
	e.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	s, err := e.store.GetAlertSettings(ctx)
	if err != nil {
		e.mu.Lock()
		defer e.mu.Unlock()
		if !e.cacheTime.IsZero() {
			return e.cached, nil
		}
		return AlertSettings{}, err
	}
	e.mu.Lock()
	e.cached = s
	e.cacheTime = time.Now()
	e.mu.Unlock()
	return s, nil
}

func (e *AlertEngine) InvalidateCache() {
	e.mu.Lock()
	e.cacheTime = time.Time{}
	e.mu.Unlock()
}

// notificationsEnabled reports whether any notification channel is configured.
// Callers that may see many addresses use this once, instead of asking the
// engine and starting a goroutine for every address.
func (e *AlertEngine) notificationsEnabled() bool {
	settings, err := e.getSettings()
	return err == nil && settings.HasNotifier()
}

// inCooldown reports whether the alert key is currently within cooldown without updating it.
func (e *AlertEngine) inCooldown(key string, cooldownMinutes int) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	last, exists := e.cooldowns[key]
	return exists && time.Since(last) < time.Duration(cooldownMinutes)*time.Minute
}

// canAlert checks if the alert key is outside of cooldown. If so, updates the cooldown time and returns true.
func (e *AlertEngine) canAlert(key string, cooldownMinutes int) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	last, exists := e.cooldowns[key]
	if exists && time.Since(last) < time.Duration(cooldownMinutes)*time.Minute {
		return false
	}
	e.cooldowns[key] = time.Now()
	return true
}

func (e *AlertEngine) sendAlert(_ context.Context, payload alertPayload) {
	go func() {
		settings, err := e.getSettings()
		if err != nil || !settings.HasNotifier() {
			return
		}

		// Channel 1: Bark (iOS APNs) — compact lock-screen notification.
		if settings.BarkReady() {
			barkSound := payload.BarkSound
			if barkSound == "" {
				if settings.BarkSound != "" {
					barkSound = settings.BarkSound
				} else {
					barkSound = "minuet.caf"
				}
			}
			if !strings.HasSuffix(barkSound, ".caf") {
				barkSound = barkSound + ".caf"
			}
			baseGroup := strings.TrimSpace(settings.BarkGroup)
			eventGroup := strings.TrimSpace(payload.BarkGroup)
			barkGroup := ""
			switch {
			case baseGroup != "" && eventGroup != "":
				barkGroup = baseGroup + "-" + eventGroup
			case eventGroup != "":
				barkGroup = eventGroup
			case baseGroup != "":
				barkGroup = baseGroup
			default:
				barkGroup = "Polaris"
			}
			barkBody := strings.TrimSpace(payload.BarkBody)
			if !strings.Contains(barkBody, "时间：") {
				if barkBody != "" {
					barkBody += "\n"
				}
				barkBody += "时间：" + FormatAlertTime(time.Now())
			}
			barkTitle := payload.BarkTitle
			if payload.Severity != "" {
				barkTitle = "[" + payload.Severity + "] " + barkTitle
			}
			msg := BarkMessage{
				DeviceKey: settings.BarkDeviceKey,
				Title:     barkTitle,
				Body:      barkBody,
				Group:     barkGroup,
				Sound:     barkSound,
				Level:     "active",
			}
			if e.bark != nil {
				pushCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				if err := e.bark.Send(pushCtx, settings.BarkServer, settings.BarkDeviceKey, msg); err != nil {
					log.Printf("failed to send bark notification (%s): %v", barkTitle, err)
				}
				cancel()
			}
		}

		// Channel 2: Telegram Bot API — HTML bot message with structured fields.
		if settings.TelegramReady() {
			text := payload.Telegram
			if strings.TrimSpace(text) == "" {
				text = payload.BarkTitle + "\n\n" + payload.BarkBody
			}
			if e.telegram != nil {
				pushCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				if err := e.telegram.SendHTML(pushCtx, settings.TelegramServer, settings.TelegramBotToken, settings.TelegramChatID, text); err != nil {
					log.Printf("failed to send telegram notification (%s): %v", payload.BarkTitle, err)
				}
				cancel()
			}
		}
	}()
}

// CheckConnectionsTelemetry evaluates live connection pushes from nodes against defined thresholds.
func (e *AlertEngine) CheckConnectionsTelemetry(nodeID, nodeName string, downloadRate, uploadRate float64, connections []storedConnection) {
	settings, err := e.getSettings()
	if err != nil || !settings.HasNotifier() {
		return
	}

	e.mu.Lock()
	e.latestConns[nodeID] = connections
	if nodeName != "" {
		e.nodeNames[nodeID] = nodeName
	}
	e.mu.Unlock()

	now := time.Now()
	allConnections := connections
	nonRelay := make([]storedConnection, 0, len(connections))
	confirmed := make([]storedConnection, 0, len(connections))
	for _, c := range connections {
		if c.IsChainRelay {
			continue
		}
		nonRelay = append(nonRelay, c)
		if c.OnChainTarget {
			pendingKey := nodeID + ":" + c.ID + "|" + c.Destination + "|" + c.Host
			if !e.confirmChainTargetTimeout(pendingKey, now) {
				continue
			}
		}
		confirmed = append(confirmed, c)
	}
	connections = confirmed
	totalRateBytes := downloadRate + uploadRate
	maxRateMbps := (totalRateBytes * 8) / (1000 * 1000)

	// 1. Instantaneous Bandwidth Spike Check
	if settings.TrafficAlertEnabled && settings.TrafficThresholdMbps > 0 {
		rateThresholdBytes := (settings.TrafficThresholdMbps * 1000 * 1000) / 8
		if totalRateBytes >= rateThresholdBytes {
			e.mu.Lock()
			start, exists := e.highRateAt[nodeID]
			if !exists {
				e.highRateAt[nodeID] = now
				start = now
			}
			durationExceeded := now.Sub(start) >= time.Duration(settings.TrafficDurationSec)*time.Second
			e.mu.Unlock()

			if durationExceeded {
				alertKey := "traffic:" + nodeID
				if e.canAlert(alertKey, settings.CooldownMinutes) {
					trafficConns := nonRelay
					if len(allConnections) > len(nonRelay) {
						trafficConns = e.enrichRelayConns(allConnections)
					}
					downloadMbps := (downloadRate * 8) / (1000 * 1000)
					uploadMbps := (uploadRate * 8) / (1000 * 1000)
					mainIP, mainDest := e.topTrafficEndpoints(trafficConns)
					if mainDest == "" {
						mainDest = "未指定域名/IP"
					}
					var topSourceDesc string
					if mainIP != "" {
						var mainUser string
						var mainLoc string
						var mainRelayNode string
						for _, c := range trafficConns {
							if cleanIPAddress(c.SourceIP) == mainIP {
								if c.User != "" && mainUser == "" && !isInternalUserID(c.User) {
									mainUser = c.User
								}
								if c.SourceLocation != "" && c.SourceLocation != "未知" && mainLoc == "" {
									mainLoc = c.SourceLocation
								}
								if c.RelayUpstreamNodeID != "" && mainRelayNode == "" {
									mainRelayNode = c.RelayUpstreamNodeID
								}
							}
						}
						if mainUser != "" {
							topSourceDesc = fmt.Sprintf("%s（%s）", mainIP, mainUser)
						} else {
							topSourceDesc = e.formatIP(mainIP)
						}
						if mainLoc != "" && !strings.Contains(topSourceDesc, mainLoc) {
							topSourceDesc += fmt.Sprintf("（%s）", mainLoc)
						}
						if mainRelayNode != "" {
							topSourceDesc += fmt.Sprintf("（中转：%s）", mainRelayNode)
						}
					}
					if topSourceDesc == "" {
						topSourceDesc = "未知来源"
					}

					overPercent := 0.0
					if settings.TrafficThresholdMbps > 0 {
						overPercent = (maxRateMbps/settings.TrafficThresholdMbps - 1) * 100
					}
					nodeCtx := e.nodeContext(nodeID, nodeName)
					lines := []string{
						tgLine("当前速率", fmt.Sprintf("%.1f Mbps（↓ %.1f ｜ ↑ %.1f Mbps）", maxRateMbps, downloadMbps, uploadMbps)),
						tgLine("主要目标", mainDest),
						tgLine("主要来源", topSourceDesc),
						tgLine("超限详情", fmt.Sprintf("阈值 %.1f Mbps · 超出 %.1f%% · 持续超过 %d 秒", settings.TrafficThresholdMbps, overPercent, settings.TrafficDurationSec)),
					}
					topDests := summarizeConnectionDestinations(trafficConns, 3)
					if len(topDests) > 1 {
						lines = append(lines, tgLine("其他活跃目标", strings.Join(topDests, "｜")))
					}

					telegramText := buildTelegramAlert(severityWarning, "流量告警", "瞬时流量突增", nodeName, e.withNodeInfo(nodeCtx, lines), "")
					barkBody := fmt.Sprintf("目标：%s\n速率：%.1f Mbps（↓%.1f / ↑%.1f · 持续 %d 秒）\n来源：%s",
						mainDest, maxRateMbps, downloadMbps, uploadMbps, settings.TrafficDurationSec, topSourceDesc)
					e.sendAlert(context.Background(), alertPayload{
						Severity:  severityWarning,
						BarkTitle: fmt.Sprintf("流量突增：%s", nodeName),
						BarkBody:  e.barkWithNode(nodeCtx, barkBody),
						BarkGroup: "流量监控",
						BarkSound: "bell.caf",
						Telegram:  telegramText,
					})
				}
			}
		} else {
			e.mu.Lock()
			delete(e.highRateAt, nodeID)
			e.mu.Unlock()
		}
	}

	// 2. Instantaneous Active Connection Surge Check
	connCount := len(connections)
	if settings.ConnAlertEnabled && settings.ConnThresholdCount > 0 && connCount >= settings.ConnThresholdCount {
		alertKey := "conn:" + nodeID
		if e.canAlert(alertKey, settings.CooldownMinutes) {
			overCount := connCount - settings.ConnThresholdCount
			if overCount < 0 {
				overCount = 0
			}
			topDests := summarizeConnectionDestinations(connections, 5)
			topSources := e.summarizeSources(connections, 3)

			nodeCtx := e.nodeContext(nodeID, nodeName)
			lines := []string{
				tgLine("当前连接", fmt.Sprintf("%d 条（上限 %d 条，超出 %d 条）", connCount, settings.ConnThresholdCount, overCount)),
			}
			if len(topDests) > 0 {
				lines = append(lines, tgLine("主要访问目标", "\n• "+strings.Join(topDests, "\n• ")))
			}
			if len(topSources) > 0 {
				lines = append(lines, tgLine("主要请求来源", "\n• "+strings.Join(topSources, "\n• ")))
			}

			telegramText := buildTelegramAlert(severityWarning, "连接告警", "活跃连接数激增", nodeName, e.withNodeInfo(nodeCtx, lines), "")

			barkBody := fmt.Sprintf("连接：%d 条（超限 %d 条）", connCount, overCount)
			if len(topDests) > 0 {
				shortDests := summarizeConnectionDestinations(connections, 2)
				barkBody += "\n目标：" + strings.Join(shortDests, "、")
			}
			if len(topSources) > 0 {
				shortSources := e.summarizeSources(connections, 2)
				barkBody += "\n来源：" + strings.Join(shortSources, "、")
			}

			e.sendAlert(context.Background(), alertPayload{
				Severity:  severityWarning,
				BarkTitle: fmt.Sprintf("连接数激增：%s", nodeName),
				BarkBody:  e.barkWithNode(nodeCtx, barkBody),
				BarkGroup: "连接监控",
				BarkSound: "anticipate.caf",
				Telegram:  telegramText,
			})
		}
	}

	// 3. Single IP Connection Surge Check
	if settings.SingleIPAlertEnabled && settings.SingleIPThresholdCount > 0 {
		ipConns := make(map[string][]storedConnection)
		for _, c := range connections {
			if ip := cleanIPAddress(c.SourceIP); ip != "" {
				ipConns[ip] = append(ipConns[ip], c)
			}
		}
		for ip, conns := range ipConns {
			count := len(conns)
			if count >= settings.SingleIPThresholdCount {
				alertKey := "single_ip:" + nodeID + ":" + ip
				if e.canAlert(alertKey, settings.CooldownMinutes) {
					overCount := count - settings.SingleIPThresholdCount
					if overCount < 0 {
						overCount = 0
					}
					topDests := summarizeConnectionDestinations(conns, 3)

					nodeCtx := e.nodeContext(nodeID, nodeName)
					lines := []string{
						tgLine("来源 IP", e.formatIP(ip)),
						tgLine("活跃连接", fmt.Sprintf("%d 条", count)),
						tgLine("单 IP 上限", fmt.Sprintf("%d 条", settings.SingleIPThresholdCount)),
						tgLine("超出数量", fmt.Sprintf("%d 条", overCount)),
					}
					if len(topDests) > 0 {
						lines = append(lines, tgLine("主要访问目标", "\n• "+strings.Join(topDests, "\n• ")))
					}

					telegramText := buildTelegramAlert(severityCritical, "安全告警", "单 IP 异常高并发", nodeName, e.withNodeInfo(nodeCtx, lines), "")

					barkBody := fmt.Sprintf("%s\n%d 条连接，超限 %d 条",
						e.formatIP(ip), count, overCount)
					if len(topDests) > 0 {
						barkBody += "\n目标：" + strings.Join(topDests, "、")
					}

					e.sendAlert(context.Background(), alertPayload{
						Severity:  severityCritical,
						BarkTitle: fmt.Sprintf("单 IP 高并发：%s", nodeName),
						BarkBody:  e.barkWithNode(nodeCtx, barkBody),
						BarkGroup: "安全警报",
						BarkSound: "horn.caf",
						Telegram:  telegramText,
					})
				}
			}
		}
	}

	e.checkProxyRouting(nodeID, nodeName, connections, settings)

	// 4. Abnormal Port/Host Scanning Detection (strictly exempts standard domain-name web browsing)
	if !settings.ScanAlertEnabled {
		return
	}

	sensitiveScanPorts := map[int]string{
		21:    "FTP",
		22:    "SSH",
		23:    "Telnet",
		25:    "SMTP",
		135:   "RPC",
		137:   "NetBIOS",
		139:   "NetBIOS",
		445:   "SMB",
		1433:  "MSSQL",
		1521:  "Oracle",
		3306:  "MySQL",
		3389:  "RDP",
		5432:  "PostgreSQL",
		5900:  "VNC",
		6379:  "Redis",
		9200:  "Elasticsearch",
		11211: "Memcached",
		27017: "MongoDB",
	}

	type clientScanStats struct {
		targetPorts    map[string]map[int]struct{}
		sensitiveHosts map[string]map[string]struct{}
		rawIPTargets   map[string]struct{}
	}

	clientStats := make(map[string]*clientScanStats)

	for _, c := range connections {
		clientIP := cleanIPAddress(c.SourceIP)
		if clientIP == "" {
			continue
		}

		dest := strings.TrimSpace(c.Destination)
		destHost := dest
		destPort := 0
		if h, pStr, err := net.SplitHostPort(dest); err == nil {
			destHost = h
			if p, err := strconv.Atoi(pStr); err == nil {
				destPort = p
			}
		}

		// Check if host is a domain name (FQDN). net.ParseIP excludes literal
		// addresses, including IPv6, so internationalized domains are handled
		// without the old ASCII-letter heuristic.
		host := strings.TrimSpace(c.Host)
		isDomain := host != "" && net.ParseIP(host) == nil && strings.Contains(host, ".")

		// Standard web browsing: domain name on common web ports (80, 443, 8080, 8443) or port 0
		// MUST be completely exempted! Browsing modern websites naturally opens dozens of domain connections.
		if isDomain && (destPort == 80 || destPort == 443 || destPort == 8080 || destPort == 8443 || destPort == 0) {
			continue
		}

		// Also exempt domain-based connections to other common application services unless targeting sensitive ports
		if isDomain {
			if _, isSensitive := sensitiveScanPorts[destPort]; !isSensitive {
				continue
			}
		}

		st, exists := clientStats[clientIP]
		if !exists {
			st = &clientScanStats{
				targetPorts:    make(map[string]map[int]struct{}),
				sensitiveHosts: make(map[string]map[string]struct{}),
				rawIPTargets:   make(map[string]struct{}),
			}
			clientStats[clientIP] = st
		}

		if destHost != "" && destPort > 0 {
			if _, ok := st.targetPorts[destHost]; !ok {
				st.targetPorts[destHost] = make(map[int]struct{})
			}
			st.targetPorts[destHost][destPort] = struct{}{}
		}

		if svc, isSensitive := sensitiveScanPorts[destPort]; isSensitive && destHost != "" {
			if st.sensitiveHosts[destHost] == nil {
				st.sensitiveHosts[destHost] = make(map[string]struct{})
			}
			st.sensitiveHosts[destHost][svc] = struct{}{}
		}

		if !isDomain && destHost != "" && net.ParseIP(destHost) != nil {
			st.rawIPTargets[destHost] = struct{}{}
		}
	}

	for clientIP, st := range clientStats {
		var triggerReason, triggerDetails string

		// Condition A: Vertical Port Scan (single destination IP with >= 10 distinct probed ports)
		for targetIP, ports := range st.targetPorts {
			if len(ports) >= 10 {
				portList := make([]int, 0, len(ports))
				for port := range ports {
					portList = append(portList, port)
				}
				sort.Ints(portList)
				sampleCount := min(6, len(portList))
				portStrs := make([]string, 0, sampleCount)
				for _, port := range portList[:sampleCount] {
					portStrs = append(portStrs, strconv.Itoa(port))
				}
				triggerReason = fmt.Sprintf("单目标多端口嗅探 (尝试连接目标 %s 的 %d 个不同端口)", targetIP, len(ports))
				triggerDetails = fmt.Sprintf("目标地址: %s, 端口样例: %s", targetIP, strings.Join(portStrs, ", "))
				break
			}
		}

		// Condition B: Sensitive Port Sweep (probing high-risk ports across >= 5 distinct targets)
		if triggerReason == "" && len(st.sensitiveHosts) >= 5 {
			hosts := make([]string, 0, len(st.sensitiveHosts))
			for host := range st.sensitiveHosts {
				hosts = append(hosts, host)
			}
			sort.Strings(hosts)
			sampleCount := min(5, len(hosts))
			samples := make([]string, 0, sampleCount)
			for _, target := range hosts[:sampleCount] {
				names := make([]string, 0, len(st.sensitiveHosts[target]))
				for service := range st.sensitiveHosts[target] {
					names = append(names, service)
				}
				sort.Strings(names)
				samples = append(samples, fmt.Sprintf("%s (%s)", target, strings.Join(names, "/")))
			}
			triggerReason = fmt.Sprintf("高危服务端口扫描 (探测 %d 个不同目标的高危端口)", len(st.sensitiveHosts))
			triggerDetails = fmt.Sprintf("样例: %s", strings.Join(samples, ", "))
		}

		// Condition C: Mass Raw IP Sweep (>= 40 distinct raw IPs without domain names on non-web ports)
		if triggerReason == "" && len(st.rawIPTargets) >= 40 {
			rawIPs := make([]string, 0, len(st.rawIPTargets))
			for ip := range st.rawIPTargets {
				rawIPs = append(rawIPs, ip)
			}
			sort.Strings(rawIPs)
			sampleCount := min(5, len(rawIPs))
			triggerReason = fmt.Sprintf("纯 IP 地址段并发漫游扫描 (探测 %d 个不同纯 IP 目标)", len(st.rawIPTargets))
			triggerDetails = fmt.Sprintf("目标样例: %s", strings.Join(rawIPs[:sampleCount], ", "))
		}

		if triggerReason != "" {
			alertKey := "scan:" + nodeID + ":" + clientIP
			cooldown := settings.CooldownMinutes
			if cooldown < 15 {
				cooldown = 15
			}
			if e.canAlert(alertKey, cooldown) {
				var scannerListener string
				for _, c := range connections {
					if cleanIPAddress(c.SourceIP) == clientIP {
						if c.ListenerName != "" && scannerListener == "" {
							scannerListener = c.ListenerName
						} else if c.Inbound != "" && scannerListener == "" {
							scannerListener = c.Inbound
						}
					}
				}

				nodeCtx := e.nodeContext(nodeID, nodeName)
				scanLines := []string{
					tgLine("来源 IP", e.formatIP(clientIP)),
				}
				if scannerListener != "" {
					scanLines = append(scanLines, tgLine("接入入站", scannerListener))
				}
				scanLines = append(scanLines,
					tgLine("扫描特征", triggerReason),
					tgLine("探测详情", triggerDetails),
				)

				barkBody := fmt.Sprintf("%s\n%s\n%s",
					e.formatIP(clientIP), triggerReason, triggerDetails)
				telegramText := buildTelegramAlert(severityCritical, "安全告警", "检测到异常网络扫描", nodeName, e.withNodeInfo(nodeCtx, scanLines), "")
				e.sendAlert(context.Background(), alertPayload{
					Severity:  severityCritical,
					BarkTitle: fmt.Sprintf("异常扫描：%s", nodeName),
					BarkBody:  e.barkWithNode(nodeCtx, barkBody),
					BarkGroup: "安全警报",
					BarkSound: "alarm.caf",
					Telegram:  telegramText,
				})
			}
		}
	}
}

type routeIssue struct {
	destIP    string
	destIPs   []string
	destIPSet map[string]struct{}
	isLocal   bool
	location  string
	outbound  string
	chainPath string
	isChained bool
	host      string
	hosts     []string
	count     int
	ports     []string
	users     []string
	sourceIPs []string
	listeners []string
	networks  []string
	servers   []string
}

func (issue *routeIssue) addDestIP(ip string) {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return
	}
	if issue.destIP == "" {
		issue.destIP = ip
	}
	if issue.destIPSet == nil {
		issue.destIPSet = make(map[string]struct{})
	}
	if _, exists := issue.destIPSet[ip]; !exists {
		issue.destIPSet[ip] = struct{}{}
		issue.destIPs = append(issue.destIPs, ip)
	}
}

func (issue *routeIssue) distinctIPCount() int {
	if len(issue.destIPs) > 0 {
		return len(issue.destIPs)
	}
	if issue.destIP != "" {
		return 1
	}
	return 0
}

func (issue *routeIssue) trafficType() string {
	if issue.isLocal && issue.distinctIPCount() > 1 && len(issue.ports) > 1 {
		netStr := strings.ToUpper(strings.Join(issue.networks, "/"))
		portDesc := formatLimitedList(issue.ports, 5, "个端口")
		if netStr != "" {
			return fmt.Sprintf("多目标内网访问 (%s · 端口 %s)", netStr, portDesc)
		}
		return fmt.Sprintf("多目标内网访问 (端口 %s)", portDesc)
	}

	hasPort := func(p string) bool {
		for _, port := range issue.ports {
			if port == p {
				return true
			}
		}
		return false
	}
	hasNet := func(n string) bool {
		for _, netw := range issue.networks {
			if strings.EqualFold(netw, n) {
				return true
			}
		}
		return false
	}

	if hasPort("3478") || hasPort("5349") || strings.Contains(strings.ToLower(issue.host), "stun") || strings.Contains(strings.ToLower(issue.host), "turn") {
		return "STUN / WebRTC 音视频通话打洞（命中防真实 IP 泄露规则）"
	}
	if hasPort("53") {
		return "DNS 域名解析 (UDP/53)"
	}
	if hasPort("853") {
		return "DoT 加密 DNS (TCP/853)"
	}
	if hasPort("123") {
		return "NTP 网络授时 (UDP/123)"
	}
	if hasPort("443") && (hasNet("udp") || len(issue.networks) == 0) {
		if hasNet("udp") {
			if net.ParseIP(issue.destIP) != nil && issue.host == "" {
				return "QUIC / HTTP3（国内应用 UDP/443 直连 IP）"
			}
			return "QUIC / HTTP3（国内应用 UDP/443）"
		}
	}
	if hasPort("443") {
		return "HTTPS Web 流量"
	}
	if hasPort("80") {
		return "HTTP Web 流量"
	}
	if hasPort("22") {
		return "SSH 远程管理"
	}
	if hasPort("5223") {
		return "Apple APNs 苹果推送服务"
	}
	if hasPort("5228") || hasPort("5229") || hasPort("5230") {
		return "Android / FCM 消息推送"
	}
	netStr := strings.ToUpper(strings.Join(issue.networks, "/"))
	portStr := strings.Join(issue.ports, ",")
	if netStr != "" && portStr != "" {
		return fmt.Sprintf("%s / 端口 %s", netStr, portStr)
	}
	if portStr != "" {
		return fmt.Sprintf("端口 %s", portStr)
	}
	return "普通网络流量"
}

func appendDistinctLimit(list []string, item string, limit int) []string {
	item = strings.TrimSpace(item)
	if item == "" {
		return list
	}
	for _, existing := range list {
		if existing == item {
			return list
		}
	}
	if limit > 0 && len(list) >= limit {
		return list
	}
	return append(list, item)
}

const chainRelayConfirmWait = 5 * time.Second

func (e *AlertEngine) confirmChainTargetTimeout(key string, now time.Time) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.pendingChainFirstSeen == nil {
		e.pendingChainFirstSeen = make(map[string]pendingChainEntry)
	}
	for k, entry := range e.pendingChainFirstSeen {
		if now.Sub(entry.lastSeen) > 5*time.Minute {
			delete(e.pendingChainFirstSeen, k)
		}
	}
	entry, exists := e.pendingChainFirstSeen[key]
	if !exists {
		e.pendingChainFirstSeen[key] = pendingChainEntry{firstSeen: now, lastSeen: now}
		return false
	}
	entry.lastSeen = now
	e.pendingChainFirstSeen[key] = entry
	return now.Sub(entry.firstSeen) >= chainRelayConfirmWait
}

type matchedRouteConn struct {
	connID    string
	isLocal   bool
	destIP    string
	location  string
	outbound  string
	chainPath string
	isChained bool
	host      string
	network   string
	destPort  string
	user      string
	srcDesc   string
	listener  string
}

func mergeRouteIssue(dst, src *routeIssue) {
	dst.count += src.count
	if len(src.destIPs) > 0 {
		for _, ip := range src.destIPs {
			dst.addDestIP(ip)
		}
	} else if src.destIP != "" {
		dst.addDestIP(src.destIP)
	}
	if dst.host == "" && src.host != "" {
		dst.host = src.host
	}
	for _, h := range src.hosts {
		dst.hosts = appendDistinctLimit(dst.hosts, h, 5)
	}
	if src.host != "" {
		dst.hosts = appendDistinctLimit(dst.hosts, src.host, 5)
	}
	for _, n := range src.networks {
		dst.networks = appendDistinctLimit(dst.networks, n, 3)
	}
	for _, p := range src.ports {
		dst.ports = appendDistinctLimit(dst.ports, p, 8)
	}
	for _, u := range src.users {
		dst.users = appendDistinctLimit(dst.users, u, 5)
	}
	for _, s := range src.sourceIPs {
		dst.sourceIPs = appendDistinctLimit(dst.sourceIPs, s, 5)
	}
	for _, l := range src.listeners {
		dst.listeners = appendDistinctLimit(dst.listeners, l, 5)
	}
	for _, srv := range src.servers {
		dst.servers = appendDistinctLimit(dst.servers, srv, 10)
	}
}

func (e *AlertEngine) proxyRouteBatchWindow() time.Duration {
	if e.proxyRouteWait > 0 {
		return e.proxyRouteWait
	}
	return defaultProxyRouteWindow
}

var domesticBaseDomains = map[string]struct{}{
	// Baidu
	"baidu.com": {}, "bdstatic.com": {}, "bcebos.com": {}, "baidupcs.com": {}, "baidustatic.com": {}, "hao123.com": {},
	// Alibaba
	"taobao.com": {}, "tmall.com": {}, "alibaba.com": {}, "alipay.com": {}, "aliyun.com": {}, "alicdn.com": {},
	"1688.com": {}, "cainiao.com": {}, "dingtalk.com": {}, "fliggy.com": {}, "ele.me": {}, "alipayobjects.com": {}, "yuque.com": {},
	"tbcdn.cn": {},
	// Tencent
	"qq.com": {}, "tencent.com": {}, "wechat.com": {}, "weixin.com": {}, "gtimg.com": {}, "idqqimg.com": {},
	"myqcloud.com": {}, "tencent-cloud.com": {}, "qcloud.com": {}, "qpic.cn": {}, "qlogo.cn": {},
	// ByteDance
	"douyin.com": {}, "bytedance.com": {}, "byteimg.com": {}, "byteeffecttos.com": {}, "feishu.net": {}, "feishu.cn": {},
	"zijieapi.com": {}, "iesdouyin.com": {}, "pstatp.com": {}, "snssdk.com": {}, "toutiao.com": {}, "huoshan.com": {},
	"xigua.com": {}, "dongchedi.com": {},
	// Bilibili
	"bilibili.com": {}, "biliapi.net": {}, "hdslb.com": {}, "bilivideo.com": {}, "bilibili.tv": {},
	// JD
	"jd.com": {}, "360buy.com": {}, "360buyimg.com": {}, "jdcache.com": {}, "jdpay.com": {},
	// NetEase
	"163.com": {}, "126.com": {}, "netease.com": {}, "127.net": {}, "ydstatic.com": {}, "youdao.com": {}, "kaola.com": {},
	// Sina & Weibo
	"weibo.com": {}, "sina.com": {}, "sina.com.cn": {}, "sinaimg.cn": {}, "sinajs.cn": {}, "weibo.cn": {},
	// Zhihu
	"zhihu.com": {}, "zhimg.com": {},
	// Meituan & Dianping
	"meituan.com": {}, "meituan.net": {}, "dianping.com": {}, "sankuai.com": {}, "dpfile.com": {},
	// Xiaomi
	"xiaomi.com": {}, "mi.com": {}, "mi-img.com": {}, "xiaomi.net": {},
	// Huawei & Honor
	"huawei.com": {}, "honor.com": {}, "dbankcdn.com": {}, "vmall.com": {}, "huaweicloud.com": {},
	// Kuaishou
	"kuaishou.com": {}, "yximgs.com": {}, "kwai.com": {}, "gifshow.com": {},
	// Xiaohongshu
	"xiaohongshu.com": {}, "xhscdn.com": {},
	// Video & Music & Entertainment
	"iqiyi.com": {}, "youku.com": {}, "tudou.com": {}, "mgtv.com": {}, "ximalaya.com": {}, "kugou.com": {}, "kuwo.cn": {},
	// Tech & Community
	"csdn.net": {}, "cnblogs.com": {}, "segmentfault.com": {}, "gitee.com": {}, "oschina.net": {}, "douban.com": {}, "tieba.com": {},
	// Travel & Transport & Maps
	"12306.cn": {}, "ctrip.com": {}, "qunar.com": {}, "amap.com": {}, "autonavi.com": {}, "didiglobal.com": {}, "didichuxing.com": {},
	// Office / Cloud / Security
	"wps.com": {}, "wps.cn": {}, "kingsoft.com": {}, "volces.com": {}, "qiniu.com": {}, "upyun.com": {},
	"360.cn": {}, "360.com": {}, "so.com": {}, "qihoo.com": {},
	// Apple China
	"apple.com.cn": {}, "icloud.com.cn": {},
	// Carriers
	"189.cn": {}, "10010.com": {}, "10086.cn": {},
	// Duolingo China
	"duolingo.cn": {},
}

func isDomesticDomain(domain string) bool {
	domain = strings.TrimSpace(strings.ToLower(domain))
	if domain == "" {
		return false
	}
	domain = strings.TrimSuffix(domain, ".")

	// 1. ccTLD .cn and common Chinese IDN TLDs
	if domain == "cn" || strings.HasSuffix(domain, ".cn") {
		return true
	}
	if domain == "中国" || strings.HasSuffix(domain, ".中国") ||
		domain == "中國" || strings.HasSuffix(domain, ".中國") ||
		domain == "xn--fiqs8s" || strings.HasSuffix(domain, ".xn--fiqs8s") ||
		domain == "xn--fiqz9s" || strings.HasSuffix(domain, ".xn--fiqz9s") ||
		domain == "公司" || strings.HasSuffix(domain, ".公司") ||
		domain == "xn--55qx5d" || strings.HasSuffix(domain, ".xn--55qx5d") ||
		domain == "网络" || strings.HasSuffix(domain, ".网络") ||
		domain == "xn--io0a7i" || strings.HasSuffix(domain, ".xn--io0a7i") {
		return true
	}

	// 2. Direct lookup or suffix match against domesticBaseDomains
	if _, ok := domesticBaseDomains[domain]; ok {
		return true
	}
	parts := strings.Split(domain, ".")
	for i := 1; i < len(parts)-1; i++ {
		sub := strings.Join(parts[i:], ".")
		if _, ok := domesticBaseDomains[sub]; ok {
			return true
		}
	}

	return false
}

func isLocalDomain(domain string) bool {
	domain = strings.TrimSpace(strings.ToLower(domain))
	if domain == "" {
		return false
	}
	domain = strings.TrimSuffix(domain, ".")
	if domain == "localhost" || strings.HasSuffix(domain, ".localhost") {
		return true
	}
	if domain == "local" || strings.HasSuffix(domain, ".local") {
		return true
	}
	if domain == "lan" || strings.HasSuffix(domain, ".lan") {
		return true
	}
	if domain == "home.arpa" || strings.HasSuffix(domain, ".home.arpa") {
		return true
	}
	if domain == "internal" || strings.HasSuffix(domain, ".internal") {
		return true
	}
	return false
}

// checkProxyRouting reports destinations that belong to China or a private
// network but are currently leaving through a proxy outbound. That is almost
// always a split-routing mistake: domestic and LAN traffic should go direct.
// Private/LAN IP targets are merged per outbound, and alerts across nodes
// within a short burst window are combined into a single summary notification.
func (e *AlertEngine) checkProxyRouting(nodeID, nodeName string, connections []storedConnection, settings AlertSettings) {
	if len(connections) == 0 {
		return
	}

	cooldown := settings.CooldownMinutes
	if cooldown < 30 {
		cooldown = 30
	}
	if e.inCooldown("proxy_route:"+nodeID, cooldown) {
		return
	}

	matched := make([]matchedRouteConn, 0, len(connections))
	for _, connection := range connections {
		if connection.IsChainRelay {
			continue
		}
		destination := strings.TrimSpace(connection.Destination)
		host := strings.TrimSpace(connection.Host)
		ipText := destination
		var destPort string
		if parsedHost, port, err := net.SplitHostPort(destination); err == nil {
			ipText = parsedHost
			destPort = strings.TrimSpace(port)
		}
		if h, p, err := net.SplitHostPort(host); err == nil {
			host = strings.TrimSpace(h)
			if destPort == "" {
				destPort = strings.TrimSpace(p)
			}
		}
		ipText = strings.TrimSpace(ipText)
		ip := net.ParseIP(ipText)
		if ip == nil {
			// A few agents only fill Host, so accept an IP literal there too.
			if parsed := net.ParseIP(host); parsed != nil {
				ip = parsed
				ipText = host
			}
		}

		var (
			isLocal    bool
			isDomestic bool
			location   string
		)

		if ip != nil {
			if ip.IsLoopback() || ip.IsUnspecified() {
				continue
			}
			isLocal = ip.IsPrivate() || ip.IsLinkLocalUnicast()
			if !isLocal && e.locator != nil {
				isDomestic = e.locator.IsDomestic(ipText)
			}
			if !isLocal && !isDomestic && host != "" && isDomesticDomain(host) {
				isDomestic = true
			}
			if !isLocal && !isDomestic {
				continue
			}

			if isLocal {
				location = "局域网/内网"
			} else if e.locator != nil {
				if resolvedLoc := strings.TrimSpace(e.locator.Locate(ipText)); resolvedLoc != "" && resolvedLoc != "未知" {
					location = resolvedLoc
				}
			}
			if location == "" && isDomestic {
				location = "中国大陆"
			}
		} else {
			targetDomain := host
			if targetDomain == "" {
				targetDomain = ipText
			}
			if targetDomain == "" {
				continue
			}

			if isLocalDomain(targetDomain) {
				isLocal = true
				location = "局域网/内网"
			} else if isDomesticDomain(targetDomain) {
				isDomestic = true
				location = "中国大陆 (域名)"
			} else {
				continue
			}
			ipText = targetDomain
			if host == "" {
				host = targetDomain
			}
		}

		resolved := e.resolveChain(nodeID, nodeName, connection)
		if !resolved.IsChained && resolved.FinalIsDirect {
			continue
		}
		outbound := strings.TrimSpace(resolved.FinalOutbound)
		if outbound == "" {
			continue
		}
		displayOutbound := outbound
		if resolved.IsChained && resolved.FinalIsDirect && resolved.FinalNodeName != "" && resolved.FinalNodeName != nodeName {
			displayOutbound = resolved.FinalNodeName
		}
		var chainPath string
		if resolved.IsChained && len(resolved.Path) > 0 {
			chainPath = strings.Join(resolved.Path, " → ") + " → " + outbound
		}

		if location == "" {
			if isLocal {
				location = "局域网/内网"
			} else {
				location = "中国大陆"
			}
		}

		var user string
		if connection.User != "" && !isInternalUserID(connection.User) {
			user = connection.User
		}
		srcIP := strings.TrimSpace(connection.SourceIP)
		if srcIP == "" && connection.Source != "" {
			if sh, _, err := net.SplitHostPort(connection.Source); err == nil {
				srcIP = strings.TrimSpace(sh)
			} else {
				srcIP = strings.TrimSpace(connection.Source)
			}
		}
		var srcDesc string
		if srcIP != "" {
			srcDesc = e.formatIP(srcIP)
			if !strings.Contains(srcDesc, "（") {
				if loc := strings.TrimSpace(connection.SourceLocation); loc != "" && loc != "未知" && loc != "未知归属地" {
					srcDesc += fmt.Sprintf("（%s）", loc)
				}
			}
		}
		listener := strings.TrimSpace(connection.ListenerName)
		if listener == "" {
			listener = strings.TrimSpace(connection.Inbound)
		}

		matched = append(matched, matchedRouteConn{
			connID:    strings.TrimSpace(connection.ID),
			isLocal:   isLocal,
			destIP:    ipText,
			location:  location,
			outbound:  displayOutbound,
			chainPath: chainPath,
			isChained: resolved.IsChained,
			host:      host,
			network:   strings.ToLower(connection.Network),
			destPort:  destPort,
			user:      user,
			srcDesc:   srcDesc,
			listener:  listener,
		})
	}
	if len(matched) == 0 {
		return
	}

	now := time.Now()
	wait := e.proxyRouteBatchWindow()

	e.proxyRouteMu.Lock()
	batch := e.proxyRouteBatch
	if batch == nil {
		batch = &proxyRouteAlertBatch{
			started: now,
			fireAt:  now.Add(wait),
			nodes:   make(map[string]*proxyRouteNodeEntry),
		}
		batch.timer = time.AfterFunc(wait, func() { e.flushProxyRouteBatch(batch) })
		e.proxyRouteBatch = batch
	} else {
		batch.fireAt = extendBoundedTimer(batch.timer, batch.started, batch.fireAt, now, wait)
	}

	entry := batch.nodes[nodeID]
	if entry == nil {
		entry = &proxyRouteNodeEntry{
			nodeID:   nodeID,
			nodeName: nodeName,
			cooldown: cooldown,
			issues:   make(map[string]*routeIssue),
			seenConn: make(map[string]struct{}),
		}
		batch.nodes[nodeID] = entry
	}
	entry.cooldown = cooldown
	if nodeName != "" {
		entry.nodeName = nodeName
	}

	for _, m := range matched {
		if m.connID != "" {
			if _, seen := entry.seenConn[m.connID]; seen {
				continue
			}
			entry.seenConn[m.connID] = struct{}{}
		}
		key := m.destIP + "|" + m.outbound
		if m.isLocal {
			key = "local|" + m.outbound
		}
		issue := entry.issues[key]
		if issue == nil {
			issue = &routeIssue{
				destIP:    m.destIP,
				isLocal:   m.isLocal,
				location:  m.location,
				outbound:  m.outbound,
				chainPath: m.chainPath,
				isChained: m.isChained,
				host:      m.host,
			}
			entry.issues[key] = issue
		}
		issue.count++
		issue.addDestIP(m.destIP)
		if issue.host == "" && m.host != "" {
			issue.host = m.host
		}
		if m.host != "" {
			issue.hosts = appendDistinctLimit(issue.hosts, m.host, 5)
		}
		if m.network != "" {
			issue.networks = appendDistinctLimit(issue.networks, m.network, 3)
		}
		if m.destPort != "" {
			issue.ports = appendDistinctLimit(issue.ports, m.destPort, 8)
		}
		if m.user != "" {
			issue.users = appendDistinctLimit(issue.users, m.user, 5)
		}
		if m.srcDesc != "" {
			issue.sourceIPs = appendDistinctLimit(issue.sourceIPs, m.srcDesc, 5)
		}
		if m.listener != "" {
			issue.listeners = appendDistinctLimit(issue.listeners, m.listener, 5)
		}
		if entry.nodeName != "" {
			issue.servers = appendDistinctLimit(issue.servers, entry.nodeName, 10)
		}
	}
	shouldFlush := len(batch.nodes) >= maxProxyRouteBatchNodes
	e.proxyRouteMu.Unlock()

	if shouldFlush {
		e.flushProxyRouteBatch(batch)
	}
}

func (e *AlertEngine) flushProxyRouteBatch(target *proxyRouteAlertBatch) {
	e.proxyRouteMu.Lock()
	if e.proxyRouteBatch == nil || (target != nil && e.proxyRouteBatch != target) {
		e.proxyRouteMu.Unlock()
		return
	}
	batch := e.proxyRouteBatch
	e.proxyRouteBatch = nil
	if batch.timer != nil {
		batch.timer.Stop()
	}
	e.proxyRouteMu.Unlock()

	if len(batch.nodes) == 0 {
		return
	}
	e.sendProxyRouteSummary(batch)
}

func (e *AlertEngine) sendProxyRouteSummary(batch *proxyRouteAlertBatch) {
	settings, err := e.getSettings()
	if err != nil || !settings.HasNotifier() {
		return
	}

	entries := make([]*proxyRouteNodeEntry, 0, len(batch.nodes))
	for _, entry := range batch.nodes {
		if len(entry.issues) > 0 {
			entries = append(entries, entry)
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].nodeName != entries[j].nodeName {
			return entries[i].nodeName < entries[j].nodeName
		}
		return entries[i].nodeID < entries[j].nodeID
	})

	activeNodes := make([]*proxyRouteNodeEntry, 0, len(entries))
	for _, entry := range entries {
		if e.canAlert("proxy_route:"+entry.nodeID, entry.cooldown) {
			activeNodes = append(activeNodes, entry)
		}
	}
	if len(activeNodes) == 0 {
		return
	}

	merged := make(map[string]*routeIssue)
	serverNames := make([]string, 0, len(activeNodes))
	for _, entry := range activeNodes {
		if entry.nodeName != "" {
			serverNames = appendDistinctLimit(serverNames, entry.nodeName, 0)
		}
		for key, issue := range entry.issues {
			existing := merged[key]
			if existing == nil {
				clone := &routeIssue{
					destIP:    issue.destIP,
					isLocal:   issue.isLocal,
					location:  issue.location,
					outbound:  issue.outbound,
					chainPath: issue.chainPath,
					isChained: issue.isChained,
					host:      issue.host,
				}
				mergeRouteIssue(clone, issue)
				merged[key] = clone
			} else {
				mergeRouteIssue(existing, issue)
			}
		}
	}
	if len(merged) == 0 {
		return
	}

	list := make([]*routeIssue, 0, len(merged))
	totalTargets := 0
	for _, issue := range merged {
		list = append(list, issue)
		n := issue.distinctIPCount()
		if n < 1 {
			n = 1
		}
		totalTargets += n
	}
	sort.Slice(list, func(left, right int) bool {
		if list[left].count != list[right].count {
			return list[left].count > list[right].count
		}
		return list[left].destIP < list[right].destIP
	})

	multiServer := len(activeNodes) > 1
	lines := make([]string, 0, len(list)*5)
	barkBlocks := make([]string, 0, 3)

	for index, issue := range list {
		isMergedIPs := issue.distinctIPCount() > 1
		var addrLabel string
		var sortedIPs []string
		if isMergedIPs {
			sortedIPs = append([]string(nil), issue.destIPs...)
			sortIPStrings(sortedIPs)
		} else {
			addrLabel = issue.destIP
			portList := issue.ports
			if len(portList) > 3 {
				portList = portList[:3]
			}
			portDesc := strings.Join(portList, ", ")
			if len(issue.networks) > 0 {
				netDesc := strings.ToUpper(strings.Join(issue.networks, "/"))
				if portDesc != "" {
					addrLabel += fmt.Sprintf(":%s (%s)", portDesc, netDesc)
				}
			} else if len(portList) == 1 {
				addrLabel += ":" + portList[0]
			} else if len(portList) > 1 {
				addrLabel += fmt.Sprintf(" (端口 %s)", strings.Join(portList, ", "))
			}
		}

		userList := issue.users
		if len(userList) > 3 {
			userList = userList[:3]
		}
		listenerList := issue.listeners
		if len(listenerList) > 3 {
			listenerList = listenerList[:3]
		}
		nodeDesc := strings.Join(userList, "、")
		if nodeDesc == "" && len(listenerList) > 0 {
			nodeDesc = strings.Join(listenerList, "、")
		}
		srcList := issue.sourceIPs
		if len(srcList) > 3 {
			srcList = srcList[:3]
		}

		if index < 5 {
			var titleHTML string
			if isMergedIPs {
				shown := sortedIPs
				if len(shown) > 3 {
					shown = shown[:3]
				}
				codeIPs := make([]string, 0, len(shown))
				for _, ip := range shown {
					codeIPs = append(codeIPs, fmt.Sprintf("<code>%s</code>", tgEscape(ip)))
				}
				titleHTML = strings.Join(codeIPs, "、")
				if len(sortedIPs) > len(shown) {
					titleHTML += fmt.Sprintf(" 等 %d 个内网 IP", len(sortedIPs))
				}
			} else if issue.host != "" && issue.host != issue.destIP && !strings.HasPrefix(addrLabel, issue.host+":") {
				titleHTML = fmt.Sprintf("<code>%s</code>（<code>%s</code>）", tgEscape(issue.host), tgEscape(addrLabel))
			} else {
				titleHTML = fmt.Sprintf("<code>%s</code>", tgEscape(addrLabel))
			}
			itemHeader := fmt.Sprintf("<b>[%d] %s</b>｜%s · %d 条",
				index+1, titleHTML, tgEscape(issue.location), issue.count)
			if index > 0 {
				itemHeader = "\n" + itemHeader
			}
			lines = append(lines, itemHeader)
			if multiServer && len(issue.servers) > 0 {
				lines = append(lines, tgLine("服务器", formatLimitedList(issue.servers, 5, "台")))
			}
			lines = append(lines, tgLine("类型", issue.trafficType()))
			if isMergedIPs && len(issue.hosts) > 0 {
				lines = append(lines, tgLine("访问域名", formatLimitedList(issue.hosts, 3, "个")))
			}
			if nodeDesc != "" {
				lines = append(lines, tgLine("节点", nodeDesc))
			}
			if len(srcList) > 0 {
				lines = append(lines, tgLine("来源", strings.Join(srcList, "、")))
			}
			lines = append(lines, tgCodeLine("最终出口", issue.outbound))
		}

		if index < 3 {
			barkTarget := addrLabel
			if isMergedIPs {
				barkTarget = formatLimitedList(sortedIPs, 3, "个内网 IP")
			} else if issue.host != "" && issue.host != issue.destIP && !strings.HasPrefix(addrLabel, issue.host+":") {
				barkTarget = fmt.Sprintf("%s (%s)", issue.host, addrLabel)
			} else {
				barkTarget = addrLabel
			}
			var itemLines []string
			itemLines = append(itemLines, fmt.Sprintf("%d. %s [%s · %d条]", index+1, barkTarget, issue.location, issue.count))
			if multiServer && len(issue.servers) > 0 {
				itemLines = append(itemLines, "   服务器："+formatLimitedList(issue.servers, 3, "台"))
			}
			itemLines = append(itemLines, "   类型："+issue.trafficType())
			if nodeDesc != "" {
				itemLines = append(itemLines, "   节点："+nodeDesc)
			}
			if len(srcList) > 0 {
				itemLines = append(itemLines, "   来源："+strings.Join(srcList, ","))
			}
			itemLines = append(itemLines, fmt.Sprintf("   出口：%s", issue.outbound))
			barkBlocks = append(barkBlocks, strings.Join(itemLines, "\n"))
		}
	}
	if len(list) > 5 {
		lines = append(lines, tgLine("其他目标", fmt.Sprintf("另有 %d 组未展开", len(list)-5)))
	}
	if len(list) > 3 {
		barkBlocks = append(barkBlocks, fmt.Sprintf("另有 %d 组异常目标", len(list)-3))
	}

	if !multiServer {
		entry := activeNodes[0]
		nodeCtx := e.nodeContext(entry.nodeID, entry.nodeName)
		telegramText := buildTelegramAlert(severityWarning, "分流策略", "国内/局域网目标正在走代理", entry.nodeName,
			e.withNodeInfo(nodeCtx, lines), "")
		barkBody := fmt.Sprintf("发现 %d 个国内/局域网目标走代理：\n%s", totalTargets, strings.Join(barkBlocks, "\n"))
		e.sendAlert(context.Background(), alertPayload{
			Severity:  severityWarning,
			BarkTitle: fmt.Sprintf("分流异常：%s", entry.nodeName),
			BarkBody:  e.barkWithNode(nodeCtx, barkBody),
			BarkGroup: "分流策略",
			BarkSound: "horn.caf",
			Telegram:  telegramText,
		})
		return
	}

	subject := fmt.Sprintf("%d 台服务器（%s）", len(serverNames), formatLimitedList(serverNames, 5, "台"))
	telegramText := buildTelegramAlert(severityWarning, "分流策略", "国内/局域网目标正在走代理", subject, lines, "")
	barkBody := fmt.Sprintf("发现 %d 个国内/局域网目标走代理（涉及 %s）：\n%s",
		totalTargets, formatLimitedList(serverNames, 3, "台"), strings.Join(barkBlocks, "\n"))
	e.sendAlert(context.Background(), alertPayload{
		Severity:  severityWarning,
		BarkTitle: fmt.Sprintf("分流异常：%d 台服务器", len(serverNames)),
		BarkBody:  barkBody,
		BarkGroup: "分流策略",
		BarkSound: "horn.caf",
		Telegram:  telegramText,
	})
}

// NotifyNodeOffline sends an alert when a node loses contact with master.
func (e *AlertEngine) NotifyNodeOffline(nodeID, nodeName string) {
	settings, err := e.getSettings()
	if err != nil || !settings.OfflineAlertEnabled {
		return
	}
	alertKey := "offline:" + nodeID
	if e.canAlert(alertKey, settings.CooldownMinutes) {
		e.mu.Lock()
		e.offlineNodes[nodeID] = time.Now()
		e.mu.Unlock()
		nodeCtx := e.nodeContext(nodeID, nodeName)
		telegramText := buildTelegramAlertRaw(severityCritical, "服务器已离线", "", nodeName, e.withNodeInfo(nodeCtx, nil))
		e.sendAlert(context.Background(), alertPayload{
			Severity:  severityCritical,
			BarkTitle: fmt.Sprintf("服务器离线：%s", nodeName),
			BarkBody:  e.barkWithNode(nodeCtx, "控制端已失去该服务器心跳（Agent 离线）。"),
			BarkGroup: "服务器状态",
			BarkSound: "alarm.caf",
			Telegram:  telegramText,
		})
	}
}

// NotifyNodeOnline sends a recovery notice only when this node previously had
// an offline notification delivered. A reconnect without a matching offline
// alert (master restart, quick session replacement) must not look like a
// recovery out of nowhere.
func (e *AlertEngine) NotifyNodeOnline(nodeID, nodeName string) {
	e.mu.Lock()
	_, wasOffline := e.offlineNodes[nodeID]
	delete(e.offlineNodes, nodeID)
	e.mu.Unlock()
	if !wasOffline {
		return
	}

	settings, err := e.getSettings()
	if err != nil || !settings.OfflineAlertEnabled {
		return
	}
	alertKey := "online:" + nodeID
	if e.canAlert(alertKey, settings.CooldownMinutes) {
		nodeCtx := e.nodeContext(nodeID, nodeName)
		telegramText := buildTelegramAlertRaw(severityRecovery, "服务器已恢复", "", nodeName, e.withNodeInfo(nodeCtx, nil))
		e.sendAlert(context.Background(), alertPayload{
			Severity:  severityRecovery,
			BarkTitle: fmt.Sprintf("服务器恢复：%s", nodeName),
			BarkBody:  e.barkWithNode(nodeCtx, "控制通道已重建，服务已恢复正常。"),
			BarkGroup: "服务器状态",
			BarkSound: "calypso.caf",
			Telegram:  telegramText,
		})
	}
}

func (e *AlertEngine) NotifyServerExpiry(node Node, days int) {
	var barkLines []string
	barkLines = append(barkLines, fmt.Sprintf("到期 %s，剩余 %d 天", node.ExpiresOn, days))

	tgLines := make([]string, 0, 4)
	tgLines = append(tgLines,
		tgLine("到期日期", node.ExpiresOn),
		tgLine("剩余时间", fmt.Sprintf("%d 天", days)),
	)
	if node.ProviderURL != "" {
		tgLines = append(tgLines, tgLinkLine("管理入口", node.ProviderURL, "打开服务商管理后台"))
	}
	nodeCtx := e.nodeContext(node.ID, node.Name)
	telegramText := buildTelegramAlert(severityWarning, "服务器即将到期", "", node.Name, e.withNodeInfo(nodeCtx, tgLines), "")

	e.sendAlert(context.Background(), alertPayload{
		Severity:  severityWarning,
		BarkTitle: fmt.Sprintf("服务器即将到期：%s", node.Name),
		BarkBody:  e.barkWithNode(nodeCtx, strings.Join(barkLines, "\n")),
		BarkGroup: "服务器到期",
		Telegram:  telegramText,
	})
}

func (e *AlertEngine) ddnsBatchWindow() time.Duration {
	if e.ddnsWait > 0 {
		return e.ddnsWait
	}
	return defaultDDNSBatchWindow
}

func (e *AlertEngine) ensureDDNSBatchLocked(now time.Time) *ddnsAlertBatch {
	wait := e.ddnsBatchWindow()
	batch := e.ddnsBatch
	if batch == nil {
		batch = &ddnsAlertBatch{
			started: now,
			fireAt:  now.Add(wait),
		}
		batch.timer = time.AfterFunc(wait, func() { e.flushDDNSBatch(batch) })
		e.ddnsBatch = batch
		return batch
	}
	batch.fireAt = extendBoundedTimer(batch.timer, batch.started, batch.fireAt, now, wait)
	return batch
}

func (e *AlertEngine) flushDDNSBatch(target *ddnsAlertBatch) {
	e.ddnsMu.Lock()
	if e.ddnsBatch == nil || (target != nil && e.ddnsBatch != target) {
		e.ddnsMu.Unlock()
		return
	}
	batch := e.ddnsBatch
	e.ddnsBatch = nil
	if batch.timer != nil {
		batch.timer.Stop()
	}
	e.ddnsMu.Unlock()

	if len(batch.updates) == 0 && len(batch.healings) == 0 && len(batch.completions) == 0 {
		return
	}
	e.sendDDNSBatchSummary(batch)
}

// NotifyDDNSUpdated queues a DDNS IP change alert. Updates across nodes within
// a short window (along with any chained DNS refresh events) are merged into one
// summary notification instead of one push per node.
func (e *AlertEngine) NotifyDDNSUpdated(nodeID, nodeName, oldIP, currentIP, domain string) {
	settings, err := e.getSettings()
	if err != nil || !settings.HasNotifier() {
		return
	}
	alertKey := "ddns:" + nodeID + ":" + currentIP
	if !e.canAlert(alertKey, 1) {
		return
	}

	domains := splitMultiItems(domain)
	now := time.Now()

	e.ddnsMu.Lock()
	batch := e.ensureDDNSBatchLocked(now)
	var existing *ddnsUpdateEntry
	for _, u := range batch.updates {
		if u.nodeID == nodeID {
			existing = u
			break
		}
	}
	if existing == nil {
		existing = &ddnsUpdateEntry{
			nodeID:   nodeID,
			nodeName: nodeName,
			oldIP:    oldIP,
			newIP:    currentIP,
		}
		batch.updates = append(batch.updates, existing)
	} else {
		if existing.nodeName == "" {
			existing.nodeName = nodeName
		}
		if existing.oldIP == "" {
			existing.oldIP = oldIP
		}
		existing.newIP = currentIP
	}
	for _, d := range domains {
		existing.domains = appendDistinctLimit(existing.domains, d, 0)
	}
	shouldFlush := len(batch.updates)+len(batch.completions) >= maxDDNSBatchSize
	e.ddnsMu.Unlock()

	if shouldFlush {
		e.flushDDNSBatch(batch)
	}
}

// NotifyChainedDNSHealing queues an alert when chained nodes are signaled to
// refresh DNS and heal connections.
func (e *AlertEngine) NotifyChainedDNSHealing(targetNodeName string, chainedCount int, domains []string, targetIP string) {
	settings, err := e.getSettings()
	if err != nil || !settings.HasNotifier() {
		return
	}
	alertKey := "dns_healing:" + targetNodeName + ":" + targetIP
	if !e.canAlert(alertKey, 1) {
		return
	}

	now := time.Now()
	e.ddnsMu.Lock()
	batch := e.ensureDDNSBatchLocked(now)
	var existing *ddnsHealingEntry
	for _, h := range batch.healings {
		if h.targetNodeName == targetNodeName {
			existing = h
			break
		}
	}
	if existing == nil {
		existing = &ddnsHealingEntry{
			targetNodeName: targetNodeName,
			chainedCount:   chainedCount,
			targetIP:       targetIP,
		}
		batch.healings = append(batch.healings, existing)
	} else {
		if chainedCount > existing.chainedCount {
			existing.chainedCount = chainedCount
		}
		existing.targetIP = targetIP
	}
	for _, d := range domains {
		existing.domains = appendDistinctLimit(existing.domains, d, 0)
	}
	e.ddnsMu.Unlock()
}

func cleanDNSSummary(summary string) string {
	s := strings.TrimSpace(summary)
	if idx := strings.Index(s, "（"); idx != -1 {
		s = strings.TrimSpace(s[:idx])
	} else if idx := strings.Index(s, "("); idx != -1 {
		s = strings.TrimSpace(s[:idx])
	}
	if idx := strings.LastIndex(s, "："); idx != -1 {
		s = strings.TrimSpace(s[idx+len("："):])
	} else if idx := strings.LastIndex(s, ":"); idx != -1 {
		s = strings.TrimSpace(s[idx+1:])
	}
	s = strings.ReplaceAll(s, " -> ", " → ")
	return s
}

func formatDNSMappingHTML(clean string) string {
	parts := strings.Split(clean, " → ")
	if len(parts) == 2 {
		return fmt.Sprintf("<code>%s</code> → <code>%s</code>", tgEscape(parts[0]), tgEscape(parts[1]))
	}
	return tgEscape(clean)
}

// NotifyChainedDNSCompleted queues a notification when a chained node completes
// DNS refresh so multiple nodes finishing together produce one merged alert.
func (e *AlertEngine) NotifyChainedDNSCompleted(nodeID, nodeName, summary string) {
	settings, err := e.getSettings()
	if err != nil || !settings.HasNotifier() {
		return
	}
	cleanSummary := cleanDNSSummary(summary)
	alertKey := "dns_completed:" + nodeID + ":" + cleanSummary
	if !e.canAlert(alertKey, 1) {
		return
	}

	now := time.Now()
	e.ddnsMu.Lock()
	batch := e.ensureDDNSBatchLocked(now)
	var existing *ddnsCompletedEntry
	for _, c := range batch.completions {
		if c.nodeID == nodeID {
			existing = c
			break
		}
	}
	if existing == nil {
		existing = &ddnsCompletedEntry{
			nodeID:   nodeID,
			nodeName: nodeName,
		}
		batch.completions = append(batch.completions, existing)
	} else if existing.nodeName == "" {
		existing.nodeName = nodeName
	}
	existing.summaries = appendDistinctLimit(existing.summaries, cleanSummary, 0)
	shouldFlush := len(batch.updates)+len(batch.completions) >= maxDDNSBatchSize
	e.ddnsMu.Unlock()

	if shouldFlush {
		e.flushDDNSBatch(batch)
	}
}

func ddnsCompletionText(c *ddnsCompletedEntry) string {
	return strings.Join(c.summaries, "；")
}

func renderDDNSCompletions(completions []*ddnsCompletedEntry) (nodeNames []string, tgLines []string, barkLines []string) {
	sort.Slice(completions, func(i, j int) bool {
		if completions[i].nodeName != completions[j].nodeName {
			return completions[i].nodeName < completions[j].nodeName
		}
		return completions[i].nodeID < completions[j].nodeID
	})

	nodeNames = make([]string, 0, len(completions))
	var commonSummary string
	allSame := len(completions) > 0
	for i, c := range completions {
		nodeNames = append(nodeNames, c.nodeName)
		summary := ddnsCompletionText(c)
		if i == 0 {
			commonSummary = summary
		} else if summary != commonSummary {
			allSame = false
		}
	}

	if allSame && commonSummary != "" {
		tgLines = []string{
			tgLine("完成节点", formatLimitedList(nodeNames, 15, "台")),
			formatDNSMappingHTML(commonSummary),
		}
		barkLines = []string{
			fmt.Sprintf("节点：%s", formatLimitedList(nodeNames, 6, "台")),
			commonSummary,
		}
		return nodeNames, tgLines, barkLines
	}

	tgShown := len(completions)
	if tgShown > 15 {
		tgShown = 15
	}
	tgLines = make([]string, 0, tgShown+1)
	for _, c := range completions[:tgShown] {
		tgLines = append(tgLines, fmt.Sprintf("• <b>%s</b>：%s", tgEscape(c.nodeName), formatDNSMappingHTML(ddnsCompletionText(c))))
	}
	if len(completions) > tgShown {
		tgLines = append(tgLines, tgLine("其他节点", fmt.Sprintf("另有 %d 台未展开", len(completions)-tgShown)))
	}

	barkShown := len(completions)
	if barkShown > 5 {
		barkShown = 5
	}
	barkLines = make([]string, 0, barkShown+1)
	for _, c := range completions[:barkShown] {
		barkLines = append(barkLines, fmt.Sprintf("• %s：%s", c.nodeName, ddnsCompletionText(c)))
	}
	if len(completions) > barkShown {
		barkLines = append(barkLines, fmt.Sprintf("另有 %d 台服务器", len(completions)-barkShown))
	}
	return nodeNames, tgLines, barkLines
}

func (e *AlertEngine) sendDDNSBatchSummary(batch *ddnsAlertBatch) {
	settings, err := e.getSettings()
	if err != nil || !settings.HasNotifier() {
		return
	}

	// Case 1: One or more DDNS IP updates (optionally combined with chained DNS healing/completions)
	if len(batch.updates) > 0 {
		sort.Slice(batch.updates, func(i, j int) bool {
			if batch.updates[i].nodeName != batch.updates[j].nodeName {
				return batch.updates[i].nodeName < batch.updates[j].nodeName
			}
			return batch.updates[i].nodeID < batch.updates[j].nodeID
		})

		var tgLines []string
		var barkLines []string
		var subject string
		var headline string
		var barkTitle string

		if len(batch.updates) == 1 {
			u := batch.updates[0]
			subject = u.nodeName
			barkTitle = fmt.Sprintf("服务器 IP 变更：%s", u.nodeName)
			domainStr := strings.Join(u.domains, "、")
			oldStr := e.formatIPList(u.oldIP)
			newStr := e.formatIPList(u.newIP)
			if domainStr != "" {
				tgLines = append(tgLines, tgLine("解析域名", domainStr))
				barkLines = append(barkLines, domainStr)
			}
			tgLines = append(tgLines, tgLine("IP 变更", fmt.Sprintf("%s → %s", oldStr, newStr)))
			barkLines = append(barkLines, fmt.Sprintf("%s → %s", oldStr, newStr))
		} else {
			headline = fmt.Sprintf("共 %d 台服务器", len(batch.updates))
			barkTitle = fmt.Sprintf("DDNS IP 变更：%d 台服务器", len(batch.updates))

			tgShown := len(batch.updates)
			if tgShown > 15 {
				tgShown = 15
			}
			for _, u := range batch.updates[:tgShown] {
				domainStr := strings.Join(u.domains, "、")
				oldStr := e.formatIPList(u.oldIP)
				newStr := e.formatIPList(u.newIP)
				entry := fmt.Sprintf("• <b>%s</b>", tgEscape(u.nodeName))
				if domainStr != "" {
					entry += fmt.Sprintf("（<code>%s</code>）", tgEscape(domainStr))
				}
				entry += fmt.Sprintf("\n  %s → %s", tgEscape(oldStr), tgEscape(newStr))
				tgLines = append(tgLines, entry)
			}
			if len(batch.updates) > tgShown {
				tgLines = append(tgLines, tgLine("其他节点", fmt.Sprintf("另有 %d 台服务器未展开", len(batch.updates)-tgShown)))
			}

			barkShown := len(batch.updates)
			if barkShown > 6 {
				barkShown = 6
			}
			for _, u := range batch.updates[:barkShown] {
				domainStr := strings.Join(u.domains, "、")
				oldStr := e.formatIPList(u.oldIP)
				newStr := e.formatIPList(u.newIP)
				if domainStr != "" {
					barkLines = append(barkLines, fmt.Sprintf("• %s（%s）\n  %s → %s", u.nodeName, domainStr, oldStr, newStr))
				} else {
					barkLines = append(barkLines, fmt.Sprintf("• %s\n  %s → %s", u.nodeName, oldStr, newStr))
				}
			}
			if len(batch.updates) > barkShown {
				barkLines = append(barkLines, fmt.Sprintf("另有 %d 台服务器", len(batch.updates)-barkShown))
			}
		}

		if len(batch.completions) > 0 {
			completedNames, compTGLines, _ := renderDDNSCompletions(batch.completions)
			tgLines = append(tgLines, fmt.Sprintf("\n<b>前置 DNS 刷新完成（%d 台）</b>", len(batch.completions)))
			tgLines = append(tgLines, compTGLines...)
			barkLines = append(barkLines, fmt.Sprintf("前置刷新完成（%d 台）：%s",
				len(completedNames), formatLimitedList(completedNames, 5, "台")))
		} else if len(batch.healings) > 0 {
			sort.Slice(batch.healings, func(i, j int) bool {
				return batch.healings[i].targetNodeName < batch.healings[j].targetNodeName
			})
			var healingParts []string
			for _, h := range batch.healings {
				healingParts = append(healingParts, fmt.Sprintf("%s（%d 台前置）", h.targetNodeName, h.chainedCount))
			}
			tgLines = append(tgLines, "\n<b>前置 DNS 刷新</b>", tgLine("触发范围", formatLimitedList(healingParts, 10, "个")))
			barkLines = append(barkLines, "已触发前置刷新："+formatLimitedList(healingParts, 4, "个"))
		}

		var barkBody string
		if len(batch.updates) == 1 {
			nodeCtx := e.nodeContext(batch.updates[0].nodeID, batch.updates[0].nodeName)
			tgLines = e.withNodeInfo(nodeCtx, tgLines)
			barkBody = e.barkWithNode(nodeCtx, strings.Join(barkLines, "\n"))
		} else {
			barkBody = strings.Join(barkLines, "\n")
		}

		telegramText := buildTelegramAlert(severityInfo, "DDNS IP 变更", headline, subject, tgLines, "")
		e.sendAlert(context.Background(), alertPayload{
			Severity:  severityInfo,
			BarkTitle: barkTitle,
			BarkBody:  barkBody,
			BarkGroup: "DDNS监控",
			BarkSound: "bell.caf",
			Telegram:  telegramText,
		})
		return
	}

	// Case 2: Chained DNS refresh completions (single or merged across nodes)
	if len(batch.completions) > 0 {
		if len(batch.completions) == 1 && len(batch.healings) == 0 {
			c := batch.completions[0]
			summary := ddnsCompletionText(c)
			nodeCtx := e.nodeContext(c.nodeID, c.nodeName)
			dnsLine := formatDNSMappingHTML(summary)
			telegramText := buildTelegramAlertRaw(severityRecovery, "DNS 刷新完成", "", c.nodeName, e.withNodeInfo(nodeCtx, []string{dnsLine}))
			e.sendAlert(context.Background(), alertPayload{
				Severity:  severityRecovery,
				BarkTitle: fmt.Sprintf("DNS 刷新完成：%s", c.nodeName),
				BarkBody:  e.barkWithNode(nodeCtx, summary),
				BarkGroup: "DNS",
				BarkSound: "calypso.caf",
				Telegram:  telegramText,
			})
			return
		}

		_, compTGLines, compBarkLines := renderDDNSCompletions(batch.completions)
		headline := fmt.Sprintf("共 %d 台服务器", len(batch.completions))
		if len(batch.completions) == 1 {
			headline = batch.completions[0].nodeName
		}
		if len(batch.healings) > 0 {
			var allDomains []string
			for _, h := range batch.healings {
				for _, d := range h.domains {
					allDomains = appendDistinctLimit(allDomains, d, 0)
				}
			}
			if len(allDomains) > 0 {
				compTGLines = append(compTGLines, tgLine("涉及域名", formatLimitedList(allDomains, 8, "个")))
			}
		}

		telegramText := buildTelegramAlertRaw(severityRecovery, "DNS 刷新完成", headline, "", compTGLines)
		barkTitle := fmt.Sprintf("DNS 刷新完成：%d 台服务器", len(batch.completions))
		if len(batch.completions) == 1 {
			barkTitle = fmt.Sprintf("DNS 刷新完成：%s", batch.completions[0].nodeName)
		}
		e.sendAlert(context.Background(), alertPayload{
			Severity:  severityRecovery,
			BarkTitle: barkTitle,
			BarkBody:  strings.Join(compBarkLines, "\n"),
			BarkGroup: "DNS",
			BarkSound: "calypso.caf",
			Telegram:  telegramText,
		})
		return
	}

	// Case 3: Chained DNS healing triggers only
	if len(batch.healings) > 0 {
		sort.Slice(batch.healings, func(i, j int) bool {
			return batch.healings[i].targetNodeName < batch.healings[j].targetNodeName
		})
		if len(batch.healings) == 1 {
			h := batch.healings[0]
			nodeCtx := nodeAlertContext{Name: h.targetNodeName, IP: cleanIPAddress(h.targetIP)}
			if nodeCtx.IP != "" && e.locator != nil {
				if location := strings.TrimSpace(e.locator.Locate(nodeCtx.IP)); location != "" && location != "未知" {
					nodeCtx.Location = location
				}
			}
			telegramText := buildTelegramAlert(severityWarning, "触发前置 DNS 刷新", "", h.targetNodeName, e.withNodeInfo(nodeCtx, []string{
				tgLine("影响域名", strings.Join(h.domains, "、")),
				tgLine("目标最新 IP", e.formatIP(h.targetIP)),
				tgLine("依赖前置服务器", fmt.Sprintf("%d 台", h.chainedCount)),
			}), "")
			e.sendAlert(context.Background(), alertPayload{
				Severity:  severityWarning,
				BarkTitle: "前置 DNS 刷新已触发",
				BarkBody: e.barkWithNode(nodeCtx, fmt.Sprintf("%d 台前置服务器正在刷新 DNS\n域名：%s",
					h.chainedCount, strings.Join(h.domains, "、"))),
				BarkGroup: "DNS",
				BarkSound: "anticipate.caf",
				Telegram:  telegramText,
			})
			return
		}

		var tgLines []string
		var barkLines []string
		for i, h := range batch.healings {
			if i < 10 {
				tgLines = append(tgLines, fmt.Sprintf("• <b>%s</b>（%d 台前置）→ %s\n  域名：<code>%s</code>",
					tgEscape(h.targetNodeName), h.chainedCount, tgEscape(e.formatIP(h.targetIP)), tgEscape(strings.Join(h.domains, "、"))))
			}
			if i < 5 {
				barkLines = append(barkLines, fmt.Sprintf("• %s（%d 台前置）：%s", h.targetNodeName, h.chainedCount, strings.Join(h.domains, "、")))
			}
		}
		if len(batch.healings) > 10 {
			tgLines = append(tgLines, tgLine("其他目标", fmt.Sprintf("另有 %d 台未展开", len(batch.healings)-10)))
		}
		if len(batch.healings) > 5 {
			barkLines = append(barkLines, fmt.Sprintf("另有 %d 个目标节点", len(batch.healings)-5))
		}
		telegramText := buildTelegramAlert(severityWarning, "触发前置 DNS 刷新", fmt.Sprintf("共 %d 个目标节点", len(batch.healings)), "", tgLines, "")
		e.sendAlert(context.Background(), alertPayload{
			Severity:  severityWarning,
			BarkTitle: fmt.Sprintf("前置 DNS 刷新已触发：%d 个目标", len(batch.healings)),
			BarkBody:  strings.Join(barkLines, "\n"),
			BarkGroup: "DNS",
			BarkSound: "anticipate.caf",
			Telegram:  telegramText,
		})
	}
}

// NotifyFail2BanBlock records a blocked attacker. Blocks are merged per node
// and jail for a short window so a burst of SSH brute-force attempts produces
// one summary notification instead of one push per source IP.
func (e *AlertEngine) NotifyFail2BanBlock(nodeID, nodeName, ip, jail string) {
	settings, err := e.getSettings()
	if err != nil || !settings.HasNotifier() {
		return
	}
	cooldown := settings.CooldownMinutes
	if cooldown < 5 {
		cooldown = 5
	}
	if !e.canAlert("f2b:"+nodeID+":"+ip, cooldown) {
		return
	}

	key := nodeID + "|" + jail
	e.fail2banMu.Lock()
	batch := e.fail2banBatch[key]
	if batch == nil {
		batch = &fail2banBatch{
			key:      key,
			nodeID:   nodeID,
			nodeName: nodeName,
			jail:     jail,
			seen:     make(map[string]struct{}),
			started:  time.Now(),
		}
		e.fail2banBatch[key] = batch
		wait := e.fail2banWait
		if wait <= 0 {
			wait = defaultFail2banWindow
		}
		batch.timer = time.AfterFunc(wait, func() { e.flushFail2banBatch(key) })
	}
	if _, exists := batch.seen[ip]; !exists {
		batch.seen[ip] = struct{}{}
		batch.ips = append(batch.ips, ip)
	}
	shouldFlush := len(batch.ips) >= maxFail2banBatchSize
	e.fail2banMu.Unlock()

	if shouldFlush {
		e.flushFail2banBatch(key)
	}
}

func (e *AlertEngine) flushFail2banBatch(key string) {
	e.fail2banMu.Lock()
	batch := e.fail2banBatch[key]
	if batch == nil {
		e.fail2banMu.Unlock()
		return
	}
	delete(e.fail2banBatch, key)
	if batch.timer != nil {
		batch.timer.Stop()
	}
	e.fail2banMu.Unlock()

	if len(batch.ips) == 0 {
		return
	}
	e.sendFail2banSummary(batch)
}

func (e *AlertEngine) sendFail2banSummary(batch *fail2banBatch) {
	settings, err := e.getSettings()
	if err != nil || !settings.HasNotifier() {
		return
	}
	cooldown := settings.CooldownMinutes
	if cooldown < 5 {
		cooldown = 5
	}
	if !e.canAlert("f2b_batch:"+batch.nodeID+":"+batch.jail, cooldown) {
		return
	}

	ips := append([]string(nil), batch.ips...)
	sort.Strings(ips)
	total := len(ips)
	shown := ips
	if len(shown) > 10 {
		shown = shown[:10]
	}
	formatted := make([]string, 0, len(shown))
	for _, ip := range shown {
		formatted = append(formatted, e.formatIP(ip))
	}
	more := total - len(shown)
	ipList := strings.Join(formatted, "、")
	if more > 0 {
		ipList += fmt.Sprintf(" 等，共 %d 个", total)
	}

	start := FormatAlertTime(batch.started)
	end := FormatAlertTime(time.Now())
	windowSeconds := int(time.Since(batch.started).Seconds())
	if windowSeconds < 1 {
		windowSeconds = 1
	}

	nodeCtx := e.nodeContext(batch.nodeID, batch.nodeName)
	if total == 1 {
		telegramText := buildTelegramAlert(severityWarning, "安全防护", "已拦截恶意来源", batch.nodeName, e.withNodeInfo(nodeCtx, []string{
			tgLine("拦截目标", e.formatIP(batch.ips[0])),
			tgLine("触发规则", batch.jail),
		}), "")
		e.sendAlert(context.Background(), alertPayload{
			Severity:  severityWarning,
			BarkTitle: fmt.Sprintf("已拦截恶意源：%s", batch.nodeName),
			BarkBody:  e.barkWithNode(nodeCtx, fmt.Sprintf("%s\n规则：%s", e.formatIP(batch.ips[0]), batch.jail)),
			BarkGroup: "防御日志",
			BarkSound: "chime.caf",
			Telegram:  telegramText,
		})
		return
	}

	telegramText := buildTelegramAlert(severityWarning, "安全防护", "批量拦截恶意来源", batch.nodeName, e.withNodeInfo(nodeCtx, []string{
		tgLine("拦截数量", fmt.Sprintf("%d 个", total)),
		tgLine("触发规则", batch.jail),
		tgLine("统计窗口", fmt.Sprintf("%s 至 %s，约 %d 秒", start, end, windowSeconds)),
		tgLine("来源列表", ipList),
	}), "")
	e.sendAlert(context.Background(), alertPayload{
		Severity:  severityWarning,
		BarkTitle: fmt.Sprintf("已拦截 %d 个恶意来源：%s", total, batch.nodeName),
		BarkBody: e.barkWithNode(nodeCtx, fmt.Sprintf("规则：%s\n统计窗口：约 %d 秒\n来源：%s",
			batch.jail, windowSeconds, ipList)),
		BarkGroup: "防御日志",
		BarkSound: "chime.caf",
		Telegram:  telegramText,
	})
}

// NotifyConsoleBruteForce alerts on repeated failed login attempts.
func (e *AlertEngine) NotifyConsoleBruteForce(clientIP string, attempts int) {
	alertKey := "bruteforce:" + clientIP
	if e.canAlert(alertKey, 15) {
		telegramText := buildTelegramAlert(severityCritical, "系统安全", "控制台防爆破触发", "", []string{
			tgLine("登录来源", e.formatIP(clientIP)),
			tgLine("失败次数", fmt.Sprintf("15 分钟内连续失败 %d 次", attempts)),
		}, "")
		e.sendAlert(context.Background(), alertPayload{
			Severity:  severityCritical,
			BarkTitle: "控制台防爆破触发",
			BarkBody:  fmt.Sprintf("%s\n15 分钟内连续失败 %d 次", e.formatIP(clientIP), attempts),
			BarkGroup: "系统安全",
			BarkSound: "alarm.caf",
			Telegram:  telegramText,
		})
	}
}

// NotifyLoginFailed notifies on operator login failure.
func (e *AlertEngine) NotifyLoginFailed(username, ip, reason, userAgent string) {
	settings, err := e.getSettings()
	if err != nil || !settings.HasNotifier() {
		return
	}
	clean := strings.TrimSpace(ip)
	if h, _, err := net.SplitHostPort(clean); err == nil {
		clean = h
	}

	e.mu.Lock()
	now := time.Now()
	for k, t := range e.loginFailAt {
		if now.Sub(t) > 15*time.Minute {
			delete(e.loginFailAt, k)
			delete(e.loginFailures, k)
		}
	}
	e.loginFailures[clean]++
	e.loginFailAt[clean] = now
	failCount := e.loginFailures[clean]
	e.mu.Unlock()

	if failCount >= 5 {
		e.NotifyConsoleBruteForce(clean, failCount)
		return
	}

	alertKey := "login_fail:" + clean
	if e.canAlert(alertKey, 2) {
		ua := truncateText(userAgent, 60)
		if ua == "" {
			ua = "未知客户端"
		}
		if username == "" {
			username = "未知用户"
		}
		if reason == "" {
			reason = "用户名或密码错误"
		}
		telegramText := buildTelegramAlert(severityWarning, "系统安全", "控制台登录失败", username, []string{
			tgLine("登录来源", e.formatIP(clean)),
			tgLine("失败原因", reason),
			tgLine("客户端", ua),
		}, "")
		e.sendAlert(context.Background(), alertPayload{
			Severity:  severityWarning,
			BarkTitle: fmt.Sprintf("控制台登录失败：%s", username),
			BarkBody:  fmt.Sprintf("%s\n%s\n客户端：%s", e.formatIP(clean), reason, ua),
			BarkGroup: "系统安全",
			BarkSound: "horn.caf",
			Telegram:  telegramText,
		})
	}
}

// NotifySubscriptionPullSuccess notifies when a client fetches subscription profile.
func (e *AlertEngine) NotifySubscriptionPullSuccess(configName, ip, userAgent string) {
	settings, err := e.getSettings()
	if err != nil || !settings.HasNotifier() {
		return
	}
	clean := strings.TrimSpace(ip)
	if h, _, err := net.SplitHostPort(clean); err == nil {
		clean = h
	}
	cooldown := settings.CooldownMinutes
	if cooldown < 5 {
		cooldown = 5
	}
	alertKey := fmt.Sprintf("sub_ok:%s:%s", configName, clean)
	if e.canAlert(alertKey, cooldown) {
		ua := truncateText(userAgent, 60)
		if ua == "" {
			ua = "未知客户端"
		}
		telegramText := buildTelegramAlert(severityInfo, "订阅分发", "订阅下载成功", configName, []string{
			tgLine("请求来源", e.formatIP(clean)),
			tgLine("客户端", ua),
		}, "")
		e.sendAlert(context.Background(), alertPayload{
			Severity:  severityInfo,
			BarkTitle: fmt.Sprintf("订阅下载成功：%s", configName),
			BarkBody:  fmt.Sprintf("%s\n客户端：%s", e.formatIP(clean), ua),
			BarkGroup: "订阅分发",
			BarkSound: "glass.caf",
			Telegram:  telegramText,
		})
	}
}

// NotifySubscriptionPullFailed notifies on subscription download failure.
func (e *AlertEngine) NotifySubscriptionPullFailed(tokenHint, reason, ip, userAgent string) {
	settings, err := e.getSettings()
	if err != nil || !settings.HasNotifier() {
		return
	}
	clean := strings.TrimSpace(ip)
	if h, _, err := net.SplitHostPort(clean); err == nil {
		clean = h
	}
	alertKey := fmt.Sprintf("sub_err:%s", clean)
	if e.canAlert(alertKey, 3) {
		ua := truncateText(userAgent, 60)
		if ua == "" {
			ua = "未知客户端"
		}
		if len(tokenHint) > 16 {
			tokenHint = tokenHint[:8] + "..." + tokenHint[len(tokenHint)-4:]
		}
		telegramText := buildTelegramAlert(severityWarning, "订阅分发", "订阅下载失败", tokenHint, []string{
			tgLine("请求来源", e.formatIP(clean)),
			tgLine("客户端", ua),
			tgLine("失败原因", reason),
		}, "")
		e.sendAlert(context.Background(), alertPayload{
			Severity:  severityWarning,
			BarkTitle: fmt.Sprintf("订阅下载失败：%s", tokenHint),
			BarkBody:  fmt.Sprintf("%s\n%s", e.formatIP(clean), reason),
			BarkGroup: "订阅分发",
			BarkSound: "horn.caf",
			Telegram:  telegramText,
		})
	}
}

// NotifySubscriptionConfigChanged tells the operator that a resource change
// will be visible in one or more client subscriptions. It is only sent when at
// least one client configuration is affected.
func (e *AlertEngine) NotifySubscriptionConfigChanged(resource, action, name, detail string, affected []string) {
	settings, err := e.getSettings()
	if err != nil || !settings.HasNotifier() {
		return
	}
	affected = deduplicateStrings(affected)
	if len(affected) == 0 {
		return
	}
	sort.Strings(affected)
	if !e.canAlert("subscription_change:"+resource+":"+name+":"+action, 1) {
		return
	}

	shown := affected
	extra := 0
	if len(shown) > 5 {
		extra = len(shown) - 5
		shown = shown[:5]
	}
	list := strings.Join(shown, "、")
	if extra > 0 {
		list += fmt.Sprintf(" 等，共 %d 个", len(affected))
	}

	barkLines := []string{
		fmt.Sprintf("变更：%s%s", resource, action),
		fmt.Sprintf("影响客户端：%d 个", len(affected)),
		list,
	}
	if strings.TrimSpace(detail) != "" {
		barkLines = append(barkLines, detail)
	}

	tgShown := affected
	if len(tgShown) > 15 {
		tgShown = tgShown[:15]
	}
	tgList := strings.Join(tgShown, "、")
	if len(tgShown) < len(affected) {
		tgList += fmt.Sprintf(" 等，共 %d 个", len(affected))
	}
	tgLines := []string{
		tgLine("名称", name),
		tgLine("变更类型", resource+action),
		tgLine("影响客户端", fmt.Sprintf("%d 个", len(affected))),
		tgLine("影响列表", tgList),
	}
	if strings.TrimSpace(detail) != "" {
		tgLines = append(tgLines, tgLine("变更内容", detail))
	}
	telegramText := buildTelegramAlertFor("订阅通知", severityInfo, "订阅配置", resource+action, name, tgLines, "")

	e.sendAlert(context.Background(), alertPayload{
		Severity:  severityInfo,
		BarkTitle: fmt.Sprintf("订阅配置已更新：%s", name),
		BarkBody:  strings.Join(barkLines, "\n"),
		BarkGroup: "订阅配置",
		BarkSound: "glass.caf",
		Telegram:  telegramText,
	})
}
