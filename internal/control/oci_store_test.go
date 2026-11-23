package control

import (
	"context"
	"testing"
)

func TestStore_OCIAccountsAndNodeBinding(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// 1. Initially no accounts
	accounts, err := store.ListOCIAccounts(ctx)
	if err != nil {
		t.Fatalf("ListOCIAccounts: %v", err)
	}
	if len(accounts) != 0 {
		t.Fatalf("expected 0 accounts, got %d", len(accounts))
	}

	pem1 := "-----BEGIN RSA PRIVATE KEY-----\nKEY1...\n-----END RSA PRIVATE KEY-----"
	pem2 := "-----BEGIN RSA PRIVATE KEY-----\nKEY2...\n-----END RSA PRIVATE KEY-----"

	// 2. Create Account 1 (Tokyo)
	acc1, err := store.CreateOCIAccount(ctx, "东京主力号", "ocid1.tenancy.tokyo", "ocid1.user.tokyo", "11:11:11:11", "ap-tokyo-1", pem1, false)
	if err != nil {
		t.Fatalf("Create Account 1: %v", err)
	}
	if !acc1.IsDefault {
		t.Fatalf("first account should automatically become default")
	}

	// 3. Create Account 2 (Frankfurt)
	acc2, err := store.CreateOCIAccount(ctx, "法兰克福备用号", "ocid1.tenancy.fra", "ocid1.user.fra", "22:22:22:22", "eu-frankfurt-1", pem2, false)
	if err != nil {
		t.Fatalf("Create Account 2: %v", err)
	}
	if acc2.IsDefault {
		t.Fatalf("second account should not be default")
	}

	// 4. List accounts: Account 1 should be first (isDefault = true)
	accounts, err = store.ListOCIAccounts(ctx)
	if err != nil || len(accounts) != 2 {
		t.Fatalf("expected 2 accounts, got %d (err: %v)", len(accounts), err)
	}
	if accounts[0].ID != acc1.ID || !accounts[0].IsDefault {
		t.Fatalf("account 1 should be listed first as default")
	}

	// 5. Test loading config by ID
	cfg2, err := store.LoadOCIAccountConfig(ctx, acc2.ID)
	if err != nil {
		t.Fatalf("LoadOCIAccountConfig(acc2): %v", err)
	}
	if cfg2.Region != "eu-frankfurt-1" || cfg2.PrivateKeyPEM != pem2 {
		t.Fatalf("unexpected cfg2: %+v", cfg2)
	}

	// 6. Test loading default config (empty accountID)
	cfgDefault, err := store.LoadOCIAccountConfig(ctx, "")
	if err != nil {
		t.Fatalf("LoadOCIAccountConfig(''): %v", err)
	}
	if cfgDefault.Region != "ap-tokyo-1" || cfgDefault.PrivateKeyPEM != pem1 {
		t.Fatalf("unexpected cfgDefault: %+v", cfgDefault)
	}

	// 7. Switch default to Account 2
	if err := store.SetDefaultOCIAccount(ctx, acc2.ID); err != nil {
		t.Fatalf("SetDefaultOCIAccount: %v", err)
	}
	cfgDefaultNew, err := store.LoadOCIAccountConfig(ctx, "")
	if err != nil {
		t.Fatalf("LoadOCIAccountConfig('') after switch: %v", err)
	}
	if cfgDefaultNew.Region != "eu-frankfurt-1" {
		t.Fatalf("expected default to be Frankfurt, got %s", cfgDefaultNew.Region)
	}

	// 8. Test Node Binding to Account 2
	nodeID := "test-node-multi-oci"
	if _, err := store.db.ExecContext(ctx, `INSERT INTO nodes (id, name, public_key, created_at) VALUES (?, ?, ?, ?)`,
		nodeID, "Oracle-DE", []byte("32-byte-public-key-test-de-node!"), nowUnix()); err != nil {
		t.Fatalf("insert test node: %v", err)
	}

	if err := store.UpdateNodeOCI(ctx, nodeID, acc2.ID, "ocid1.vnic.de", "ocid1.privateip.de", "ocid1.compartment.de", "eu-frankfurt-1"); err != nil {
		t.Fatalf("UpdateNodeOCI: %v", err)
	}

	loadedNode, err := store.GetNode(ctx, nodeID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if loadedNode.OCIAccountID != acc2.ID || loadedNode.OCIVnicID != "ocid1.vnic.de" || loadedNode.OCIRegion != "eu-frankfurt-1" {
		t.Fatalf("node OCI fields mismatch: %+v", loadedNode)
	}

	// 9. Delete Account 1 (ensure Account 2 remains and node is intact)
	if err := store.DeleteOCIAccount(ctx, acc1.ID); err != nil {
		t.Fatalf("DeleteOCIAccount: %v", err)
	}
	accounts, err = store.ListOCIAccounts(ctx)
	if err != nil || len(accounts) != 1 {
		t.Fatalf("expected 1 account after delete, got %d", len(accounts))
	}
}
