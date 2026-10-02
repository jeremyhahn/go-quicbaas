//go:build quicbaas_signal

package signal

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-signal/pkg/keys"
	"github.com/jeremyhahn/go-signal/pkg/messages"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// serviceTestKVClient wraps a simple in-memory map to implement
// qrdbtransport.KVClient. The variadic options are ignored.
type serviceTestKVClient struct {
	data map[string][]byte
}

func newServiceTestKVClient() *serviceTestKVClient {
	return &serviceTestKVClient{data: make(map[string][]byte)}
}

func (c *serviceTestKVClient) Put(_ context.Context, key string, value []byte, _ ...qrdbtransport.WriteOption) error {
	cp := make([]byte, len(value))
	copy(cp, value)
	c.data[key] = cp
	return nil
}

func (c *serviceTestKVClient) Get(_ context.Context, key string, _ ...qrdbtransport.ReadOption) ([]byte, error) {
	v, ok := c.data[key]
	if !ok {
		return nil, &qrdbtransport.KeyNotFoundError{Key: key}
	}
	cp := make([]byte, len(v))
	copy(cp, v)
	return cp, nil
}

func (c *serviceTestKVClient) Delete(_ context.Context, key string, _ ...qrdbtransport.WriteOption) error {
	if _, ok := c.data[key]; !ok {
		return &qrdbtransport.KeyNotFoundError{Key: key}
	}
	delete(c.data, key)
	return nil
}

func (c *serviceTestKVClient) List(_ context.Context, prefix string, _ ...qrdbtransport.ReadOption) ([]string, error) {
	var result []string
	for k := range c.data {
		if strings.HasPrefix(k, prefix) {
			result = append(result, k)
		}
	}
	sort.Strings(result)
	return result, nil
}

func (c *serviceTestKVClient) Scan(_ context.Context, prefix string, _ ...qrdbtransport.ReadOption) (map[string][]byte, error) {
	result := make(map[string][]byte)
	for k, v := range c.data {
		if strings.HasPrefix(k, prefix) {
			cp := make([]byte, len(v))
			copy(cp, v)
			result[k] = cp
		}
	}
	return result, nil
}

func (c *serviceTestKVClient) Exists(_ context.Context, key string, _ ...qrdbtransport.ReadOption) (bool, error) {
	_, ok := c.data[key]
	return ok, nil
}

var _ qrdbtransport.KVClient = (*serviceTestKVClient)(nil)

func newTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	return log
}

func newEnabledService(t *testing.T) (SignalService, *serviceTestKVClient) {
	t.Helper()
	kv := newServiceTestKVClient()
	log := newTestLogger(t)
	svc := NewSignalService(kv, log, &SignalConfig{Enabled: true})
	return svc, kv
}

func provisionTestTenant(t *testing.T, svc SignalService, tenantID string) {
	t.Helper()
	ctx := context.Background()
	cfg := &SignalTenantConfig{
		Enabled:     true,
		MaxAccounts: 100,
		MaxDevices:  6,
	}
	if err := svc.ProvisionTenant(ctx, tenantID, cfg); err != nil {
		t.Fatalf("failed to provision tenant: %v", err)
	}
}

func tenantCtx(tenantID string) context.Context {
	return tenantctx.WithTenant(context.Background(), tenantID)
}

func TestNewSignalService(t *testing.T) {
	kv := newServiceTestKVClient()
	log := newTestLogger(t)
	svc := NewSignalService(kv, log, &SignalConfig{Enabled: true})
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

func TestNewSignalService_Disabled(t *testing.T) {
	kv := newServiceTestKVClient()
	log := newTestLogger(t)
	svc := NewSignalService(kv, log, &SignalConfig{Enabled: false})
	if svc == nil {
		t.Fatal("expected non-nil service even when disabled")
	}
}

func TestSignalService_Start_Disabled(t *testing.T) {
	kv := newServiceTestKVClient()
	log := newTestLogger(t)
	svc := NewSignalService(kv, log, &SignalConfig{Enabled: false})
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("expected Start to succeed when disabled, got: %v", err)
	}
}

func TestSignalService_Start_Enabled(t *testing.T) {
	kv := newServiceTestKVClient()
	log := newTestLogger(t)

	// Pre-populate a tenant config in the KV store.
	cfg := &SignalTenantConfig{
		Enabled:          true,
		MaxAccounts:      50,
		MaxDevices:       3,
		MessageRetention: "7d",
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}
	key := tenantConfigKeyPrefix + "preloaded-tenant" + tenantConfigKeySuffix
	kv.data[key] = data

	svc := NewSignalService(kv, log, &SignalConfig{Enabled: true})
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Verify the config was loaded.
	loaded, err := svc.GetTenantConfig(context.Background(), "preloaded-tenant")
	if err != nil {
		t.Fatalf("expected loaded tenant config, got error: %v", err)
	}
	if loaded.MaxAccounts != 50 {
		t.Fatalf("expected MaxAccounts 50, got %d", loaded.MaxAccounts)
	}
}

func TestSignalService_ProvisionTenant(t *testing.T) {
	svc, _ := newEnabledService(t)
	ctx := context.Background()

	cfg := &SignalTenantConfig{Enabled: true}
	if err := svc.ProvisionTenant(ctx, "new-tenant", cfg); err != nil {
		t.Fatalf("ProvisionTenant failed: %v", err)
	}

	got, err := svc.GetTenantConfig(ctx, "new-tenant")
	if err != nil {
		t.Fatalf("GetTenantConfig failed: %v", err)
	}
	if got.MaxAccounts != defaultMaxAccounts {
		t.Fatalf("expected defaults applied, got MaxAccounts %d", got.MaxAccounts)
	}
}

func TestSignalService_ProvisionTenant_AlreadyExists(t *testing.T) {
	svc, _ := newEnabledService(t)
	provisionTestTenant(t, svc, "dup-tenant")

	err := svc.ProvisionTenant(context.Background(), "dup-tenant", &SignalTenantConfig{Enabled: true})
	if err == nil {
		t.Fatal("expected error for duplicate provision")
	}
	var target *TenantAlreadyProvisionedError
	if !errors.As(err, &target) {
		t.Fatalf("expected TenantAlreadyProvisionedError, got %T: %v", err, err)
	}
}

func TestSignalService_ProvisionTenant_InvalidConfig(t *testing.T) {
	svc, _ := newEnabledService(t)

	cfg := &SignalTenantConfig{MaxAccounts: -1}
	err := svc.ProvisionTenant(context.Background(), "bad-tenant", cfg)
	if err == nil {
		t.Fatal("expected error for invalid config")
	}
	var target *InvalidConfigError
	if !errors.As(err, &target) {
		t.Fatalf("expected InvalidConfigError, got %T: %v", err, err)
	}
}

func TestSignalService_ProvisionTenant_EmptyTenantID(t *testing.T) {
	svc, _ := newEnabledService(t)

	err := svc.ProvisionTenant(context.Background(), "", &SignalTenantConfig{Enabled: true})
	if err == nil {
		t.Fatal("expected error for empty tenant ID")
	}
	var target *InvalidConfigError
	if !errors.As(err, &target) {
		t.Fatalf("expected InvalidConfigError, got %T: %v", err, err)
	}
}

func TestSignalService_ProvisionTenant_Disabled(t *testing.T) {
	kv := newServiceTestKVClient()
	log := newTestLogger(t)
	svc := NewSignalService(kv, log, &SignalConfig{Enabled: false})

	err := svc.ProvisionTenant(context.Background(), "tenant", &SignalTenantConfig{Enabled: true})
	if err == nil {
		t.Fatal("expected error when service is disabled")
	}
	var target *SignalNotEnabledError
	if !errors.As(err, &target) {
		t.Fatalf("expected SignalNotEnabledError, got %T: %v", err, err)
	}
}

func TestSignalService_DeprovisionTenant(t *testing.T) {
	svc, _ := newEnabledService(t)
	provisionTestTenant(t, svc, "remove-tenant")

	if err := svc.DeprovisionTenant(context.Background(), "remove-tenant"); err != nil {
		t.Fatalf("DeprovisionTenant failed: %v", err)
	}

	_, err := svc.GetTenantConfig(context.Background(), "remove-tenant")
	if err == nil {
		t.Fatal("expected error after deprovisioning")
	}
	var target *TenantNotFoundError
	if !errors.As(err, &target) {
		t.Fatalf("expected TenantNotFoundError, got %T: %v", err, err)
	}
}

func TestSignalService_DeprovisionTenant_NotFound(t *testing.T) {
	svc, _ := newEnabledService(t)

	err := svc.DeprovisionTenant(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for deprovisioning nonexistent tenant")
	}
	var target *TenantNotFoundError
	if !errors.As(err, &target) {
		t.Fatalf("expected TenantNotFoundError, got %T: %v", err, err)
	}
}

func TestSignalService_GetTenantConfig(t *testing.T) {
	svc, _ := newEnabledService(t)
	provisionTestTenant(t, svc, "get-tenant")

	cfg, err := svc.GetTenantConfig(context.Background(), "get-tenant")
	if err != nil {
		t.Fatalf("GetTenantConfig failed: %v", err)
	}
	if !cfg.Enabled {
		t.Fatal("expected tenant config to be enabled")
	}
}

func TestSignalService_GetTenantConfig_NotFound(t *testing.T) {
	svc, _ := newEnabledService(t)

	_, err := svc.GetTenantConfig(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error for missing tenant config")
	}
	var target *TenantNotFoundError
	if !errors.As(err, &target) {
		t.Fatalf("expected TenantNotFoundError, got %T: %v", err, err)
	}
}

func TestSignalService_RegisterAccount(t *testing.T) {
	svc, _ := newEnabledService(t)
	provisionTestTenant(t, svc, "reg-tenant")
	ctx := tenantCtx("reg-tenant")

	resp, err := svc.RegisterAccount(ctx, &RegisterRequest{
		Number:     "+15551234567",
		Password:   "test-password",
		DeviceName: "test-device",
	})
	if err != nil {
		t.Fatalf("RegisterAccount failed: %v", err)
	}
	if resp.UUID == "" {
		t.Fatal("expected non-empty UUID")
	}
	if resp.Number != "+15551234567" {
		t.Fatalf("expected number +15551234567, got %s", resp.Number)
	}
	if resp.DeviceID != 1 {
		t.Fatalf("expected device ID 1, got %d", resp.DeviceID)
	}
}

func TestSignalService_RegisterAccount_QuotaExceeded(t *testing.T) {
	svc, _ := newEnabledService(t)
	ctx := context.Background()

	// Provision with MaxAccounts=1.
	cfg := &SignalTenantConfig{
		Enabled:     true,
		MaxAccounts: 1,
		MaxDevices:  6,
	}
	if err := svc.ProvisionTenant(ctx, "quota-tenant", cfg); err != nil {
		t.Fatalf("ProvisionTenant failed: %v", err)
	}

	tCtx := tenantCtx("quota-tenant")

	// Register first account.
	_, err := svc.RegisterAccount(tCtx, &RegisterRequest{
		Number:     "+15551111111",
		Password:   "pw",
		DeviceName: "dev",
	})
	if err != nil {
		t.Fatalf("first RegisterAccount failed: %v", err)
	}

	// Second registration should fail.
	_, err = svc.RegisterAccount(tCtx, &RegisterRequest{
		Number:     "+15552222222",
		Password:   "pw",
		DeviceName: "dev",
	})
	if err == nil {
		t.Fatal("expected quota exceeded error")
	}
	var target *AccountQuotaExceededError
	if !errors.As(err, &target) {
		t.Fatalf("expected AccountQuotaExceededError, got %T: %v", err, err)
	}
}

func TestSignalService_GetAccount(t *testing.T) {
	svc, _ := newEnabledService(t)
	provisionTestTenant(t, svc, "getacc-tenant")
	ctx := tenantCtx("getacc-tenant")

	resp, err := svc.RegisterAccount(ctx, &RegisterRequest{
		Number:     "+15553334444",
		Password:   "pw",
		DeviceName: "dev",
	})
	if err != nil {
		t.Fatalf("RegisterAccount failed: %v", err)
	}

	account, err := svc.GetAccount(ctx, resp.UUID)
	if err != nil {
		t.Fatalf("GetAccount failed: %v", err)
	}
	if account.UUID != resp.UUID {
		t.Fatalf("expected UUID %s, got %s", resp.UUID, account.UUID)
	}
	if account.Number != "+15553334444" {
		t.Fatalf("expected number +15553334444, got %s", account.Number)
	}
}

func TestSignalService_DeleteAccount(t *testing.T) {
	svc, _ := newEnabledService(t)
	provisionTestTenant(t, svc, "delacc-tenant")
	ctx := tenantCtx("delacc-tenant")

	resp, err := svc.RegisterAccount(ctx, &RegisterRequest{
		Number:     "+15555556666",
		Password:   "pw",
		DeviceName: "dev",
	})
	if err != nil {
		t.Fatalf("RegisterAccount failed: %v", err)
	}

	if err := svc.DeleteAccount(ctx, resp.UUID); err != nil {
		t.Fatalf("DeleteAccount failed: %v", err)
	}

	_, err = svc.GetAccount(ctx, resp.UUID)
	if err == nil {
		t.Fatal("expected error after deleting account")
	}
}

func TestSignalService_SendMessage(t *testing.T) {
	svc, _ := newEnabledService(t)
	provisionTestTenant(t, svc, "msg-tenant")
	ctx := tenantCtx("msg-tenant")

	resp, err := svc.RegisterAccount(ctx, &RegisterRequest{
		Number:     "+15557778888",
		Password:   "pw",
		DeviceName: "dev",
	})
	if err != nil {
		t.Fatalf("RegisterAccount failed: %v", err)
	}

	err = svc.SendMessage(ctx, &SendMessageRequest{
		DestinationUUID: resp.UUID,
		DeviceID:        1,
		Content:         []byte("hello"),
		Type:            int(messages.EnvelopeTypeCiphertext),
		Timestamp:       12345,
	})
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}
}

func TestSignalService_GetMessages(t *testing.T) {
	svc, _ := newEnabledService(t)
	provisionTestTenant(t, svc, "getmsg-tenant")
	ctx := tenantCtx("getmsg-tenant")

	resp, err := svc.RegisterAccount(ctx, &RegisterRequest{
		Number:     "+15559990000",
		Password:   "pw",
		DeviceName: "dev",
	})
	if err != nil {
		t.Fatalf("RegisterAccount failed: %v", err)
	}

	err = svc.SendMessage(ctx, &SendMessageRequest{
		DestinationUUID: resp.UUID,
		DeviceID:        1,
		Content:         []byte("test-message"),
		Type:            int(messages.EnvelopeTypeCiphertext),
		Timestamp:       67890,
	})
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	envelopes, err := svc.GetMessages(ctx, resp.UUID, 1)
	if err != nil {
		t.Fatalf("GetMessages failed: %v", err)
	}
	if len(envelopes) != 1 {
		t.Fatalf("expected 1 message, got %d", len(envelopes))
	}
	if string(envelopes[0].Content) != "test-message" {
		t.Fatalf("expected content test-message, got %s", string(envelopes[0].Content))
	}
}

func TestSignalService_GetMessages_Empty(t *testing.T) {
	svc, _ := newEnabledService(t)
	provisionTestTenant(t, svc, "emptymsg-tenant")
	ctx := tenantCtx("emptymsg-tenant")

	resp, err := svc.RegisterAccount(ctx, &RegisterRequest{
		Number:     "+15551112222",
		Password:   "pw",
		DeviceName: "dev",
	})
	if err != nil {
		t.Fatalf("RegisterAccount failed: %v", err)
	}

	envelopes, err := svc.GetMessages(ctx, resp.UUID, 1)
	if err != nil {
		t.Fatalf("GetMessages failed: %v", err)
	}
	if len(envelopes) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(envelopes))
	}
}

func TestSignalService_StorePreKeys(t *testing.T) {
	svc, _ := newEnabledService(t)
	provisionTestTenant(t, svc, "keys-tenant")
	ctx := tenantCtx("keys-tenant")

	resp, err := svc.RegisterAccount(ctx, &RegisterRequest{
		Number:     "+15553330000",
		Password:   "pw",
		DeviceName: "dev",
	})
	if err != nil {
		t.Fatalf("RegisterAccount failed: %v", err)
	}

	bundle := &keys.SetKeysRequest{
		SignedPreKey: &keys.SignedPreKey{
			KeyID:     1,
			PublicKey: []byte("signed-pub-key"),
			Signature: []byte("signed-sig"),
		},
		PreKeys: []*keys.PreKey{
			{KeyID: 1, PublicKey: []byte("prekey-1")},
			{KeyID: 2, PublicKey: []byte("prekey-2")},
		},
	}

	if err := svc.StorePreKeys(ctx, resp.UUID, 1, bundle); err != nil {
		t.Fatalf("StorePreKeys failed: %v", err)
	}
}

func TestSignalService_GetPreKeys(t *testing.T) {
	svc, _ := newEnabledService(t)
	provisionTestTenant(t, svc, "getkeys-tenant")
	ctx := tenantCtx("getkeys-tenant")

	resp, err := svc.RegisterAccount(ctx, &RegisterRequest{
		Number:     "+15554440000",
		Password:   "pw",
		DeviceName: "dev",
	})
	if err != nil {
		t.Fatalf("RegisterAccount failed: %v", err)
	}

	bundle := &keys.SetKeysRequest{
		SignedPreKey: &keys.SignedPreKey{
			KeyID:     10,
			PublicKey: []byte("signed-pub"),
			Signature: []byte("signed-sig"),
		},
	}
	if err := svc.StorePreKeys(ctx, resp.UUID, 1, bundle); err != nil {
		t.Fatalf("StorePreKeys failed: %v", err)
	}

	item, err := svc.GetPreKeys(ctx, resp.UUID, 1)
	if err != nil {
		t.Fatalf("GetPreKeys failed: %v", err)
	}
	if item == nil {
		t.Fatal("expected non-nil PreKeyResponseItem")
	}
	if item.SignedPreKey == nil {
		t.Fatal("expected non-nil SignedPreKey in response")
	}
	if item.SignedPreKey.KeyID != 10 {
		t.Fatalf("expected SignedPreKey KeyID 10, got %d", item.SignedPreKey.KeyID)
	}
}

func TestSignalService_NoTenantContext(t *testing.T) {
	svc, _ := newEnabledService(t)
	provisionTestTenant(t, svc, "ctx-tenant")

	// Use context without tenant.
	ctx := context.Background()

	_, err := svc.RegisterAccount(ctx, &RegisterRequest{
		Number:     "+15550000000",
		Password:   "pw",
		DeviceName: "dev",
	})
	if err == nil {
		t.Fatal("expected error when no tenant in context")
	}
	var target *TenantNotFoundError
	if !errors.As(err, &target) {
		t.Fatalf("expected TenantNotFoundError, got %T: %v", err, err)
	}
}

func TestSignalService_NoTenantContext_GetAccount(t *testing.T) {
	svc, _ := newEnabledService(t)
	provisionTestTenant(t, svc, "ctx2-tenant")

	_, err := svc.GetAccount(context.Background(), "some-uuid")
	if err == nil {
		t.Fatal("expected error when no tenant in context")
	}
}

func TestExtractTenantID(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		expected string
	}{
		{
			name:     "valid key",
			key:      "signal/tenants/my-tenant/config",
			expected: "my-tenant",
		},
		{
			name:     "valid key with dashes",
			key:      "signal/tenants/tenant-with-dashes-123/config",
			expected: "tenant-with-dashes-123",
		},
		{
			name:     "key too short",
			key:      "signal/tenants//config",
			expected: "",
		},
		{
			name:     "empty key",
			key:      "",
			expected: "",
		},
		{
			name:     "prefix only",
			key:      "signal/tenants/",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTenantID(tt.key)
			if got != tt.expected {
				t.Fatalf("extractTenantID(%q) = %q, want %q", tt.key, got, tt.expected)
			}
		})
	}
}

func TestExtractTenantID_MinimalValid(t *testing.T) {
	got := extractTenantID("signal/tenants/x/config")
	if got != "x" {
		t.Fatalf("expected x, got %q", got)
	}
}

func TestSignalService_Stop(t *testing.T) {
	svc, _ := newEnabledService(t)
	if err := svc.Stop(context.Background()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestSignalService_GetTenantConfig_EmptyID(t *testing.T) {
	svc, _ := newEnabledService(t)

	_, err := svc.GetTenantConfig(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty tenant ID")
	}
	var target *InvalidConfigError
	if !errors.As(err, &target) {
		t.Fatalf("expected InvalidConfigError, got %T: %v", err, err)
	}
}

func TestSignalService_DeprovisionTenant_EmptyID(t *testing.T) {
	svc, _ := newEnabledService(t)

	err := svc.DeprovisionTenant(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty tenant ID")
	}
	var target *InvalidConfigError
	if !errors.As(err, &target) {
		t.Fatalf("expected InvalidConfigError, got %T: %v", err, err)
	}
}

func TestSignalService_GetTenantStores_DisabledService(t *testing.T) {
	kv := newServiceTestKVClient()
	log := newTestLogger(t)
	svc := NewSignalService(kv, log, &SignalConfig{Enabled: false})

	_, err := svc.GetAccount(tenantCtx("any-tenant"), "some-uuid")
	if err == nil {
		t.Fatal("expected error when service is disabled")
	}
	var target *SignalNotEnabledError
	if !errors.As(err, &target) {
		t.Fatalf("expected SignalNotEnabledError, got %T: %v", err, err)
	}
}

func TestSignalService_GetTenantStores_DisabledTenant(t *testing.T) {
	svc, _ := newEnabledService(t)
	ctx := context.Background()

	// Provision with disabled tenant config.
	cfg := &SignalTenantConfig{
		Enabled:     false,
		MaxAccounts: 10,
		MaxDevices:  3,
	}
	if err := svc.ProvisionTenant(ctx, "disabled-tenant", cfg); err != nil {
		t.Fatalf("ProvisionTenant failed: %v", err)
	}

	_, err := svc.GetAccount(tenantCtx("disabled-tenant"), "some-uuid")
	if err == nil {
		t.Fatal("expected error when tenant is disabled")
	}
	var target *SignalNotEnabledError
	if !errors.As(err, &target) {
		t.Fatalf("expected SignalNotEnabledError, got %T: %v", err, err)
	}
}

func TestSignalService_DeleteAccount_NoTenant(t *testing.T) {
	svc, _ := newEnabledService(t)
	provisionTestTenant(t, svc, "delacc2-tenant")

	err := svc.DeleteAccount(context.Background(), "some-uuid")
	if err == nil {
		t.Fatal("expected error when no tenant in context")
	}
}

func TestSignalService_SendMessage_NoTenant(t *testing.T) {
	svc, _ := newEnabledService(t)
	provisionTestTenant(t, svc, "msg2-tenant")

	err := svc.SendMessage(context.Background(), &SendMessageRequest{
		DestinationUUID: "uuid",
		DeviceID:        1,
		Content:         []byte("data"),
		Type:            1,
		Timestamp:       1,
	})
	if err == nil {
		t.Fatal("expected error when no tenant in context")
	}
}

func TestSignalService_GetMessages_NoTenant(t *testing.T) {
	svc, _ := newEnabledService(t)

	_, err := svc.GetMessages(context.Background(), "uuid", 1)
	if err == nil {
		t.Fatal("expected error when no tenant in context")
	}
}

func TestSignalService_StorePreKeys_NoTenant(t *testing.T) {
	svc, _ := newEnabledService(t)

	bundle := &keys.SetKeysRequest{
		SignedPreKey: &keys.SignedPreKey{
			KeyID:     1,
			PublicKey: []byte("key"),
			Signature: []byte("sig"),
		},
	}
	err := svc.StorePreKeys(context.Background(), "uuid", 1, bundle)
	if err == nil {
		t.Fatal("expected error when no tenant in context")
	}
}

func TestSignalService_GetPreKeys_NoTenant(t *testing.T) {
	svc, _ := newEnabledService(t)

	_, err := svc.GetPreKeys(context.Background(), "uuid", 1)
	if err == nil {
		t.Fatal("expected error when no tenant in context")
	}
}

func TestSignalService_Start_LoadsInvalidConfig(t *testing.T) {
	kv := newServiceTestKVClient()
	log := newTestLogger(t)

	// Store invalid JSON as a tenant config.
	key := tenantConfigKeyPrefix + "bad-config-tenant" + tenantConfigKeySuffix
	kv.data[key] = []byte("not-valid-json")

	svc := NewSignalService(kv, log, &SignalConfig{Enabled: true})
	// Start should not fail; it logs and skips invalid configs.
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// The bad config should not be loaded.
	_, err := svc.GetTenantConfig(context.Background(), "bad-config-tenant")
	if err == nil {
		t.Fatal("expected error for bad config tenant")
	}
}

func TestSignalService_ProvisionTenant_MarshalSuccess(t *testing.T) {
	svc, kv := newEnabledService(t)
	ctx := context.Background()

	cfg := &SignalTenantConfig{
		Enabled:          true,
		MaxAccounts:      200,
		MaxDevices:       4,
		MessageRetention: "14d",
		FIDO2Enabled:     true,
		AllowFIDO2Only:   true,
	}
	if err := svc.ProvisionTenant(ctx, "full-tenant", cfg); err != nil {
		t.Fatalf("ProvisionTenant failed: %v", err)
	}

	// Verify persisted in KV store.
	key := tenantConfigKeyPrefix + "full-tenant" + tenantConfigKeySuffix
	data, ok := kv.data[key]
	if !ok {
		t.Fatal("expected tenant config to be persisted in KV store")
	}

	var loaded SignalTenantConfig
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("failed to unmarshal persisted config: %v", err)
	}
	if loaded.MaxAccounts != 200 {
		t.Fatalf("expected persisted MaxAccounts 200, got %d", loaded.MaxAccounts)
	}
}

func TestSignalService_GetTenantStores_TenantNotProvisioned(t *testing.T) {
	svc, _ := newEnabledService(t)

	// Tenant in context but not provisioned.
	ctx := tenantCtx("unprovisioned-tenant")
	_, err := svc.GetAccount(ctx, "some-uuid")
	if err == nil {
		t.Fatal("expected error for unprovisioned tenant")
	}
	var target *TenantNotFoundError
	if !errors.As(err, &target) {
		t.Fatalf("expected TenantNotFoundError, got %T: %v", err, err)
	}
}
