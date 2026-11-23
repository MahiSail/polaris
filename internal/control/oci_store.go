package control

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/MahiSail/polaris/internal/security"
)

// OCIAccount represents a configured Oracle Cloud account.
type OCIAccount struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	TenancyID        string `json:"tenancy_id"`
	UserID           string `json:"user_id"`
	Fingerprint      string `json:"fingerprint"`
	Region           string `json:"region"`
	HasPrivateKey    bool   `json:"has_private_key"`
	PrivateKeyMasked string `json:"private_key_masked,omitempty"`
	IsDefault        bool   `json:"is_default"`
	Enabled          bool   `json:"enabled"`
	CreatedAt        int64  `json:"created_at"`
	UpdatedAt        int64  `json:"updated_at"`
}

// migrateLegacyOCISettingsIfNeeded migrates single-row oci_settings into oci_accounts.
func (s *Store) migrateLegacyOCISettingsIfNeeded(ctx context.Context) {
	var count int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM oci_accounts`).Scan(&count)
	if count > 0 {
		return
	}

	var tenancyID, userID, fingerprint, region string
	var encrypted []byte
	var enabled int
	err := s.db.QueryRowContext(ctx, `SELECT tenancy_id, user_id, fingerprint, region, private_key_encrypted, enabled FROM oci_settings WHERE id = 1`).
		Scan(&tenancyID, &userID, &fingerprint, &region, &encrypted, &enabled)
	if err == nil && len(encrypted) > 0 && tenancyID != "" {
		id, err := newID()
		if err == nil {
			now := nowUnix()
			_, _ = s.db.ExecContext(ctx, `INSERT INTO oci_accounts (id, name, tenancy_id, user_id, fingerprint, region, private_key_encrypted, is_default, enabled, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?)`,
				id, "默认 OCI 账号", tenancyID, userID, fingerprint, region, encrypted, enabled, now, now)
		}
	}
}

// ListOCIAccounts returns all configured OCI accounts.
func (s *Store) ListOCIAccounts(ctx context.Context) ([]OCIAccount, error) {
	s.migrateLegacyOCISettingsIfNeeded(ctx)

	rows, err := s.db.QueryContext(ctx, `SELECT id, name, tenancy_id, user_id, fingerprint, region, private_key_encrypted, is_default, enabled, created_at, updated_at
		FROM oci_accounts ORDER BY is_default DESC, name ASC, created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list oci accounts: %w", err)
	}
	defer rows.Close()

	var accounts []OCIAccount
	for rows.Next() {
		var acc OCIAccount
		var encrypted []byte
		var isDefault, enabledInt int
		if err := rows.Scan(&acc.ID, &acc.Name, &acc.TenancyID, &acc.UserID, &acc.Fingerprint, &acc.Region, &encrypted, &isDefault, &enabledInt, &acc.CreatedAt, &acc.UpdatedAt); err != nil {
			return nil, err
		}
		acc.IsDefault = isDefault == 1
		acc.Enabled = enabledInt == 1
		if len(encrypted) > 0 {
			acc.HasPrivateKey = true
			keyBytes, err := security.Decrypt(s.masterKey, encrypted)
			if err == nil && len(keyBytes) > 0 {
				acc.PrivateKeyMasked = maskPEM(string(keyBytes))
			}
		}
		accounts = append(accounts, acc)
	}
	return accounts, rows.Err()
}

// GetOCIAccount retrieves an OCI account by ID.
func (s *Store) GetOCIAccount(ctx context.Context, id string) (OCIAccount, error) {
	s.migrateLegacyOCISettingsIfNeeded(ctx)

	var acc OCIAccount
	var encrypted []byte
	var isDefault, enabledInt int
	err := s.db.QueryRowContext(ctx, `SELECT id, name, tenancy_id, user_id, fingerprint, region, private_key_encrypted, is_default, enabled, created_at, updated_at
		FROM oci_accounts WHERE id = ?`, id).
		Scan(&acc.ID, &acc.Name, &acc.TenancyID, &acc.UserID, &acc.Fingerprint, &acc.Region, &encrypted, &isDefault, &enabledInt, &acc.CreatedAt, &acc.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return OCIAccount{}, ErrNotFound
		}
		return OCIAccount{}, fmt.Errorf("get oci account: %w", err)
	}
	acc.IsDefault = isDefault == 1
	acc.Enabled = enabledInt == 1
	if len(encrypted) > 0 {
		acc.HasPrivateKey = true
		keyBytes, err := security.Decrypt(s.masterKey, encrypted)
		if err == nil && len(keyBytes) > 0 {
			acc.PrivateKeyMasked = maskPEM(string(keyBytes))
		}
	}
	return acc, nil
}

// CreateOCIAccount adds a new OCI account.
func (s *Store) CreateOCIAccount(ctx context.Context, name, tenancyID, userID, fingerprint, region, privateKeyPEM string, isDefault, enabled bool) (OCIAccount, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return OCIAccount{}, errors.New("账号名称不能为空")
	}
	tenancyID = strings.TrimSpace(tenancyID)
	if tenancyID == "" {
		return OCIAccount{}, errors.New("租户 OCID 不能为空")
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return OCIAccount{}, errors.New("用户 OCID 不能为空")
	}
	fingerprint = strings.TrimSpace(fingerprint)
	if fingerprint == "" {
		return OCIAccount{}, errors.New("公钥指纹不能为空")
	}
	region = strings.ToLower(strings.TrimSpace(region))
	if region == "" {
		return OCIAccount{}, errors.New("默认区域不能为空")
	}
	privateKeyPEM = strings.TrimSpace(privateKeyPEM)
	if privateKeyPEM == "" {
		return OCIAccount{}, errors.New("API 私钥不能为空")
	}

	encrypted, err := security.Encrypt(s.masterKey, []byte(privateKeyPEM))
	if err != nil {
		return OCIAccount{}, fmt.Errorf("encrypt oci private key: %w", err)
	}

	id, err := newID()
	if err != nil {
		return OCIAccount{}, err
	}

	// Check if this is the first account
	var count int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM oci_accounts`).Scan(&count)
	if count == 0 {
		isDefault = true
	}

	if isDefault {
		_, _ = s.db.ExecContext(ctx, `UPDATE oci_accounts SET is_default = 0`)
	}

	now := nowUnix()
	defaultVal := 0
	if isDefault {
		defaultVal = 1
	}
	enabledVal := 0
	if enabled {
		enabledVal = 1
	}

	_, err = s.db.ExecContext(ctx, `INSERT INTO oci_accounts (id, name, tenancy_id, user_id, fingerprint, region, private_key_encrypted, is_default, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, name, tenancyID, userID, fingerprint, region, encrypted, defaultVal, enabledVal, now, now)
	if err != nil {
		return OCIAccount{}, fmt.Errorf("insert oci account: %w", err)
	}

	return s.GetOCIAccount(ctx, id)
}

// UpdateOCIAccount updates an existing OCI account. If privateKeyPEM is empty, the existing private key is kept.
func (s *Store) UpdateOCIAccount(ctx context.Context, id, name, tenancyID, userID, fingerprint, region, privateKeyPEM string, isDefault, enabled bool) (OCIAccount, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return OCIAccount{}, errors.New("账号名称不能为空")
	}
	tenancyID = strings.TrimSpace(tenancyID)
	if tenancyID == "" {
		return OCIAccount{}, errors.New("租户 OCID 不能为空")
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return OCIAccount{}, errors.New("用户 OCID 不能为空")
	}
	fingerprint = strings.TrimSpace(fingerprint)
	if fingerprint == "" {
		return OCIAccount{}, errors.New("公钥指纹不能为空")
	}
	region = strings.ToLower(strings.TrimSpace(region))
	if region == "" {
		return OCIAccount{}, errors.New("默认区域不能为空")
	}

	if isDefault {
		_, _ = s.db.ExecContext(ctx, `UPDATE oci_accounts SET is_default = 0 WHERE id != ?`, id)
	}

	now := nowUnix()
	defaultVal := 0
	if isDefault {
		defaultVal = 1
	}
	enabledVal := 0
	if enabled {
		enabledVal = 1
	}

	privateKeyPEM = strings.TrimSpace(privateKeyPEM)
	if privateKeyPEM != "" {
		encrypted, err := security.Encrypt(s.masterKey, []byte(privateKeyPEM))
		if err != nil {
			return OCIAccount{}, fmt.Errorf("encrypt oci private key: %w", err)
		}
		_, err = s.db.ExecContext(ctx, `UPDATE oci_accounts SET name = ?, tenancy_id = ?, user_id = ?, fingerprint = ?, region = ?, private_key_encrypted = ?, is_default = ?, enabled = ?, updated_at = ?
			WHERE id = ?`,
			name, tenancyID, userID, fingerprint, region, encrypted, defaultVal, enabledVal, now, id)
		if err != nil {
			return OCIAccount{}, fmt.Errorf("update oci account: %w", err)
		}
	} else {
		_, err := s.db.ExecContext(ctx, `UPDATE oci_accounts SET name = ?, tenancy_id = ?, user_id = ?, fingerprint = ?, region = ?, is_default = ?, enabled = ?, updated_at = ?
			WHERE id = ?`,
			name, tenancyID, userID, fingerprint, region, defaultVal, enabledVal, now, id)
		if err != nil {
			return OCIAccount{}, fmt.Errorf("update oci account: %w", err)
		}
	}

	return s.GetOCIAccount(ctx, id)
}

// DeleteOCIAccount deletes an OCI account.
func (s *Store) DeleteOCIAccount(ctx context.Context, id string) error {
	acc, err := s.GetOCIAccount(ctx, id)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `DELETE FROM oci_accounts WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete oci account: %w", err)
	}

	// Unbind nodes referencing this account
	_, _ = s.db.ExecContext(ctx, `UPDATE nodes SET oci_account_id = '' WHERE oci_account_id = ?`, id)

	// If the deleted account was default, pick another account to be default
	if acc.IsDefault {
		_, _ = s.db.ExecContext(ctx, `UPDATE oci_accounts SET is_default = 1 WHERE id IN (SELECT id FROM oci_accounts LIMIT 1)`)
	}

	return nil
}

// SetDefaultOCIAccount sets the specified account as the default.
func (s *Store) SetDefaultOCIAccount(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE oci_accounts SET is_default = CASE WHEN id = ? THEN 1 ELSE 0 END`, id)
	return err
}

// LoadOCIAccountConfig loads decrypted OCIConfig for a specific account ID (or the default account if empty).
func (s *Store) LoadOCIAccountConfig(ctx context.Context, accountID string) (OCIConfig, error) {
	s.migrateLegacyOCISettingsIfNeeded(ctx)

	var query string
	var args []any
	if strings.TrimSpace(accountID) != "" {
		query = `SELECT tenancy_id, user_id, fingerprint, region, private_key_encrypted, enabled FROM oci_accounts WHERE id = ?`
		args = []any{accountID}
	} else {
		query = `SELECT tenancy_id, user_id, fingerprint, region, private_key_encrypted, enabled FROM oci_accounts WHERE enabled = 1 ORDER BY is_default DESC, created_at ASC LIMIT 1`
	}

	var tenancyID, userID, fingerprint, region string
	var encrypted []byte
	var enabledInt int
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&tenancyID, &userID, &fingerprint, &region, &encrypted, &enabledInt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Fallback: check legacy oci_settings
			return s.loadLegacyOCIConfig(ctx)
		}
		return OCIConfig{}, fmt.Errorf("load oci account config: %w", err)
	}

	if enabledInt == 0 {
		return OCIConfig{}, errors.New("该 OCI 账号已被停用")
	}

	keyBytes, err := security.Decrypt(s.masterKey, encrypted)
	if err != nil {
		return OCIConfig{}, fmt.Errorf("decrypt oci private key: %w", err)
	}

	return OCIConfig{
		TenancyID:     tenancyID,
		UserID:        userID,
		Fingerprint:   fingerprint,
		Region:        region,
		PrivateKeyPEM: string(keyBytes),
	}, nil
}

func (s *Store) loadLegacyOCIConfig(ctx context.Context) (OCIConfig, error) {
	var tenancyID, userID, fingerprint, region string
	var encrypted []byte
	var enabled int
	err := s.db.QueryRowContext(ctx, `SELECT tenancy_id, user_id, fingerprint, region, private_key_encrypted, enabled FROM oci_settings WHERE id = 1`).
		Scan(&tenancyID, &userID, &fingerprint, &region, &encrypted, &enabled)
	if err != nil {
		return OCIConfig{}, errors.New("尚未配置任何 OCI 账号")
	}
	if enabled != 1 || len(encrypted) == 0 {
		return OCIConfig{}, errors.New("尚未配置任何 OCI 账号")
	}
	keyBytes, err := security.Decrypt(s.masterKey, encrypted)
	if err != nil {
		return OCIConfig{}, fmt.Errorf("decrypt oci private key: %w", err)
	}
	return OCIConfig{
		TenancyID:     tenancyID,
		UserID:        userID,
		Fingerprint:   fingerprint,
		Region:        region,
		PrivateKeyPEM: string(keyBytes),
	}, nil
}

// LoadOCIConfig is backward compatible: loads the default OCI config.
func (s *Store) LoadOCIConfig(ctx context.Context) (OCIConfig, bool, error) {
	cfg, err := s.LoadOCIAccountConfig(ctx, "")
	if err != nil {
		return OCIConfig{}, false, nil
	}
	return cfg, true, nil
}

// UpdateNodeOCI updates node's OCI binding including account ID.
func (s *Store) UpdateNodeOCI(ctx context.Context, nodeID, accountID, vnicID, privateIPID, compartmentID, region string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE nodes SET oci_account_id = ?, oci_vnic_id = ?, oci_private_ip_id = ?, oci_compartment_id = ?, oci_region = ? WHERE id = ?`,
		accountID, vnicID, privateIPID, compartmentID, region, nodeID)
	return err
}

// Legacy OCISettingsView compatibility
type OCISettingsView struct {
	TenancyID        string `json:"tenancy_id"`
	UserID           string `json:"user_id"`
	Fingerprint      string `json:"fingerprint"`
	Region           string `json:"region"`
	HasPrivateKey    bool   `json:"has_private_key"`
	PrivateKeyMasked string `json:"private_key_masked,omitempty"`
	Enabled          bool   `json:"enabled"`
	UpdatedAt        int64  `json:"updated_at"`
}

func (s *Store) OCISettings(ctx context.Context) (OCISettingsView, error) {
	accounts, err := s.ListOCIAccounts(ctx)
	if err != nil {
		return OCISettingsView{}, err
	}
	if len(accounts) == 0 {
		return OCISettingsView{}, nil
	}
	defaultAcc := accounts[0]
	return OCISettingsView{
		TenancyID:        defaultAcc.TenancyID,
		UserID:           defaultAcc.UserID,
		Fingerprint:      defaultAcc.Fingerprint,
		Region:           defaultAcc.Region,
		HasPrivateKey:    defaultAcc.HasPrivateKey,
		PrivateKeyMasked: defaultAcc.PrivateKeyMasked,
		Enabled:          true,
		UpdatedAt:        defaultAcc.UpdatedAt,
	}, nil
}

func (s *Store) SaveOCISettings(ctx context.Context, tenancyID, userID, fingerprint, region, privateKeyPEM string, enabled bool) error {
	accounts, err := s.ListOCIAccounts(ctx)
	if err == nil && len(accounts) > 0 {
		_, err := s.UpdateOCIAccount(ctx, accounts[0].ID, accounts[0].Name, tenancyID, userID, fingerprint, region, privateKeyPEM, true, enabled)
		return err
	}
	_, err = s.CreateOCIAccount(ctx, "默认 OCI 账号", tenancyID, userID, fingerprint, region, privateKeyPEM, true, enabled)
	return err
}

func maskPEM(pemStr string) string {
	lines := strings.Split(strings.TrimSpace(pemStr), "\n")
	if len(lines) <= 2 {
		return "-----BEGIN RSA PRIVATE KEY-----\n********************\n-----END RSA PRIVATE KEY-----"
	}
	return fmt.Sprintf("%s\n******************** (configured, %d bytes)\n%s", lines[0], len(pemStr), lines[len(lines)-1])
}
