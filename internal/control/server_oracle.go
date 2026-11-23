package control

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	ociRotateMu      sync.Mutex
	ociRotatingNodes = make(map[string]bool)
)

func (s *Server) listOCIAccounts(w http.ResponseWriter, r *http.Request) {
	if _, err := s.operator(r, false); err != nil {
		writeError(w, err)
		return
	}
	accounts, err := s.store.ListOCIAccounts(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	if accounts == nil {
		accounts = []OCIAccount{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": accounts})
}

func (s *Server) createOCIAccount(w http.ResponseWriter, r *http.Request) {
	operator, err := s.admin(r)
	if err != nil {
		writeError(w, err)
		return
	}

	var input struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		TenancyID   string `json:"tenancy_id"`
		UserID      string `json:"user_id"`
		Fingerprint string `json:"fingerprint"`
		Region      string `json:"region"`
		PrivateKey  string `json:"private_key"`
		IsDefault   bool   `json:"is_default"`
		Enabled     *bool  `json:"enabled"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}

	input.PrivateKey = strings.TrimSpace(input.PrivateKey)
	if input.PrivateKey != "" {
		block, _ := pem.Decode([]byte(input.PrivateKey))
		if block == nil {
			writeError(w, errors.New("私钥必须为有效的 PEM 格式"))
			return
		}
		_, errPKCS8 := x509.ParsePKCS8PrivateKey(block.Bytes)
		_, errPKCS1 := x509.ParsePKCS1PrivateKey(block.Bytes)
		if errPKCS8 != nil && errPKCS1 != nil {
			writeError(w, errors.New("私钥解析失败，请提供 PKCS#1 或 PKCS#8 格式的 RSA 私钥"))
			return
		}
	}

	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}

	acc, err := s.store.CreateOCIAccount(r.Context(), input.Name, input.TenancyID, input.UserID, input.Fingerprint, input.Region, input.PrivateKey, input.IsDefault, enabled)
	if err != nil {
		writeError(w, err)
		return
	}

	_ = s.store.AppendAudit(r.Context(), operator.ID, "oci.account_created", "system", acc.ID, fmt.Sprintf("OCI 账号创建成功: %s (%s)", acc.Name, acc.Region))
	writeJSON(w, http.StatusOK, acc)
}

func (s *Server) updateOCIAccount(w http.ResponseWriter, r *http.Request) {
	operator, err := s.admin(r)
	if err != nil {
		writeError(w, err)
		return
	}

	id := r.PathValue("id")
	if id == "" {
		writeError(w, errors.New("account id is required"))
		return
	}

	var input struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		TenancyID   string `json:"tenancy_id"`
		UserID      string `json:"user_id"`
		Fingerprint string `json:"fingerprint"`
		Region      string `json:"region"`
		PrivateKey  string `json:"private_key"`
		IsDefault   bool   `json:"is_default"`
		Enabled     *bool  `json:"enabled"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}

	input.PrivateKey = strings.TrimSpace(input.PrivateKey)
	if input.PrivateKey != "" {
		block, _ := pem.Decode([]byte(input.PrivateKey))
		if block == nil {
			writeError(w, errors.New("私钥必须为有效的 PEM 格式"))
			return
		}
		_, errPKCS8 := x509.ParsePKCS8PrivateKey(block.Bytes)
		_, errPKCS1 := x509.ParsePKCS1PrivateKey(block.Bytes)
		if errPKCS8 != nil && errPKCS1 != nil {
			writeError(w, errors.New("私钥解析失败，请提供 PKCS#1 或 PKCS#8 格式的 RSA 私钥"))
			return
		}
	}

	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}

	acc, err := s.store.UpdateOCIAccount(r.Context(), id, input.Name, input.TenancyID, input.UserID, input.Fingerprint, input.Region, input.PrivateKey, input.IsDefault, enabled)
	if err != nil {
		writeError(w, err)
		return
	}

	_ = s.store.AppendAudit(r.Context(), operator.ID, "oci.account_updated", "system", acc.ID, fmt.Sprintf("OCI 账号更新成功: %s", acc.Name))
	writeJSON(w, http.StatusOK, acc)
}

func (s *Server) deleteOCIAccount(w http.ResponseWriter, r *http.Request) {
	operator, err := s.admin(r)
	if err != nil {
		writeError(w, err)
		return
	}

	id := r.PathValue("id")
	if id == "" {
		writeError(w, errors.New("account id is required"))
		return
	}

	if err := s.store.DeleteOCIAccount(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}

	_ = s.store.AppendAudit(r.Context(), operator.ID, "oci.account_deleted", "system", id, "OCI 账号已删除")
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) setDefaultOCIAccount(w http.ResponseWriter, r *http.Request) {
	operator, err := s.admin(r)
	if err != nil {
		writeError(w, err)
		return
	}

	id := r.PathValue("id")
	if id == "" {
		writeError(w, errors.New("account id is required"))
		return
	}

	if err := s.store.SetDefaultOCIAccount(r.Context(), id); err != nil {
		writeError(w, fmt.Errorf("设置默认账号失败: %w", err))
		return
	}

	_ = s.store.AppendAudit(r.Context(), operator.ID, "oci.account_set_default", "system", id, "已将 OCI 账号设为默认")
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) testOCIAccount(w http.ResponseWriter, r *http.Request) {
	if _, err := s.admin(r); err != nil {
		writeError(w, err)
		return
	}

	accountID := r.PathValue("id")
	var input struct {
		TenancyID   string `json:"tenancy_id"`
		UserID      string `json:"user_id"`
		Fingerprint string `json:"fingerprint"`
		Region      string `json:"region"`
		PrivateKey  string `json:"private_key"`
	}
	_ = json.NewDecoder(r.Body).Decode(&input)

	var cfg OCIConfig
	if strings.TrimSpace(input.TenancyID) != "" && strings.TrimSpace(input.PrivateKey) != "" {
		cfg = OCIConfig{
			TenancyID:     strings.TrimSpace(input.TenancyID),
			UserID:        strings.TrimSpace(input.UserID),
			Fingerprint:   strings.TrimSpace(input.Fingerprint),
			Region:        strings.ToLower(strings.TrimSpace(input.Region)),
			PrivateKeyPEM: strings.TrimSpace(input.PrivateKey),
		}
	} else {
		// Load from store by accountID or default
		loadedCfg, err := s.store.LoadOCIAccountConfig(r.Context(), accountID)
		if err != nil {
			writeError(w, fmt.Errorf("加载 OCI 账号失败: %w", err))
			return
		}
		cfg = loadedCfg
		if strings.TrimSpace(input.Region) != "" {
			cfg.Region = strings.ToLower(strings.TrimSpace(input.Region))
		}
	}

	client, err := NewOCIClient(cfg)
	if err != nil {
		writeError(w, fmt.Errorf("初始化 OCI 客户端失败: %w", err))
		return
	}

	if err := client.TestConnection(r.Context()); err != nil {
		writeError(w, fmt.Errorf("OCI API 连通性测试失败: %w", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "OCI API 连接成功，凭据与区域有效",
	})
}

func (s *Server) listOCIAccountInstances(w http.ResponseWriter, r *http.Request) {
	if _, err := s.operator(r, false); err != nil {
		writeError(w, err)
		return
	}

	accountID := r.PathValue("id")
	cfg, err := s.store.LoadOCIAccountConfig(r.Context(), accountID)
	if err != nil {
		writeError(w, fmt.Errorf("加载 OCI 账号配置失败: %w", err))
		return
	}

	client, err := NewOCIClient(cfg)
	if err != nil {
		writeError(w, fmt.Errorf("初始化 OCI 客户端失败: %w", err))
		return
	}

	instances, err := client.ListInstances(r.Context())
	if err != nil {
		writeError(w, fmt.Errorf("查询 OCI 实例列表失败: %w", err))
		return
	}

	if instances == nil {
		instances = []OCIInstance{}
	}

	writeJSON(w, http.StatusOK, map[string]any{"instances": instances})
}

func (s *Server) updateNodeOCI(w http.ResponseWriter, r *http.Request) {
	operator, err := s.writer(r)
	if err != nil {
		writeError(w, err)
		return
	}

	nodeID := r.PathValue("id")
	if nodeID == "" {
		writeError(w, errors.New("node id is required"))
		return
	}

	var input struct {
		AccountID     string `json:"account_id"`
		VnicID        string `json:"vnic_id"`
		PrivateIPID   string `json:"private_ip_id"`
		CompartmentID string `json:"compartment_id"`
		Region        string `json:"region"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}

	accountID := strings.TrimSpace(input.AccountID)
	vnicID := strings.TrimSpace(input.VnicID)
	privateIPID := strings.TrimSpace(input.PrivateIPID)
	compartmentID := strings.TrimSpace(input.CompartmentID)
	region := strings.ToLower(strings.TrimSpace(input.Region))

	if err := s.store.UpdateNodeOCI(r.Context(), nodeID, accountID, vnicID, privateIPID, compartmentID, region); err != nil {
		writeError(w, err)
		return
	}

	_ = s.store.AppendAudit(r.Context(), operator.ID, "node.oci_updated", "node", nodeID, "node OCI identifiers updated")

	node, err := s.store.GetNode(r.Context(), nodeID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, node)
}

func (s *Server) rotateNodeOCIIP(w http.ResponseWriter, r *http.Request) {
	operator, err := s.writer(r)
	if err != nil {
		writeError(w, err)
		return
	}

	nodeID := r.PathValue("id")
	if nodeID == "" {
		writeError(w, errors.New("node id is required"))
		return
	}

	ociRotateMu.Lock()
	if ociRotatingNodes[nodeID] {
		ociRotateMu.Unlock()
		writeError(w, errors.New("该服务器正在更换 IP 中，请勿重复操作"))
		return
	}
	ociRotatingNodes[nodeID] = true
	ociRotateMu.Unlock()

	defer func() {
		ociRotateMu.Lock()
		delete(ociRotatingNodes, nodeID)
		ociRotateMu.Unlock()
	}()

	node, err := s.store.GetNode(r.Context(), nodeID)
	if err != nil {
		writeError(w, err)
		return
	}

	// Load OCI account config: priority is node's bound account, otherwise default account
	cfg, err := s.store.LoadOCIAccountConfig(r.Context(), node.OCIAccountID)
	if err != nil {
		writeError(w, fmt.Errorf("未能获取可用的 OCI 账号配置: %w", err))
		return
	}

	// Use node-specific region override if present
	if strings.TrimSpace(node.OCIRegion) != "" {
		cfg.Region = strings.ToLower(strings.TrimSpace(node.OCIRegion))
	}

	client, err := NewOCIClient(cfg)
	if err != nil {
		writeError(w, fmt.Errorf("初始化 OCI 客户端失败: %w", err))
		return
	}

	compartmentID := strings.TrimSpace(node.OCICompartmentID)
	privateIPID := strings.TrimSpace(node.OCIPrivateIPID)
	vnicID := strings.TrimSpace(node.OCIVnicID)

	// If identifiers are incomplete, attempt auto-resolution via VNIC ID
	if vnicID != "" {
		if compartmentID == "" {
			vnic, err := client.GetVNIC(r.Context(), vnicID)
			if err != nil {
				writeError(w, fmt.Errorf("通过 VNIC 查询区间 (Compartment) 失败: %w", err))
				return
			}
			compartmentID = vnic.CompartmentID
		}

		if privateIPID == "" {
			privIP, err := client.GetPrimaryPrivateIP(r.Context(), vnicID)
			if err != nil {
				writeError(w, fmt.Errorf("通过 VNIC 查询主私有 IP 失败: %w", err))
				return
			}
			privateIPID = privIP.ID
		}

		// Cache resolved IDs back to node
		_ = s.store.UpdateNodeOCI(r.Context(), node.ID, node.OCIAccountID, vnicID, privateIPID, compartmentID, cfg.Region)
	} else if compartmentID == "" || privateIPID == "" {
		writeError(w, errors.New("未配置该节点的 VNIC OCID 或主私有 IP OCID，无法在 OCI 执行换 IP 操作"))
		return
	}

	// Use an independent operation context with a safe timeout (90s).
	// This ensures that once old IP deletion starts, allocating the replacement IP
	// and updating node state won't be interrupted midway even if client cancels or disconnects.
	opCtx, opCancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer opCancel()

	// Perform atomic IP rotation on OCI
	oldIP, newIP, err := client.RotateEphemeralIP(opCtx, compartmentID, privateIPID)
	if err != nil {
		writeError(w, fmt.Errorf("OCI 更换公网 IP 失败: %w", err))
		return
	}

	// Update node's observed IP
	_ = s.store.SetNodeLastObservedIP(r.Context(), node.ID, newIP)

	// If node's client address is directly set to an IP address, update client address as well
	trimmedAddr := strings.TrimSpace(node.ClientAddress)
	if net.ParseIP(trimmedAddr) != nil || trimmedAddr == oldIP {
		_ = s.store.UpdateNode(r.Context(), node.ID, node.Name, newIP)
	}

	// Trigger DDNS update if enabled on this node
	ddnsTriggered := false
	if node.DDNSEnabled && (len(node.DDNSRecordIDs) > 0 || node.DDNSRecordID != "") {
		ddnsTriggered = true
		go s.triggerNodeDDNS(context.Background(), node.ID, newIP)
	}

	// Record audit event
	auditMsg := fmt.Sprintf("Oracle 公网 IP 已从 %s 更换为 %s", oldIP, newIP)
	if ddnsTriggered {
		auditMsg += " (已触发 DDNS 自动更新)"
	}
	_ = s.store.AppendAudit(r.Context(), operator.ID, "node.oci_ip_changed", "node", node.ID, auditMsg)

	writeJSON(w, http.StatusOK, map[string]any{
		"success":        true,
		"old_ip":         oldIP,
		"new_ip":         newIP,
		"ddns_triggered": ddnsTriggered,
		"message":        auditMsg,
	})
}

// Backward-compatible single-settings handlers
func (s *Server) ociSettings(w http.ResponseWriter, r *http.Request) {
	if _, err := s.operator(r, false); err != nil {
		writeError(w, err)
		return
	}
	view, err := s.store.OCISettings(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) setOCISettings(w http.ResponseWriter, r *http.Request) {
	s.createOCIAccount(w, r)
}
