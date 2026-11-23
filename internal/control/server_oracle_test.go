package control

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServer_OCIAccountsAndIPRotation(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	server, err := NewServer(store, false)
	if err != nil {
		t.Fatal(err)
	}

	admin, _, err := store.createOperator(context.Background(), "admin-oci", "password123456", "admin", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.createSession(context.Background(), admin.ID, admin.Role, false)
	if err != nil {
		t.Fatal(err)
	}
	addAuth := func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session.Token})
		r.Header.Set("X-CSRF-Token", session.CSRFToken)
	}

	privKey, pemStr := generateTestRSAKey(t)
	pubKey := &privKey.PublicKey
	deletedOldIP := false

	// Mock OCI Server
	mockOCI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		verifyOCISignature(t, pubKey, r, nil)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/20160918/regions":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"key":"NRT","name":"ap-tokyo-1"}]`))

		case r.Method == http.MethodGet && r.URL.Path == "/20160918/vnics/ocid1.vnic.srv1":
			_ = json.NewEncoder(w).Encode(OCIVNIC{
				ID:            "ocid1.vnic.srv1",
				CompartmentID: "ocid1.compartment.srv1",
				PublicIP:      "140.238.1.1",
				IsPrimary:     true,
			})

		case r.Method == http.MethodGet && r.URL.Path == "/20160918/privateIps":
			_ = json.NewEncoder(w).Encode([]OCIPrivateIP{
				{
					ID:        "ocid1.privateip.srv1",
					IPAddress: "10.0.0.10",
					VnicID:    "ocid1.vnic.srv1",
					IsPrimary: true,
				},
			})

		case r.Method == http.MethodPost && r.URL.Path == "/20160918/publicIps/actions/getByPrivateIpId":
			if deletedOldIP {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(OCIPublicIP{
				ID:             "ocid1.publicip.old",
				IPAddress:      "140.238.1.1",
				Lifetime:       "EPHEMERAL",
				LifecycleState: "ASSIGNED",
			})

		case r.Method == http.MethodDelete && r.URL.Path == "/20160918/publicIps/ocid1.publicip.old":
			deletedOldIP = true
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodPost && r.URL.Path == "/20160918/publicIps":
			_ = json.NewEncoder(w).Encode(OCIPublicIP{
				ID:             "ocid1.publicip.new",
				IPAddress:      "129.153.2.2",
				Lifetime:       "EPHEMERAL",
				LifecycleState: "ASSIGNED",
			})

		default:
			http.NotFound(w, r)
		}
	}))
	defer mockOCI.Close()

	handler := server.BrowserHandler()

	// 1. Create OCI Account 1 via POST /api/v1/oci/accounts
	acc1Body := map[string]any{
		"name":        "主力甲骨文账号",
		"tenancy_id":  "ocid1.tenancy.global",
		"user_id":     "ocid1.user.global",
		"fingerprint": "11:22:33:44",
		"region":      "ap-tokyo-1",
		"private_key": pemStr,
		"is_default":  true,
		"enabled":     true,
	}
	acc1JSON, _ := json.Marshal(acc1Body)
	reqAcc1 := httptest.NewRequest(http.MethodPost, "/api/v1/oci/accounts", bytes.NewReader(acc1JSON))
	addAuth(reqAcc1)
	recAcc1 := httptest.NewRecorder()
	handler.ServeHTTP(recAcc1, reqAcc1)

	if recAcc1.Code != http.StatusOK {
		t.Fatalf("POST /api/v1/oci/accounts failed (%d): %s", recAcc1.Code, recAcc1.Body.String())
	}
	var createdAcc1 OCIAccount
	if err := json.Unmarshal(recAcc1.Body.Bytes(), &createdAcc1); err != nil {
		t.Fatal(err)
	}
	if !createdAcc1.IsDefault || createdAcc1.ID == "" {
		t.Fatalf("unexpected createdAcc1: %+v", createdAcc1)
	}

	// 2. Create OCI Account 2 (Frankfurt)
	acc2Body := map[string]any{
		"name":        "欧洲备用账号",
		"tenancy_id":  "ocid1.tenancy.eu",
		"user_id":     "ocid1.user.eu",
		"fingerprint": "55:66:77:88",
		"region":      "eu-frankfurt-1",
		"private_key": pemStr,
		"is_default":  false,
	}
	acc2JSON, _ := json.Marshal(acc2Body)
	reqAcc2 := httptest.NewRequest(http.MethodPost, "/api/v1/oci/accounts", bytes.NewReader(acc2JSON))
	addAuth(reqAcc2)
	recAcc2 := httptest.NewRecorder()
	handler.ServeHTTP(recAcc2, reqAcc2)
	if recAcc2.Code != http.StatusOK {
		t.Fatalf("POST /api/v1/oci/accounts 2 failed (%d): %s", recAcc2.Code, recAcc2.Body.String())
	}
	var createdAcc2 OCIAccount
	if err := json.Unmarshal(recAcc2.Body.Bytes(), &createdAcc2); err != nil {
		t.Fatal(err)
	}

	// 2.1 Switch default to Account 2 via POST /api/v1/oci/accounts/{id}/default
	reqSetDef := httptest.NewRequest(http.MethodPost, "/api/v1/oci/accounts/"+createdAcc2.ID+"/default", nil)
	addAuth(reqSetDef)
	recSetDef := httptest.NewRecorder()
	handler.ServeHTTP(recSetDef, reqSetDef)
	if recSetDef.Code != http.StatusOK {
		t.Fatalf("POST /api/v1/oci/accounts/{id}/default failed (%d): %s", recSetDef.Code, recSetDef.Body.String())
	}

	// Switch back to Account 1
	reqSetDef1 := httptest.NewRequest(http.MethodPost, "/api/v1/oci/accounts/"+createdAcc1.ID+"/default", nil)
	addAuth(reqSetDef1)
	recSetDef1 := httptest.NewRecorder()
	handler.ServeHTTP(recSetDef1, reqSetDef1)
	if recSetDef1.Code != http.StatusOK {
		t.Fatalf("POST /api/v1/oci/accounts/{id}/default 1 failed (%d): %s", recSetDef1.Code, recSetDef1.Body.String())
	}

	// 3. List Accounts via GET /api/v1/oci/accounts
	reqList := httptest.NewRequest(http.MethodGet, "/api/v1/oci/accounts", nil)
	addAuth(reqList)
	recList := httptest.NewRecorder()
	handler.ServeHTTP(recList, reqList)

	if recList.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/oci/accounts failed (%d): %s", recList.Code, recList.Body.String())
	}
	var listResp struct {
		Accounts []OCIAccount `json:"accounts"`
	}
	if err := json.Unmarshal(recList.Body.Bytes(), &listResp); err != nil {
		t.Fatal(err)
	}
	if len(listResp.Accounts) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(listResp.Accounts))
	}
	if !strings.Contains(listResp.Accounts[0].PrivateKeyMasked, "configured") {
		t.Fatalf("expected masked private key, got %q", listResp.Accounts[0].PrivateKeyMasked)
	}

	// 3.1 Verify POST /api/v1/oci/accounts/test route works (not 404)
	testPayload := map[string]string{
		"tenancy_id":  "invalid",
		"user_id":     "invalid",
		"fingerprint": "invalid",
		"region":      "ap-singapore-1",
		"private_key": "not-a-valid-key",
	}
	testJSON, _ := json.Marshal(testPayload)
	reqTestRoute := httptest.NewRequest(http.MethodPost, "/api/v1/oci/accounts/test", bytes.NewReader(testJSON))
	addAuth(reqTestRoute)
	recTestRoute := httptest.NewRecorder()
	handler.ServeHTTP(recTestRoute, reqTestRoute)
	if recTestRoute.Code == http.StatusNotFound {
		t.Fatalf("POST /api/v1/oci/accounts/test returned 404 Not Found")
	}

	// 3.2 Verify POST /api/v1/oci/accounts/{id}/test route works (not 404)
	reqTestIDRoute := httptest.NewRequest(http.MethodPost, "/api/v1/oci/accounts/nonexistent-id/test", bytes.NewReader([]byte("{}")))
	addAuth(reqTestIDRoute)
	recTestIDRoute := httptest.NewRecorder()
	handler.ServeHTTP(recTestIDRoute, reqTestIDRoute)
	if recTestIDRoute.Code == http.StatusNotFound {
		t.Fatalf("POST /api/v1/oci/accounts/{id}/test returned 404 Not Found")
	}

	// 4. Create a test Node in DB
	nodeID := "node-oracle-multi-test-1"
	_, err = store.db.Exec(`INSERT INTO nodes (id, name, public_key, client_address, last_observed_ip, provider, created_at) VALUES (?, ?, ?, ?, ?, ?, unixepoch())`,
		nodeID, "OracleNode1", []byte("32-byte-public-key-for-test-node!"), "140.238.1.1", "140.238.1.1", "Oracle Cloud")
	if err != nil {
		t.Fatal(err)
	}

	// 5. Update Node OCI configuration via PUT /api/v1/nodes/{id}/oci bound to Account 1
	nodeOCIBody := map[string]string{
		"account_id": createdAcc1.ID,
		"vnic_id":    "ocid1.vnic.srv1",
		"region":     "ap-tokyo-1",
	}
	nodeOCIJSON, _ := json.Marshal(nodeOCIBody)
	reqNodeOCI := httptest.NewRequest(http.MethodPut, "/api/v1/nodes/"+nodeID+"/oci", bytes.NewReader(nodeOCIJSON))
	addAuth(reqNodeOCI)
	recNodeOCI := httptest.NewRecorder()
	handler.ServeHTTP(recNodeOCI, reqNodeOCI)

	if recNodeOCI.Code != http.StatusOK {
		t.Fatalf("PUT /api/v1/nodes/{id}/oci failed (%d): %s", recNodeOCI.Code, recNodeOCI.Body.String())
	}

	// Verify node loaded
	n, err := store.GetNode(context.Background(), nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if n.OCIAccountID != createdAcc1.ID || n.OCIVnicID != "ocid1.vnic.srv1" {
		t.Fatalf("unexpected node OCI binding: %+v", n)
	}

	// 6. Test rotation client directly
	cfg, err := store.LoadOCIAccountConfig(context.Background(), n.OCIAccountID)
	if err != nil {
		t.Fatalf("LoadOCIAccountConfig: %v", err)
	}
	client, err := NewOCIClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	client.SetBaseURL(mockOCI.URL)

	oldIP, newIP, err := client.RotateEphemeralIP(context.Background(), "ocid1.compartment.srv1", "ocid1.privateip.srv1")
	if err != nil {
		t.Fatalf("RotateEphemeralIP: %v", err)
	}
	if oldIP != "140.238.1.1" || newIP != "129.153.2.2" {
		t.Fatalf("unexpected rotation: old=%s, new=%s", oldIP, newIP)
	}
}
