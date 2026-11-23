package control

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OCIConfig holds API credentials and target region for Oracle Cloud Infrastructure.
type OCIConfig struct {
	TenancyID     string `json:"tenancy_id"`
	UserID        string `json:"user_id"`
	Fingerprint   string `json:"fingerprint"`
	Region        string `json:"region"`
	PrivateKeyPEM string `json:"private_key_pem,omitempty"`
}

// OCIPublicIP represents an OCI Public IP object.
type OCIPublicIP struct {
	ID             string `json:"id"`
	IPAddress      string `json:"ipAddress"`
	Lifetime       string `json:"lifetime"`
	LifecycleState string `json:"lifecycleState"`
	PrivateIPID    string `json:"privateIpId,omitempty"`
	CompartmentID  string `json:"compartmentId,omitempty"`
}

// OCIPrivateIP represents an OCI Private IP object assigned to a VNIC.
type OCIPrivateIP struct {
	ID        string `json:"id"`
	IPAddress string `json:"ipAddress"`
	VnicID    string `json:"vnicId"`
	SubnetID  string `json:"subnetId"`
	IsPrimary bool   `json:"isPrimary"`
}

// OCIVNIC represents an OCI Virtual Network Interface Card.
type OCIVNIC struct {
	ID            string `json:"id"`
	CompartmentID string `json:"compartmentId"`
	PublicIP      string `json:"publicIp"`
	IsPrimary     bool   `json:"isPrimary"`
}

// OCIInstance represents an OCI Compute Instance with attached VNIC and IP info.
type OCIInstance struct {
	ID             string `json:"id"`
	DisplayName    string `json:"name"`
	LifecycleState string `json:"state"`
	Shape          string `json:"shape"`
	PublicIP       string `json:"public_ip"`
	VnicID         string `json:"vnic_id"`
	CompartmentID  string `json:"compartment_id"`
	Region         string `json:"region"`
}

// OCIClient is a lightweight, zero-dependency client that speaks directly to the OCI REST API.
type OCIClient struct {
	cfg        OCIConfig
	privateKey *rsa.PrivateKey
	httpClient *http.Client
	baseURL    string
}

// NewOCIClient initializes an OCIClient with parsed RSA private key and verified region.
func NewOCIClient(cfg OCIConfig) (*OCIClient, error) {
	if strings.TrimSpace(cfg.TenancyID) == "" {
		return nil, errors.New("OCI tenancy ID is required")
	}
	if strings.TrimSpace(cfg.UserID) == "" {
		return nil, errors.New("OCI user ID is required")
	}
	if strings.TrimSpace(cfg.Fingerprint) == "" {
		return nil, errors.New("OCI fingerprint is required")
	}
	if strings.TrimSpace(cfg.Region) == "" {
		return nil, errors.New("OCI region is required")
	}
	if strings.TrimSpace(cfg.PrivateKeyPEM) == "" {
		return nil, errors.New("OCI private key PEM is required")
	}

	block, _ := pem.Decode([]byte(cfg.PrivateKeyPEM))
	if block == nil {
		return nil, errors.New("failed to parse OCI private key PEM (no valid PEM block found)")
	}

	var rsaKey *rsa.PrivateKey
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if k, ok := key.(*rsa.PrivateKey); ok {
			rsaKey = k
		}
	}
	if rsaKey == nil {
		if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
			rsaKey = k
		}
	}
	if rsaKey == nil {
		return nil, errors.New("OCI private key must be a valid RSA private key in PKCS#1 or PKCS#8 format")
	}

	region := strings.ToLower(strings.TrimSpace(cfg.Region))
	baseURL := fmt.Sprintf("https://iaas.%s.oraclecloud.com", region)

	return &OCIClient{
		cfg:        cfg,
		privateKey: rsaKey,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    baseURL,
	}, nil
}

// SetBaseURL overrides the base URL (primarily used for unit testing).
func (c *OCIClient) SetBaseURL(url string) {
	c.baseURL = strings.TrimRight(url, "/")
}

// signRequest signs the HTTP request adhering to OCI Request Signatures specification.
func (c *OCIClient) signRequest(req *http.Request, bodyBytes []byte) error {
	dateStr := time.Now().UTC().Format(http.TimeFormat)
	req.Header.Set("Date", dateStr)

	target := fmt.Sprintf("%s %s", strings.ToLower(req.Method), req.URL.RequestURI())
	host := req.URL.Host

	var signingString string
	var signedHeaders string

	if len(bodyBytes) > 0 {
		hash := sha256.Sum256(bodyBytes)
		shaBase64 := base64.StdEncoding.EncodeToString(hash[:])
		req.Header.Set("x-content-sha256", shaBase64)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Content-Length", fmt.Sprintf("%d", len(bodyBytes)))

		signedHeaders = "(request-target) host date content-length content-type x-content-sha256"
		signingString = fmt.Sprintf("(request-target): %s\nhost: %s\ndate: %s\ncontent-length: %d\ncontent-type: application/json\nx-content-sha256: %s",
			target, host, dateStr, len(bodyBytes), shaBase64)
	} else {
		signedHeaders = "(request-target) host date"
		signingString = fmt.Sprintf("(request-target): %s\nhost: %s\ndate: %s", target, host, dateStr)
	}

	h := sha256.Sum256([]byte(signingString))
	sigBytes, err := rsa.SignPKCS1v15(rand.Reader, c.privateKey, crypto.SHA256, h[:])
	if err != nil {
		return fmt.Errorf("sign string: %w", err)
	}
	sigBase64 := base64.StdEncoding.EncodeToString(sigBytes)

	keyID := fmt.Sprintf("%s/%s/%s", strings.TrimSpace(c.cfg.TenancyID), strings.TrimSpace(c.cfg.UserID), strings.TrimSpace(c.cfg.Fingerprint))
	authHeader := fmt.Sprintf(`Signature version="1",keyId="%s",algorithm="rsa-sha256",headers="%s",signature="%s"`,
		keyID, signedHeaders, sigBase64)

	req.Header.Set("Authorization", authHeader)
	return nil
}

func (c *OCIClient) do(ctx context.Context, method, path string, bodyData any) (*http.Response, []byte, error) {
	var bodyBytes []byte
	var bodyReader io.Reader
	if bodyData != nil {
		b, err := json.Marshal(bodyData)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal body: %w", err)
		}
		bodyBytes = b
		bodyReader = bytes.NewReader(b)
	}

	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, nil, fmt.Errorf("create request: %w", err)
	}

	if err := c.signRequest(req, bodyBytes); err != nil {
		return nil, nil, fmt.Errorf("sign request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("execute request (%s %s): %w", method, path, err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("read response: %w", err)
	}

	return resp, respBytes, nil
}

// TestConnection verifies credentials by querying the OCI regions endpoint on the Identity service.
func (c *OCIClient) TestConnection(ctx context.Context) error {
	region := strings.ToLower(strings.TrimSpace(c.cfg.Region))
	baseURL := fmt.Sprintf("https://identity.%s.oraclecloud.com", region)
	if !strings.HasPrefix(c.baseURL, "https://iaas.") {
		baseURL = c.baseURL
	}

	url := baseURL + "/20160918/regions"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create test request: %w", err)
	}

	if err := c.signRequest(req, nil); err != nil {
		return fmt.Errorf("sign test request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("connect to OCI identity: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("OCI API returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// GetPublicIPByPrivateIP retrieves the Public IP object assigned to the specified private IP OCID.
// If no public IP is attached, it returns (nil, nil).
func (c *OCIClient) GetPublicIPByPrivateIP(ctx context.Context, privateIPID string) (*OCIPublicIP, error) {
	path := fmt.Sprintf("/20160918/publicIps/actions/getPublicIpByPrivateIpId?privateIpId=%s", privateIPID)
	resp, body, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("OCI get public ip (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var ip OCIPublicIP
	if err := json.Unmarshal(body, &ip); err != nil {
		return nil, fmt.Errorf("unmarshal public ip: %w", err)
	}
	return &ip, nil
}

// DeletePublicIP unassigns and releases a public IP back to the Oracle pool.
func (c *OCIClient) DeletePublicIP(ctx context.Context, publicIPID string) error {
	path := fmt.Sprintf("/20160918/publicIps/%s", publicIPID)
	resp, body, err := c.do(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK {
		return nil
	}
	return fmt.Errorf("OCI delete public ip (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
}

// CreateEphemeralPublicIP allocates and immediately maps a new ephemeral public IPv4 to the primary private IP.
func (c *OCIClient) CreateEphemeralPublicIP(ctx context.Context, compartmentID, privateIPID, displayName string) (*OCIPublicIP, error) {
	if displayName == "" {
		displayName = "polaris-ephemeral-ip"
	}
	payload := map[string]string{
		"compartmentId": compartmentID,
		"lifetime":      "EPHEMERAL",
		"privateIpId":   privateIPID,
		"displayName":   displayName,
	}

	resp, body, err := c.do(ctx, http.MethodPost, "/20160918/publicIps", payload)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("OCI create public ip (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var newIP OCIPublicIP
	if err := json.Unmarshal(body, &newIP); err != nil {
		return nil, fmt.Errorf("unmarshal new public ip: %w", err)
	}
	return &newIP, nil
}

// ListPrivateIPs queries all private IPs assigned to a VNIC.
func (c *OCIClient) ListPrivateIPs(ctx context.Context, vnicID string) ([]OCIPrivateIP, error) {
	path := fmt.Sprintf("/20160918/privateIps?vnicId=%s", vnicID)
	resp, body, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("OCI list private ips (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var list []OCIPrivateIP
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("unmarshal private ips: %w", err)
	}
	return list, nil
}

// GetPrimaryPrivateIP finds the primary private IP on a VNIC.
func (c *OCIClient) GetPrimaryPrivateIP(ctx context.Context, vnicID string) (*OCIPrivateIP, error) {
	ips, err := c.ListPrivateIPs(ctx, vnicID)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		if ip.IsPrimary {
			return &ip, nil
		}
	}
	if len(ips) > 0 {
		return &ips[0], nil
	}
	return nil, fmt.Errorf("no private IPs found on VNIC %s", vnicID)
}

// GetVNIC retrieves VNIC metadata including compartmentId and current public IP.
func (c *OCIClient) GetVNIC(ctx context.Context, vnicID string) (*OCIVNIC, error) {
	path := fmt.Sprintf("/20160918/vnics/%s", vnicID)
	resp, body, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("OCI get vnic (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var vnic OCIVNIC
	if err := json.Unmarshal(body, &vnic); err != nil {
		return nil, fmt.Errorf("unmarshal vnic: %w", err)
	}
	return &vnic, nil
}

// RotateEphemeralIP executes the full atomic sequence:
// 1. Look up existing public IP.
// 2. Delete/release the existing IP.
// 3. Request a brand new Ephemeral Public IP from the Oracle pool.
func (c *OCIClient) RotateEphemeralIP(ctx context.Context, compartmentID, privateIPID string) (oldIP, newIP string, err error) {
	existing, err := c.GetPublicIPByPrivateIP(ctx, privateIPID)
	if err != nil {
		return "", "", fmt.Errorf("query existing public IP: %w", err)
	}

	if existing != nil && existing.ID != "" {
		oldIP = existing.IPAddress
		if err := c.DeletePublicIP(ctx, existing.ID); err != nil {
			return oldIP, "", fmt.Errorf("delete old public IP (%s): %w", existing.ID, err)
		}
	}

	// Immediate re-creation of ephemeral public IP
	created, err := c.CreateEphemeralPublicIP(ctx, compartmentID, privateIPID, "polaris-rotated-ip")
	if err != nil {
		return oldIP, "", fmt.Errorf("allocate new ephemeral public IP: %w", err)
	}

	return oldIP, created.IPAddress, nil
}

// ListInstances retrieves all compute instances in the tenancy and resolves their primary VNIC and public IP.
func (c *OCIClient) ListInstances(ctx context.Context) ([]OCIInstance, error) {
	tenancyID := strings.TrimSpace(c.cfg.TenancyID)
	if tenancyID == "" {
		return nil, errors.New("tenancy ID is required to list instances")
	}

	path := fmt.Sprintf("/20160918/instances?compartmentId=%s", tenancyID)
	resp, body, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("list instances: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("OCI list instances (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var rawInstances []struct {
		ID             string `json:"id"`
		DisplayName    string `json:"displayName"`
		LifecycleState string `json:"lifecycleState"`
		Region         string `json:"region"`
		Shape          string `json:"shape"`
		CompartmentID  string `json:"compartmentId"`
	}
	if err := json.Unmarshal(body, &rawInstances); err != nil {
		return nil, fmt.Errorf("unmarshal instances: %w", err)
	}

	region := strings.ToLower(strings.TrimSpace(c.cfg.Region))
	var result []OCIInstance

	for _, raw := range rawInstances {
		if strings.EqualFold(raw.LifecycleState, "TERMINATED") {
			continue
		}

		inst := OCIInstance{
			ID:             raw.ID,
			DisplayName:    raw.DisplayName,
			LifecycleState: raw.LifecycleState,
			Shape:          raw.Shape,
			CompartmentID:  raw.CompartmentID,
			Region:         region,
		}

		attPath := fmt.Sprintf("/20160918/vnicAttachments?compartmentId=%s&instanceId=%s", tenancyID, raw.ID)
		attResp, attBody, attErr := c.do(ctx, http.MethodGet, attPath, nil)
		if attErr == nil && attResp.StatusCode >= 200 && attResp.StatusCode < 300 {
			var attachments []struct {
				ID        string `json:"id"`
				VnicID    string `json:"vnicId"`
				NicIndex  int    `json:"nicIndex"`
				IsPrimary bool   `json:"isPrimary"`
			}
			if err := json.Unmarshal(attBody, &attachments); err == nil && len(attachments) > 0 {
				primaryVnicID := attachments[0].VnicID
				for _, att := range attachments {
					if att.IsPrimary || att.NicIndex == 0 {
						primaryVnicID = att.VnicID
						break
					}
				}
				inst.VnicID = primaryVnicID
				if vnic, err := c.GetVNIC(ctx, primaryVnicID); err == nil && vnic != nil {
					inst.PublicIP = vnic.PublicIP
					if vnic.CompartmentID != "" {
						inst.CompartmentID = vnic.CompartmentID
					}
				}
			}
		}

		result = append(result, inst)
	}

	return result, nil
}
