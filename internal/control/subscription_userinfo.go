package control

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// formatSubscriptionUserInfo formats the standard Subscription-Userinfo header:
// upload=<bytes>; download=<bytes>; total=<bytes>; expire=<unix_timestamp>
func formatSubscriptionUserInfo(upload, download, total, expire int64) string {
	parts := make([]string, 0, 4)
	if upload < 0 {
		upload = 0
	}
	if download < 0 {
		download = 0
	}
	parts = append(parts, fmt.Sprintf("upload=%d", upload), fmt.Sprintf("download=%d", download))
	if total > 0 {
		parts = append(parts, fmt.Sprintf("total=%d", total))
	}
	if expire > 0 {
		parts = append(parts, fmt.Sprintf("expire=%d", expire))
	}
	return strings.Join(parts, "; ")
}

var trafficPlanPattern = regexp.MustCompile(`(?i)^\s*([0-9]+(?:\.[0-9]+)?)\s*(TB|GB|MB|KB|B)?\s*$`)

// parseTrafficPlanBytes converts a human string like "2 TB", "500 GB", "100 MB" or "2000" into bytes.
func parseTrafficPlanBytes(plan string) int64 {
	plan = strings.TrimSpace(plan)
	if plan == "" {
		return 0
	}
	matches := trafficPlanPattern.FindStringSubmatch(plan)
	if len(matches) < 2 {
		return 0
	}
	val, err := strconv.ParseFloat(matches[1], 64)
	if err != nil || val <= 0 {
		return 0
	}
	unit := ""
	if len(matches) >= 3 {
		unit = strings.ToUpper(matches[2])
	}
	switch unit {
	case "TB":
		return int64(val * 1024 * 1024 * 1024 * 1024)
	case "GB":
		return int64(val * 1024 * 1024 * 1024)
	case "MB":
		return int64(val * 1024 * 1024)
	case "KB":
		return int64(val * 1024)
	case "B":
		return int64(val)
	default:
		if val >= 1_000_000_000 {
			return int64(val)
		}
		return int64(val * 1024 * 1024 * 1024)
	}
}

// EndpointsTrafficStats computes cumulative upload/download bytes from connection_records
// for the given endpoint IDs, along with the total quota (sum of distinct node traffic plans).
func (s *Store) EndpointsTrafficStats(ctx context.Context, endpointIDs []string) (upload int64, download int64, total int64, err error) {
	if len(endpointIDs) == 0 {
		return 0, 0, 0, nil
	}

	placeholders := strings.Repeat("?,", len(endpointIDs))
	placeholders = placeholders[:len(placeholders)-1]
	query := fmt.Sprintf(`SELECT e.name, e.alias, COALESCE(n.id, ''), COALESCE(n.traffic_plan, '')
		FROM endpoints e
		LEFT JOIN listeners l ON l.id = e.listener_id
		LEFT JOIN nodes n ON n.id = l.node_id
		WHERE e.id IN (%s)`, placeholders)

	args := make([]any, len(endpointIDs))
	for i, id := range endpointIDs {
		args[i] = id
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("query endpoint traffic users: %w", err)
	}
	defer rows.Close()

	userSet := make(map[string]struct{})
	nodePlans := make(map[string]string)
	for rows.Next() {
		var name, alias, nodeID, plan string
		if err := rows.Scan(&name, &alias, &nodeID, &plan); err != nil {
			return 0, 0, 0, err
		}
		if name = strings.TrimSpace(name); name != "" {
			userSet[name] = struct{}{}
		}
		if alias = strings.TrimSpace(alias); alias != "" {
			userSet[alias] = struct{}{}
		}
		if nodeID = strings.TrimSpace(nodeID); nodeID != "" && strings.TrimSpace(plan) != "" {
			nodePlans[nodeID] = strings.TrimSpace(plan)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, 0, 0, err
	}

	for _, plan := range nodePlans {
		total += parseTrafficPlanBytes(plan)
	}

	if len(userSet) == 0 {
		return 0, 0, total, nil
	}

	userArgs := make([]any, 0, len(userSet))
	for u := range userSet {
		userArgs = append(userArgs, u)
	}
	userPlaceholders := strings.Repeat("?,", len(userArgs))
	userPlaceholders = userPlaceholders[:len(userPlaceholders)-1]

	connQuery := fmt.Sprintf(`SELECT COALESCE(SUM(upload), 0), COALESCE(SUM(download), 0)
		FROM connection_records
		WHERE user IN (%s)`, userPlaceholders)

	err = s.db.QueryRowContext(ctx, connQuery, userArgs...).Scan(&upload, &download)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, 0, total, fmt.Errorf("sum connection_records traffic: %w", err)
	}
	return upload, download, total, nil
}

// MihomoClientConfigTraffic computes the upload, download, and total quota for a Mihomo client config.
func (s *Store) MihomoClientConfigTraffic(ctx context.Context, configID string) (upload int64, download int64, total int64, err error) {
	config, err := s.mihomoClientConfigByID(ctx, configID)
	if err != nil {
		return 0, 0, 0, err
	}
	groups, err := s.resolveMihomoProxyGroups(ctx, config.ProxyGroupIDs)
	if err != nil {
		return 0, 0, 0, err
	}
	endpointIDMap := make(map[string]struct{})
	for _, group := range groups {
		for _, member := range group.Members {
			if member.Kind == "endpoint" && member.ID != "" {
				endpointIDMap[member.ID] = struct{}{}
			}
		}
	}
	for _, id := range config.EndpointIDs {
		if id != "" {
			endpointIDMap[id] = struct{}{}
		}
	}
	endpointIDs := make([]string, 0, len(endpointIDMap))
	for id := range endpointIDMap {
		endpointIDs = append(endpointIDs, id)
	}

	return s.EndpointsTrafficStats(ctx, endpointIDs)
}

func sanitizeASCIIFilename(name, fallback string) string {
	ascii := strings.Trim(strings.Map(func(r rune) rune {
		if r == '-' || r == '_' || r == '.' || (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			return r
		}
		return '-'
	}, name), "-")
	for strings.Contains(ascii, "--") {
		ascii = strings.ReplaceAll(ascii, "--", "-")
	}
	if ascii == "" {
		return fallback
	}
	return ascii
}

func requestWebURL(r *http.Request) string {
	if r == nil {
		return ""
	}
	host := r.Host
	if host == "" {
		return ""
	}
	scheme := "http"
	if r.TLS != nil ||
		strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") ||
		strings.EqualFold(r.Header.Get("CF-Visitor"), `{"scheme":"https"}`) {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/", scheme, host)
}
