//go:build quicbaas_signal

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

// Package signal integrates go-signal as a multi-tenant Signal messaging
// service into QuicBaaS. Tenant isolation is achieved by extracting the tenant
// ID from the request context and wrapping the base KV client with a
// TenantKVClient that transparently prefixes all keys.
package signal

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"sync"

	"github.com/google/uuid"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/jeremyhahn/go-signal/pkg/accounts"
	"github.com/jeremyhahn/go-signal/pkg/keys"
	"github.com/jeremyhahn/go-signal/pkg/messages"
	"github.com/jeremyhahn/go-signal/pkg/profiles"
	signalstore "github.com/jeremyhahn/go-signal/pkg/store"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

const (
	// tenantConfigKeyPrefix is the KV key prefix for tenant configurations.
	tenantConfigKeyPrefix = "signal/tenants/"

	// tenantConfigKeySuffix is appended after the tenant ID.
	tenantConfigKeySuffix = "/config"
)

// SignalService defines the multi-tenant Signal messaging service interface.
// A single instance serves all tenants, routing each request to the correct
// tenant namespace by extracting the tenant ID from the context.
type SignalService interface {
	// Account operations - tenant extracted from ctx.
	RegisterAccount(ctx context.Context, req *RegisterRequest) (*RegisterResponse, error)
	GetAccount(ctx context.Context, uuid string) (*accounts.Account, error)
	DeleteAccount(ctx context.Context, uuid string) error

	// Messaging - tenant extracted from ctx.
	SendMessage(ctx context.Context, req *SendMessageRequest) error
	GetMessages(ctx context.Context, accountUUID string, deviceID uint64) ([]*messages.Envelope, error)

	// Keys - tenant extracted from ctx.
	StorePreKeys(ctx context.Context, accountUUID string, deviceID uint64, bundle *keys.SetKeysRequest) error
	GetPreKeys(ctx context.Context, accountUUID string, deviceID uint64) (*keys.PreKeyResponseItem, error)

	// Tenant management.
	ProvisionTenant(ctx context.Context, tenantID string, cfg *SignalTenantConfig) error
	DeprovisionTenant(ctx context.Context, tenantID string) error
	GetTenantConfig(ctx context.Context, tenantID string) (*SignalTenantConfig, error)

	// Lifecycle.
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// RegisterRequest contains registration parameters.
type RegisterRequest struct {
	Number     string `json:"number"`
	Password   string `json:"password"`
	DeviceName string `json:"deviceName"`
}

// RegisterResponse contains the registered account info.
type RegisterResponse struct {
	UUID     string `json:"uuid"`
	Number   string `json:"number"`
	DeviceID uint64 `json:"deviceId"`
}

// SendMessageRequest wraps a message send operation.
type SendMessageRequest struct {
	DestinationUUID string `json:"destinationUuid"`
	DeviceID        uint64 `json:"deviceId"`
	Content         []byte `json:"content"`
	Type            int    `json:"type"`
	Timestamp       uint64 `json:"timestamp"`
}

// signalStores groups all signal store instances for a single request scope.
type signalStores struct {
	accounts *accounts.AccountsManager
	messages *messages.MessagesManager
	keys     *keys.KeysManager
	profiles profiles.ProfileStore
}

// newSignalStores creates the full set of go-signal managers backed by the
// given tenant-scoped KV adapter.
func newSignalStores(kv signalstore.KVClient, maxDevices int) *signalStores {
	accountStore := accounts.NewKVAccountStore(kv)
	messageStore := messages.NewKVMessageStore(kv)
	keyStore := keys.NewKVPreKeyStore(kv)
	profileStore := profiles.NewKVProfileStore(kv)
	return &signalStores{
		accounts: accounts.NewAccountsManager(accountStore, maxDevices),
		messages: messages.NewMessagesManager(messageStore),
		keys:     keys.NewKeysManager(keyStore),
		profiles: profileStore,
	}
}

// signalService is the concrete implementation of SignalService. A single
// instance serves all tenants using context-based routing.
type signalService struct {
	kvClient   qrdbtransport.KVClient
	logger     *logger.Logger
	cfg        *SignalConfig
	mu         sync.RWMutex
	tenantCfgs map[string]*SignalTenantConfig
}

// NewSignalService creates a new multi-tenant Signal service backed by the
// given base KV client. The service is not started until Start is called.
func NewSignalService(
	kvClient qrdbtransport.KVClient,
	log *logger.Logger,
	cfg *SignalConfig,
) SignalService {
	return &signalService{
		kvClient:   kvClient,
		logger:     log.WithField("component", "signal"),
		cfg:        cfg,
		tenantCfgs: make(map[string]*SignalTenantConfig),
	}
}

// Start initializes the Signal service and loads persisted tenant configs
// from the backing KV store.
func (s *signalService) Start(ctx context.Context) error {
	if !s.cfg.Enabled {
		s.logger.Info("signal service disabled, skipping start")
		return nil
	}
	s.logger.Info("signal service starting")
	if err := s.loadTenantConfigs(ctx); err != nil {
		return err
	}
	s.logger.Info("signal service started")
	return nil
}

// Stop gracefully shuts down the Signal service.
func (s *signalService) Stop(_ context.Context) error {
	s.logger.Info("signal service stopped")
	return nil
}

// RegisterAccount creates a new Signal account within the tenant extracted
// from ctx.
func (s *signalService) RegisterAccount(ctx context.Context, req *RegisterRequest) (*RegisterResponse, error) {
	stores, tenantCfg, err := s.getTenantStores(ctx)
	if err != nil {
		return nil, err
	}

	// Check account quota by counting existing accounts.
	accountCount, countErr := s.countAccounts(ctx)
	if countErr != nil {
		return nil, countErr
	}
	if accountCount >= tenantCfg.MaxAccounts {
		tenantID, _ := tenantctx.Lookup(ctx) // comma-ok: countAccounts above already required the binding
		return nil, &AccountQuotaExceededError{
			TenantID:    tenantID,
			MaxAccounts: tenantCfg.MaxAccounts,
		}
	}

	accountUUID := uuid.New().String()

	// Generate a real Ed25519 identity key pair for the account.
	//
	// This is Signal-protocol X3DH identity material, not PKI. It is never
	// certified by the go-xkms CA and never presented in a TLS handshake; it
	// seeds a double ratchet, and the prekey below is signed by THIS key rather
	// than by any certificate authority. Routing it through go-xkms key custody
	// would make it a platform-held key, which is the opposite of what a
	// per-account messaging identity is for. Whether this material belongs under
	// go-xkms custody at all is a separate architecture question
	// (go-quicbaas#910).
	//
	// local-keygen-ok: Signal-protocol identity key, not PKI — see above.
	identityPub, identityPriv, identityErr := ed25519.GenerateKey(rand.Reader)
	if identityErr != nil {
		return nil, &KeyGenerationError{
			KeyType: "identity",
			Err:     identityErr,
		}
	}

	// Generate a real Ed25519 signed pre-key pair.
	//
	// local-keygen-ok: Signal-protocol signed prekey; same reasoning as the
	// identity key above.
	spkPub, _, spkErr := ed25519.GenerateKey(rand.Reader)
	if spkErr != nil {
		return nil, &KeyGenerationError{
			KeyType: "signed-pre-key",
			Err:     spkErr,
		}
	}

	// Sign the signed pre-key public key with the identity private key.
	// This binding proves the SPK belongs to this identity.
	spkSignature := ed25519.Sign(identityPriv, spkPub)

	signedPreKey := &accounts.SignedPreKey{
		KeyID:     1,
		PublicKey: spkPub,
		Signature: spkSignature,
	}

	account := &accounts.Account{
		UUID:        accountUUID,
		Number:      req.Number,
		IdentityKey: identityPub,
		Devices: []accounts.Device{
			{
				ID:           accounts.PrimaryDeviceID,
				Name:         req.DeviceName,
				AuthToken:    req.Password,
				SignedPreKey: signedPreKey,
			},
		},
	}

	if err := stores.accounts.Create(ctx, account); err != nil {
		return nil, err
	}

	return &RegisterResponse{
		UUID:     accountUUID,
		Number:   req.Number,
		DeviceID: accounts.PrimaryDeviceID,
	}, nil
}

// GetAccount retrieves a Signal account by UUID within the tenant extracted
// from ctx.
func (s *signalService) GetAccount(ctx context.Context, accountUUID string) (*accounts.Account, error) {
	stores, _, err := s.getTenantStores(ctx)
	if err != nil {
		return nil, err
	}
	return stores.accounts.Get(ctx, accountUUID)
}

// DeleteAccount removes a Signal account and all associated data within the
// tenant extracted from ctx.
func (s *signalService) DeleteAccount(ctx context.Context, accountUUID string) error {
	stores, _, err := s.getTenantStores(ctx)
	if err != nil {
		return err
	}
	return stores.accounts.Delete(ctx, accountUUID)
}

// SendMessage stores a message envelope for delivery to the destination device
// within the tenant extracted from ctx.
func (s *signalService) SendMessage(ctx context.Context, req *SendMessageRequest) error {
	stores, _, err := s.getTenantStores(ctx)
	if err != nil {
		return err
	}

	envelope := &messages.Envelope{
		Type:            messages.EnvelopeType(req.Type),
		DestinationUUID: req.DestinationUUID,
		Content:         req.Content,
		Timestamp:       req.Timestamp,
	}

	return stores.messages.Insert(ctx, req.DestinationUUID, req.DeviceID, envelope)
}

// GetMessages retrieves pending messages for a specific device within the
// tenant extracted from ctx.
func (s *signalService) GetMessages(ctx context.Context, accountUUID string, deviceID uint64) ([]*messages.Envelope, error) {
	stores, _, err := s.getTenantStores(ctx)
	if err != nil {
		return nil, err
	}
	return stores.messages.GetMessagesForDevice(ctx, accountUUID, deviceID)
}

// StorePreKeys persists pre-key material for a device within the tenant
// extracted from ctx.
func (s *signalService) StorePreKeys(ctx context.Context, accountUUID string, deviceID uint64, bundle *keys.SetKeysRequest) error {
	stores, _, err := s.getTenantStores(ctx)
	if err != nil {
		return err
	}
	return stores.keys.StoreKeys(ctx, accountUUID, deviceID, bundle)
}

// GetPreKeys retrieves the pre-key bundle for a device within the tenant
// extracted from ctx.
func (s *signalService) GetPreKeys(ctx context.Context, accountUUID string, deviceID uint64) (*keys.PreKeyResponseItem, error) {
	stores, _, err := s.getTenantStores(ctx)
	if err != nil {
		return nil, err
	}
	return stores.keys.GetPreKeys(ctx, accountUUID, deviceID)
}

// ProvisionTenant initializes Signal for a new tenant by storing its
// configuration in the backing KV store.
func (s *signalService) ProvisionTenant(ctx context.Context, tenantID string, cfg *SignalTenantConfig) error {
	if !s.cfg.Enabled {
		return &SignalNotEnabledError{TenantID: tenantID}
	}
	if tenantID == "" {
		return &InvalidConfigError{Field: "tenantID", Reason: "must not be empty"}
	}

	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return err
	}

	s.mu.RLock()
	_, exists := s.tenantCfgs[tenantID]
	s.mu.RUnlock()
	if exists {
		return &TenantAlreadyProvisionedError{TenantID: tenantID}
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		return &InvalidConfigError{Field: "config", Reason: "failed to marshal tenant config"}
	}

	key := tenantConfigKeyPrefix + tenantID + tenantConfigKeySuffix
	if err := s.kvClient.Put(ctx, key, data); err != nil {
		return err
	}

	s.mu.Lock()
	s.tenantCfgs[tenantID] = cfg
	s.mu.Unlock()

	s.logger.WithField("tenantID", tenantID).Info("signal tenant provisioned")
	return nil
}

// DeprovisionTenant removes Signal configuration for a tenant.
func (s *signalService) DeprovisionTenant(ctx context.Context, tenantID string) error {
	if tenantID == "" {
		return &InvalidConfigError{Field: "tenantID", Reason: "must not be empty"}
	}

	s.mu.RLock()
	_, exists := s.tenantCfgs[tenantID]
	s.mu.RUnlock()
	if !exists {
		return &TenantNotFoundError{TenantID: tenantID}
	}

	key := tenantConfigKeyPrefix + tenantID + tenantConfigKeySuffix
	if err := s.kvClient.Delete(ctx, key); err != nil {
		return err
	}

	s.mu.Lock()
	delete(s.tenantCfgs, tenantID)
	s.mu.Unlock()

	s.logger.WithField("tenantID", tenantID).Info("signal tenant deprovisioned")
	return nil
}

// GetTenantConfig retrieves the Signal configuration for a tenant.
func (s *signalService) GetTenantConfig(_ context.Context, tenantID string) (*SignalTenantConfig, error) {
	if tenantID == "" {
		return nil, &InvalidConfigError{Field: "tenantID", Reason: "must not be empty"}
	}

	s.mu.RLock()
	cfg, exists := s.tenantCfgs[tenantID]
	s.mu.RUnlock()
	if !exists {
		return nil, &TenantNotFoundError{TenantID: tenantID}
	}
	return cfg, nil
}

// getTenantStores extracts the tenant ID from ctx, resolves the tenant config,
// wraps the base KV client with tenant-scoped prefixing, and returns the full
// set of signal stores for the request.
func (s *signalService) getTenantStores(ctx context.Context) (*signalStores, *SignalTenantConfig, error) {
	if !s.cfg.Enabled {
		return nil, nil, &SignalNotEnabledError{TenantID: ""}
	}

	tenantID, ok := tenantctx.Lookup(ctx)
	if !ok {
		return nil, nil, &TenantNotFoundError{TenantID: ""}
	}

	s.mu.RLock()
	tenantCfg, exists := s.tenantCfgs[tenantID]
	s.mu.RUnlock()
	if !exists {
		return nil, nil, &TenantNotFoundError{TenantID: tenantID}
	}

	if !tenantCfg.Enabled {
		return nil, nil, &SignalNotEnabledError{TenantID: tenantID}
	}

	tenantKV, err := qrdbtransport.WithTenant(s.kvClient, tenantID)
	if err != nil {
		return nil, nil, err
	}

	adapter := NewKVAdapter(tenantKV)
	stores := newSignalStores(adapter, tenantCfg.MaxDevices)
	return stores, tenantCfg, nil
}

// loadTenantConfigs reads all persisted tenant configurations from the KV
// store and populates the in-memory cache.
func (s *signalService) loadTenantConfigs(ctx context.Context) error {
	configKeys, err := s.kvClient.List(ctx, tenantConfigKeyPrefix)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, key := range configKeys {
		data, err := s.kvClient.Get(ctx, key)
		if err != nil {
			s.logger.WithField("key", key).WithError(err).Error("failed to load tenant config")
			continue
		}

		var cfg SignalTenantConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			s.logger.WithField("key", key).WithError(err).Error("failed to unmarshal tenant config")
			continue
		}

		tenantID := extractTenantID(key)
		if tenantID == "" {
			continue
		}

		s.tenantCfgs[tenantID] = &cfg
		s.logger.WithField("tenantID", tenantID).Info("loaded signal tenant config")
	}

	return nil
}

// countAccounts returns the number of accounts in the current tenant namespace.
func (s *signalService) countAccounts(ctx context.Context) (int, error) {
	tenantID, ok := tenantctx.Lookup(ctx)
	if !ok {
		return 0, &TenantNotFoundError{TenantID: ""}
	}

	tenantKV, err := qrdbtransport.WithTenant(s.kvClient, tenantID)
	if err != nil {
		return 0, err
	}

	accountKeys, err := tenantKV.List(ctx, "signal/accounts/")
	if err != nil {
		return 0, err
	}

	// Filter out index keys (by-number/) to count only primary account entries.
	count := 0
	for _, k := range accountKeys {
		if len(k) > len("signal/accounts/by-number/") &&
			k[:len("signal/accounts/by-number/")] == "signal/accounts/by-number/" {
			continue
		}
		count++
	}
	return count, nil
}

// extractTenantID parses the tenant ID from a tenant config KV key of the
// form "signal/tenants/{tenantID}/config".
func extractTenantID(key string) string {
	const prefixLen = len("signal/tenants/")
	const suffixLen = len("/config")

	if len(key) <= prefixLen+suffixLen {
		return ""
	}
	return key[prefixLen : len(key)-suffixLen]
}
