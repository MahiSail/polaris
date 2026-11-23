package control

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

var (
	// GFWCheckDomesticBaseURL is the base URL for the mainland China probe node (Anhui/Jiangsu Telecom).
	GFWCheckDomesticBaseURL = "https://tcp.png.pub"
	// GFWCheckOverseasBaseURL is the base URL for the overseas probe node.
	GFWCheckOverseasBaseURL = "https://tcp.lc"
	// GFWCheckHTTPClient is the HTTP client used for querying probe APIs.
	GFWCheckHTTPClient = &http.Client{Timeout: 8 * time.Second}
)

type gfwPortCheckReq struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type gfwPortCheckResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data *struct {
		Host    string `json:"host"`
		Port    int    `json:"port"`
		Status  string `json:"status"` // "open", "filtered", "closed"
		Elapsed int    `json:"elapsed"`
	} `json:"data"`
}

type gfwPingCheckReq struct {
	IP string `json:"ip"`
}

type gfwPingCheckResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data *struct {
		IP       string `json:"ip"`
		Alive    bool   `json:"alive"`
		Sent     int    `json:"sent"`
		Received int    `json:"received"`
		Loss     int    `json:"loss"`
	} `json:"data"`
}

func queryGFWPortCheck(ctx context.Context, baseURL, host string, port int) (string, error) {
	reqBody, err := json.Marshal(gfwPortCheckReq{Host: host, Port: port})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/v2/port/check", bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := GFWCheckHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var parsed gfwPortCheckResp
	if err := json.Unmarshal(bodyBytes, &parsed); err != nil {
		return "", fmt.Errorf("unmarshal port check response: %w", err)
	}

	if parsed.Code != 200 || parsed.Data == nil {
		return "", fmt.Errorf("api error code %d: %s", parsed.Code, parsed.Msg)
	}

	return parsed.Data.Status, nil
}

func queryGFWPingCheck(ctx context.Context, baseURL, ip string) (alive bool, loss int, err error) {
	reqBody, err := json.Marshal(gfwPingCheckReq{IP: ip})
	if err != nil {
		return false, 100, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/v2/ping/check", bytes.NewReader(reqBody))
	if err != nil {
		return false, 100, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := GFWCheckHTTPClient.Do(req)
	if err != nil {
		return false, 100, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, 100, err
	}

	var parsed gfwPingCheckResp
	if err := json.Unmarshal(bodyBytes, &parsed); err != nil {
		return false, 100, fmt.Errorf("unmarshal ping check response: %w", err)
	}

	if parsed.Code != 200 || parsed.Data == nil {
		return false, 100, fmt.Errorf("api error code %d: %s", parsed.Code, parsed.Msg)
	}

	return parsed.Data.Alive, parsed.Data.Loss, nil
}

// CheckGFWBlocked checks whether the given IP is blocked by China's Great Firewall (GFW)
// using differential testing between mainland China probe and overseas probe.
// Default port is 22 (SSH).
// Returns blocked = true if the IP is blocked in China, false otherwise.
func CheckGFWBlocked(ctx context.Context, ip string, port int) (blocked bool, reason string, err error) {
	if port <= 0 {
		port = 22
	}

	// Step 1: TCP Port differential test
	domStatus, domErr := queryGFWPortCheck(ctx, GFWCheckDomesticBaseURL, ip, port)
	overseasStatus, overseasErr := queryGFWPortCheck(ctx, GFWCheckOverseasBaseURL, ip, port)

	log.Printf("[GFWCheck] TCP port %d check for %s: domestic=%s (err: %v), overseas=%s (err: %v)",
		port, ip, domStatus, domErr, overseasStatus, overseasErr)

	if overseasErr == nil && overseasStatus == "open" {
		if domErr == nil && domStatus == "open" {
			return false, "TCP port reachable from mainland China", nil
		}
		if domErr == nil && domStatus != "open" {
			return true, fmt.Sprintf("TCP port %d open overseas but blocked/filtered in mainland China (%s)", port, domStatus), nil
		}
		// If domestic probe request itself failed (network/DNS/HTTP error), do not falsely assume the target IP is blocked.
		// Instead, fall through to ICMP ping check or report detection warning.
	}

	// Step 2: Fall back to ICMP Ping differential test if port is not globally open
	domAlive, domLoss, domPingErr := queryGFWPingCheck(ctx, GFWCheckDomesticBaseURL, ip)
	overseasAlive, _, overseasPingErr := queryGFWPingCheck(ctx, GFWCheckOverseasBaseURL, ip)

	log.Printf("[GFWCheck] ICMP ping check for %s: domestic alive=%v loss=%d%% (err: %v), overseas alive=%v (err: %v)",
		ip, domAlive, domLoss, domPingErr, overseasAlive, overseasPingErr)

	if overseasPingErr == nil && overseasAlive {
		if domPingErr == nil && (!domAlive || domLoss == 100) {
			return true, fmt.Sprintf("ICMP ping responsive overseas but 100%% packet loss in mainland China"), nil
		}
	}

	if domErr != nil && domPingErr != nil {
		return false, "Domestic probe node unavailable", fmt.Errorf("domestic probe failed (port check: %v, ping check: %v)", domErr, domPingErr)
	}

	return false, "Not blocked (or port closed globally)", nil
}
