package control

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCheckGFWBlocked_Scenarios(t *testing.T) {
	tests := []struct {
		name           string
		domesticStatus string
		overseasStatus string
		domesticAlive  bool
		domesticLoss   int
		overseasAlive  bool
		expectedBlock  bool
	}{
		{
			name:           "Clean IP - Both open",
			domesticStatus: "open",
			overseasStatus: "open",
			expectedBlock:  false,
		},
		{
			name:           "GFW Blocked - Overseas open, domestic filtered",
			domesticStatus: "filtered",
			overseasStatus: "open",
			expectedBlock:  true,
		},
		{
			name:           "GFW Blocked - Overseas open, domestic closed",
			domesticStatus: "closed",
			overseasStatus: "open",
			expectedBlock:  true,
		},
		{
			name:           "Fallback Ping - Overseas alive, domestic 100% loss",
			domesticStatus: "closed",
			overseasStatus: "closed",
			domesticAlive:  false,
			domesticLoss:   100,
			overseasAlive:  true,
			expectedBlock:  true,
		},
		{
			name:           "Globally Closed - Neither reachable",
			domesticStatus: "closed",
			overseasStatus: "closed",
			domesticAlive:  false,
			domesticLoss:   100,
			overseasAlive:  false,
			expectedBlock:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			domServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/api/v2/port/check" {
					_ = json.NewEncoder(w).Encode(gfwPortCheckResp{
						Code: 200,
						Msg:  "ok",
						Data: &struct {
							Host    string `json:"host"`
							Port    int    `json:"port"`
							Status  string `json:"status"`
							Elapsed int    `json:"elapsed"`
						}{
							Host:   "1.2.3.4",
							Port:   22,
							Status: tc.domesticStatus,
						},
					})
					return
				}
				if r.URL.Path == "/api/v2/ping/check" {
					_ = json.NewEncoder(w).Encode(gfwPingCheckResp{
						Code: 200,
						Msg:  "ok",
						Data: &struct {
							IP       string `json:"ip"`
							Alive    bool   `json:"alive"`
							Sent     int    `json:"sent"`
							Received int    `json:"received"`
							Loss     int    `json:"loss"`
						}{
							IP:    "1.2.3.4",
							Alive: tc.domesticAlive,
							Loss:  tc.domesticLoss,
						},
					})
					return
				}
			}))
			defer domServer.Close()

			overseasServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/api/v2/port/check" {
					_ = json.NewEncoder(w).Encode(gfwPortCheckResp{
						Code: 200,
						Msg:  "ok",
						Data: &struct {
							Host    string `json:"host"`
							Port    int    `json:"port"`
							Status  string `json:"status"`
							Elapsed int    `json:"elapsed"`
						}{
							Host:   "1.2.3.4",
							Port:   22,
							Status: tc.overseasStatus,
						},
					})
					return
				}
				if r.URL.Path == "/api/v2/ping/check" {
					_ = json.NewEncoder(w).Encode(gfwPingCheckResp{
						Code: 200,
						Msg:  "ok",
						Data: &struct {
							IP       string `json:"ip"`
							Alive    bool   `json:"alive"`
							Sent     int    `json:"sent"`
							Received int    `json:"received"`
							Loss     int    `json:"loss"`
						}{
							IP:    "1.2.3.4",
							Alive: tc.overseasAlive,
							Loss:  0,
						},
					})
					return
				}
			}))
			defer overseasServer.Close()

			// Temporarily redirect probe URLs
			origDom := GFWCheckDomesticBaseURL
			origOverseas := GFWCheckOverseasBaseURL
			origClient := GFWCheckHTTPClient
			defer func() {
				GFWCheckDomesticBaseURL = origDom
				GFWCheckOverseasBaseURL = origOverseas
				GFWCheckHTTPClient = origClient
			}()

			GFWCheckDomesticBaseURL = domServer.URL
			GFWCheckOverseasBaseURL = overseasServer.URL
			GFWCheckHTTPClient = domServer.Client()

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			blocked, reason, err := CheckGFWBlocked(ctx, "1.2.3.4", 22)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if blocked != tc.expectedBlock {
				t.Errorf("expected blocked=%v, got %v (reason: %s)", tc.expectedBlock, blocked, reason)
			}
		})
	}
}

func TestOCIClient_RotateEphemeralIP_GFWBlockedRotation(t *testing.T) {
	privKey, pemStr := generateTestRSAKey(t)
	pubKey := &privKey.PublicKey

	// Setup mock domestic server:
	// IP 140.245.1.1 is filtered (GFW blocked in China)
	// IP 140.245.2.2 is open (Clean in China)
	domServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var portReq gfwPortCheckReq
		_ = json.NewDecoder(r.Body).Decode(&portReq)

		status := "open"
		if portReq.Host == "140.245.1.1" {
			status = "filtered"
		}

		_ = json.NewEncoder(w).Encode(gfwPortCheckResp{
			Code: 200,
			Msg:  "ok",
			Data: &struct {
				Host    string `json:"host"`
				Port    int    `json:"port"`
				Status  string `json:"status"`
				Elapsed int    `json:"elapsed"`
			}{
				Host:   portReq.Host,
				Port:   portReq.Port,
				Status: status,
			},
		})
	}))
	defer domServer.Close()

	// Setup mock overseas server:
	// Both 140.245.1.1 and 140.245.2.2 are open overseas
	overseasServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var portReq gfwPortCheckReq
		_ = json.NewDecoder(r.Body).Decode(&portReq)

		_ = json.NewEncoder(w).Encode(gfwPortCheckResp{
			Code: 200,
			Msg:  "ok",
			Data: &struct {
				Host    string `json:"host"`
				Port    int    `json:"port"`
				Status  string `json:"status"`
				Elapsed int    `json:"elapsed"`
			}{
				Host:   portReq.Host,
				Port:   portReq.Port,
				Status: "open",
			},
		})
	}))
	defer overseasServer.Close()

	origDom := GFWCheckDomesticBaseURL
	origOverseas := GFWCheckOverseasBaseURL
	origClient := GFWCheckHTTPClient
	defer func() {
		GFWCheckDomesticBaseURL = origDom
		GFWCheckOverseasBaseURL = origOverseas
		GFWCheckHTTPClient = origClient
	}()

	GFWCheckDomesticBaseURL = domServer.URL
	GFWCheckOverseasBaseURL = overseasServer.URL
	GFWCheckHTTPClient = domServer.Client()

	createRound := 0
	deletedBlockedIP := false
	ipAssigned := true

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		verifyOCISignature(t, pubKey, r, nil)

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/20160918/publicIps/actions/getByPrivateIpId":
			w.Header().Set("Content-Type", "application/json")
			if !ipAssigned {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(OCIPublicIP{
				ID:        "ocid1.publicip.test.old",
				IPAddress: "140.238.10.10",
			})

		case r.Method == http.MethodDelete && r.URL.Path == "/20160918/publicIps/ocid1.publicip.test.old":
			ipAssigned = false
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodDelete && r.URL.Path == "/20160918/publicIps/ocid1.publicip.test.blocked":
			deletedBlockedIP = true
			ipAssigned = false
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodPost && r.URL.Path == "/20160918/publicIps":
			createRound++
			ipAssigned = true
			w.Header().Set("Content-Type", "application/json")
			if createRound == 1 {
				// Round 1: Returns 140.245.1.1 (clean subnet, but blocked by GFW)
				_ = json.NewEncoder(w).Encode(OCIPublicIP{
					ID:        "ocid1.publicip.test.blocked",
					IPAddress: "140.245.1.1",
				})
				return
			}
			// Round 2: Returns 140.245.2.2 (clean subnet and unblocked)
			_ = json.NewEncoder(w).Encode(OCIPublicIP{
				ID:        "ocid1.publicip.test.unblocked",
				IPAddress: "140.245.2.2",
			})
		}
	}))
	defer ts.Close()

	cfg := OCIConfig{
		TenancyID:     "ocid1.tenancy.test",
		UserID:        "ocid1.user.test",
		Fingerprint:   "aa:bb:cc:dd",
		Region:        "ap-singapore-1",
		PrivateKeyPEM: pemStr,
	}

	client, err := NewOCIClient(cfg)
	if err != nil {
		t.Fatalf("NewOCIClient: %v", err)
	}
	client.SetBaseURL(ts.URL)

	ctx := context.Background()
	oldIP, newIP, err := client.RotateEphemeralIP(ctx, "ocid1.compartment.test", "ocid1.privateip.test")
	if err != nil {
		t.Fatalf("RotateEphemeralIP failed: %v", err)
	}

	if oldIP != "140.238.10.10" {
		t.Errorf("expected old IP 140.238.10.10, got %s", oldIP)
	}
	if newIP != "140.245.2.2" {
		t.Errorf("expected new IP 140.245.2.2, got %s", newIP)
	}
	if !deletedBlockedIP {
		t.Error("expected GFW-blocked IP 140.245.1.1 to be deleted and released")
	}
}

type roundTripperFunc func(req *http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}


func TestOCIClient_RotateEphemeralIP_UserOptions(t *testing.T) {
	privKey, pemStr := generateTestRSAKey(t)
	pubKey := &privKey.PublicKey

	// Dom server returns filtered, overseas returns open -> GFW blocked
	domServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(gfwPortCheckResp{
			Code: 200,
			Msg:  "ok",
			Data: &struct {
				Host    string `json:"host"`
				Port    int    `json:"port"`
				Status  string `json:"status"`
				Elapsed int    `json:"elapsed"`
			}{
				Host:   "140.245.1.1",
				Port:   22,
				Status: "filtered",
			},
		})
	}))
	defer domServer.Close()

	overseasServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(gfwPortCheckResp{
			Code: 200,
			Msg:  "ok",
			Data: &struct {
				Host    string `json:"host"`
				Port    int    `json:"port"`
				Status  string `json:"status"`
				Elapsed int    `json:"elapsed"`
			}{
				Host:   "140.245.1.1",
				Port:   22,
				Status: "open",
			},
		})
	}))
	defer overseasServer.Close()

	origDom := GFWCheckDomesticBaseURL
	origOverseas := GFWCheckOverseasBaseURL
	origClient := GFWCheckHTTPClient
	defer func() {
		GFWCheckDomesticBaseURL = origDom
		GFWCheckOverseasBaseURL = origOverseas
		GFWCheckHTTPClient = origClient
	}()

	GFWCheckDomesticBaseURL = domServer.URL
	GFWCheckOverseasBaseURL = overseasServer.URL
	GFWCheckHTTPClient = domServer.Client()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		verifyOCISignature(t, pubKey, r, nil)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/20160918/publicIps/actions/getByPrivateIpId":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/20160918/publicIps":
			w.Header().Set("Content-Type", "application/json")
			// Return 168.138.1.1 (an excluded subnet)
			_ = json.NewEncoder(w).Encode(OCIPublicIP{
				ID:        "ocid1.publicip.test.polluted",
				IPAddress: "168.138.1.1",
			})
		}
	}))
	defer ts.Close()

	cfg := OCIConfig{
		TenancyID:     "ocid1.tenancy.test",
		UserID:        "ocid1.user.test",
		Fingerprint:   "aa:bb:cc:dd",
		Region:        "ap-singapore-1",
		PrivateKeyPEM: pemStr,
	}

	client, err := NewOCIClient(cfg)
	if err != nil {
		t.Fatalf("NewOCIClient: %v", err)
	}
	client.SetBaseURL(ts.URL)

	ctx := context.Background()

	// Case 1: ExcludeSubnets=false -> 168.138.1.1 is accepted immediately without filtering
	_, newIP, err := client.RotateEphemeralIPWithOptions(ctx, "ocid1.comp.1", "ocid1.priv.1", RotateEphemeralIPOptions{
		ExcludeSubnets: false,
		CheckGFW:       false,
		MaxRounds:      1,
	})
	if err != nil {
		t.Fatalf("RotateEphemeralIPWithOptions: %v", err)
	}
	if newIP != "168.138.1.1" {
		t.Errorf("expected 168.138.1.1 when ExcludeSubnets=false, got %s", newIP)
	}
}
