package control

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func generateTestRSAKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	return key, string(pemBytes)
}

func verifyOCISignature(t *testing.T, pubKey *rsa.PublicKey, req *http.Request, bodyBytes []byte) {
	t.Helper()
	auth := req.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Signature ") {
		t.Fatalf("expected Signature authorization header, got: %q", auth)
	}

	reHeaders := regexp.MustCompile(`headers="([^"]+)"`)
	reSig := regexp.MustCompile(`signature="([^"]+)"`)

	headersMatch := reHeaders.FindStringSubmatch(auth)
	sigMatch := reSig.FindStringSubmatch(auth)
	if len(headersMatch) < 2 || len(sigMatch) < 2 {
		t.Fatalf("failed to parse authorization header: %q", auth)
	}

	signedHeaderNames := strings.Split(headersMatch[1], " ")
	var parts []string
	for _, name := range signedHeaderNames {
		switch name {
		case "(request-target)":
			target := fmt.Sprintf("(request-target): %s %s", strings.ToLower(req.Method), req.URL.RequestURI())
			parts = append(parts, target)
		case "host":
			parts = append(parts, fmt.Sprintf("host: %s", req.Host))
		case "date":
			parts = append(parts, fmt.Sprintf("date: %s", req.Header.Get("Date")))
		case "content-length":
			parts = append(parts, fmt.Sprintf("content-length: %s", req.Header.Get("Content-Length")))
		case "content-type":
			parts = append(parts, fmt.Sprintf("content-type: %s", req.Header.Get("Content-Type")))
		case "x-content-sha256":
			parts = append(parts, fmt.Sprintf("x-content-sha256: %s", req.Header.Get("x-content-sha256")))
		default:
			t.Fatalf("unknown signed header: %s", name)
		}
	}

	signingString := strings.Join(parts, "\n")
	sigBytes, err := base64.StdEncoding.DecodeString(sigMatch[1])
	if err != nil {
		t.Fatalf("decode signature base64: %v", err)
	}

	h := sha256.Sum256([]byte(signingString))
	if err := rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, h[:], sigBytes); err != nil {
		t.Fatalf("rsa signature verification failed: %v", err)
	}
}

func TestOCIClient_RequestSigningAndRotation(t *testing.T) {
	privKey, pemStr := generateTestRSAKey(t)
	pubKey := &privKey.PublicKey

	deletedOldIP := false
	createdNewIP := false

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		verifyOCISignature(t, pubKey, r, nil)

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/20160918/regions":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"key":"NRT","name":"ap-tokyo-1"}]`))

		case r.Method == http.MethodGet && r.URL.Path == "/20160918/publicIps/actions/getPublicIpByPrivateIpId":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(OCIPublicIP{
				ID:             "ocid1.publicip.test.old",
				IPAddress:      "140.238.10.10",
				Lifetime:       "EPHEMERAL",
				LifecycleState: "ASSIGNED",
			})

		case r.Method == http.MethodDelete && r.URL.Path == "/20160918/publicIps/ocid1.publicip.test.old":
			deletedOldIP = true
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodPost && r.URL.Path == "/20160918/publicIps":
			createdNewIP = true
			var payload map[string]string
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if payload["lifetime"] != "EPHEMERAL" {
				t.Errorf("expected lifetime EPHEMERAL, got %s", payload["lifetime"])
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(OCIPublicIP{
				ID:             "ocid1.publicip.test.new",
				IPAddress:      "129.153.20.20",
				Lifetime:       "EPHEMERAL",
				LifecycleState: "ASSIGNED",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/20160918/instances":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"id":"ocid1.instance.1","displayName":"vm-tokyo-1","lifecycleState":"RUNNING","region":"ap-tokyo-1","shape":"VM.Standard.A1.Flex","compartmentId":"ocid1.tenancy.test"}]`))

		case r.Method == http.MethodGet && r.URL.Path == "/20160918/vnicAttachments":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"id":"ocid1.att.1","instanceId":"ocid1.instance.1","vnicId":"ocid1.vnic.test.1","isPrimary":true}]`))

		case r.Method == http.MethodGet && r.URL.Path == "/20160918/vnics/ocid1.vnic.test.1":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"ocid1.vnic.test.1","compartmentId":"ocid1.tenancy.test","publicIp":"140.238.10.10","isPrimary":true}`))

		case r.Method == http.MethodGet && r.URL.Path == "/20160918/privateIps":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]OCIPrivateIP{
				{
					ID:        "ocid1.privateip.test.primary",
					IPAddress: "10.0.0.5",
					VnicID:    "ocid1.vnic.test.1",
					IsPrimary: true,
				},
			})

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	cfg := OCIConfig{
		TenancyID:     "ocid1.tenancy.test",
		UserID:        "ocid1.user.test",
		Fingerprint:   "aa:bb:cc:dd",
		Region:        "ap-tokyo-1",
		PrivateKeyPEM: pemStr,
	}

	client, err := NewOCIClient(cfg)
	if err != nil {
		t.Fatalf("NewOCIClient: %v", err)
	}
	client.SetBaseURL(ts.URL)

	ctx := context.Background()

	// 1. Test connection
	if err := client.TestConnection(ctx); err != nil {
		t.Fatalf("TestConnection: %v", err)
	}

	// 2. Test GetPrimaryPrivateIP
	privIP, err := client.GetPrimaryPrivateIP(ctx, "ocid1.vnic.test.1")
	if err != nil {
		t.Fatalf("GetPrimaryPrivateIP: %v", err)
	}
	if privIP.ID != "ocid1.privateip.test.primary" || !privIP.IsPrimary {
		t.Fatalf("unexpected primary private IP: %+v", privIP)
	}

	// 3. Test RotateEphemeralIP
	oldIP, newIP, err := client.RotateEphemeralIP(ctx, "ocid1.compartment.test", privIP.ID)
	if err != nil {
		t.Fatalf("RotateEphemeralIP: %v", err)
	}
	if oldIP != "140.238.10.10" {
		t.Errorf("expected old IP 140.238.10.10, got %s", oldIP)
	}
	if newIP != "129.153.20.20" {
		t.Errorf("expected new IP 129.153.20.20, got %s", newIP)
	}
	if !deletedOldIP {
		t.Error("expected old IP to be deleted")
	}
	if !createdNewIP {
		t.Error("expected new IP to be created")
	}

	// 4. Test ListInstances
	instances, err := client.ListInstances(ctx)
	if err != nil {
		t.Fatalf("ListInstances: %v", err)
	}
	if len(instances) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(instances))
	}
	if instances[0].DisplayName != "vm-tokyo-1" || instances[0].PublicIP != "140.238.10.10" || instances[0].VnicID != "ocid1.vnic.test.1" {
		t.Fatalf("unexpected instance data: %+v", instances[0])
	}
}
