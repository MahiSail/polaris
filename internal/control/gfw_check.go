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
// Returns blocked = true ONLY when both probe nodes are functioning properly and prove that
// the target IP is reachable overseas but blocked in mainland China.
// If any third-party probe node itself fails (network timeout, DNS error, 5xx), it fails open (blocked = false)
// to prevent discarding valid IPs.
func CheckGFWBlocked(ctx context.Context, ip string, port int) (blocked bool, reason string, err error) {
	if port <= 0 {
		port = 22
	}

	// Step 1: TCP Port differential test
	domStatus, domErr := queryGFWPortCheck(ctx, GFWCheckDomesticBaseURL, ip, port)
	overseasStatus, overseasErr := queryGFWPortCheck(ctx, GFWCheckOverseasBaseURL, ip, port)

	log.Printf("[GFWCheck] TCP port %d check for %s: domestic=%s (err: %v), overseas=%s (err: %v)",
		port, ip, domStatus, domErr, overseasStatus, overseasErr)

	// CRITICAL: If the domestic probe service itself fails (DNS failure, network timeout, HTTP 5xx),
	// this indicates the probe node is down, NOT that the target IP is blocked.
	// We MUST NOT mark the IP as blocked, otherwise valid IPs would be discarded in a loop.
	if domErr != nil {
		return false, "Domestic probe node unavailable", fmt.Errorf("domestic probe port check failed: %w", domErr)
	}

	// If overseas probe failed, we cannot establish a reliable reachability baseline.
	if overseasErr != nil {
		return false, "Overseas probe node unavailable", fmt.Errorf("overseas probe port check failed: %w", overseasErr)
	}

	// At this point, both probe API calls succeeded (HTTP 200).
	// We can now safely evaluate the target IP reachability:
	if overseasStatus == "open" {
		if domStatus == "open" {
			return false, "TCP port reachable from mainland China", nil
		}
		// Overseas is open, but mainland China probe explicitly received connection timeout (filtered) or reset (closed).
		return true, fmt.Sprintf("TCP port %d open overseas but blocked/filtered in mainland China (%s)", port, domStatus), nil
	}

	// Step 2: Fall back to ICMP Ping differential test ONLY if port 22 is not open overseas
	// (e.g. instance SSH is on another port or firewall blocks port 22 globally).
	domAlive, domLoss, domPingErr := queryGFWPingCheck(ctx, GFWCheckDomesticBaseURL, ip)
	if domPingErr != nil {
		return false, "Domestic probe ping unavailable", fmt.Errorf("domestic probe ping check failed: %w", domPingErr)
	}

	overseasAlive, _, overseasPingErr := queryGFWPingCheck(ctx, GFWCheckOverseasBaseURL, ip)
	if overseasPingErr != nil {
		return false, "Overseas probe ping unavailable", fmt.Errorf("overseas probe ping check failed: %w", overseasPingErr)
	}

	log.Printf("[GFWCheck] ICMP ping check for %s: domestic alive=%v loss=%d%%, overseas alive=%v",
		ip, domAlive, domLoss, overseasAlive)

	if overseasAlive && (!domAlive || domLoss == 100) {
		return true, "ICMP ping responsive overseas but 100% packet loss in mainland China", nil
	}

	return false, "Not blocked (or port closed globally)", nil
}
