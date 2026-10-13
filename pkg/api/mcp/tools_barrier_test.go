//go:build mcp

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

package mcp

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// fakeBarrierService — deterministic in-process BarrierServicer for tests.
// =============================================================================

// fakeBarrierService implements service.BarrierServicer with scripted
// responses. Production-grade enough for unit tests; not a stub of real
// barrier behaviour.
type fakeBarrierService struct {
	// scripted return values
	status          *service.BarrierStatusInfo
	statusErr       error
	initErr         error
	unsealErr       error
	sealErr         error
	shamirInit      *service.ShamirInitInfo
	shamirInitErr   error
	unsealShare     *service.UnsealShareProgress
	unsealShareErr  error
	unsealSharesErr error
	rekey           *service.ShamirInitInfo
	rekeyErr        error
	recoveryKeys    *service.RecoveryKeysInfo
	recoveryKeysErr error
	recoverErr      error
	deleteRecErr    error
	hasRecoveryKeys bool
	hasRecoveryErr  error
	rootToken       *service.RootTokenInfo
	rootTokenErr    error

	enableEncryptionErr error

	// observed arguments
	gotInitSecret        string
	gotUnsealSecret      string
	gotShamirThr         int
	gotShamirTotal       int
	gotShamirSecret      string
	gotShare             string
	gotShares            []string
	gotRekeyThr          int
	gotRekeyTotal        int
	gotRecGenThr         int
	gotRecGenTotal       int
	gotRecKeys           []string
	gotRootShares        []string
	gotEnableEncSecret   []byte
	gotEnableEncTenantID string
	gotEnableEncOpts     qrdbsdk.MigrationOptions
}

func (f *fakeBarrierService) BarrierStatus(_ context.Context) (*service.BarrierStatusInfo, error) {
	return f.status, f.statusErr
}

func (f *fakeBarrierService) BarrierInitialize(_ context.Context, secret []byte) error {
	f.gotInitSecret = string(secret)
	return f.initErr
}

func (f *fakeBarrierService) BarrierUnseal(_ context.Context, secret []byte) error {
	f.gotUnsealSecret = string(secret)
	return f.unsealErr
}

func (f *fakeBarrierService) BarrierSeal(_ context.Context) error {
	return f.sealErr
}

func (f *fakeBarrierService) BarrierInitializeShamir(_ context.Context, secret []byte, threshold, total int) (*service.ShamirInitInfo, error) {
	f.gotShamirSecret = string(secret)
	f.gotShamirThr = threshold
	f.gotShamirTotal = total
	return f.shamirInit, f.shamirInitErr
}

func (f *fakeBarrierService) BarrierUnsealWithShare(_ context.Context, share string) (*service.UnsealShareProgress, error) {
	f.gotShare = share
	return f.unsealShare, f.unsealShareErr
}

func (f *fakeBarrierService) BarrierUnsealWithShares(_ context.Context, shares []string) error {
	f.gotShares = append([]string(nil), shares...)
	return f.unsealSharesErr
}

func (f *fakeBarrierService) BarrierRekey(_ context.Context, threshold, total int) (*service.ShamirInitInfo, error) {
	f.gotRekeyThr = threshold
	f.gotRekeyTotal = total
	return f.rekey, f.rekeyErr
}

func (f *fakeBarrierService) BarrierGenerateRecoveryKeys(_ context.Context, threshold, total int) (*service.RecoveryKeysInfo, error) {
	f.gotRecGenThr = threshold
	f.gotRecGenTotal = total
	return f.recoveryKeys, f.recoveryKeysErr
}

func (f *fakeBarrierService) BarrierRecoverWithKeys(_ context.Context, keys []string) error {
	f.gotRecKeys = append([]string(nil), keys...)
	return f.recoverErr
}

func (f *fakeBarrierService) BarrierDeleteRecoveryKeys(_ context.Context) error {
	return f.deleteRecErr
}

func (f *fakeBarrierService) BarrierHasRecoveryKeys(_ context.Context) (bool, error) {
	return f.hasRecoveryKeys, f.hasRecoveryErr
}

func (f *fakeBarrierService) BarrierGenerateRootToken(_ context.Context, shares []string) (*service.RootTokenInfo, error) {
	f.gotRootShares = append([]string(nil), shares...)
	return f.rootToken, f.rootTokenErr
}

func (f *fakeBarrierService) EnableEncryption(_ context.Context, tenantID string, opts qrdbsdk.MigrationOptions) error {
	f.gotEnableEncTenantID = tenantID
	f.gotEnableEncOpts = opts
	// The credential is protected memory the tool destroys once this call
	// returns, so the plaintext has to be read out from in here.
	f.gotEnableEncSecret = nil
	if opts.Credentials.Secret != nil {
		if plain, err := opts.Credentials.Secret.Open(); err == nil {
			defer plain.Destroy()
			if b, openErr := plain.Open(); openErr == nil {
				f.gotEnableEncSecret = append([]byte(nil), b...)
			}
		}
	}
	return f.enableEncryptionErr
}

// =============================================================================
// fakeGovernancer — deterministic mcpTenantGovernancer for tests.
// =============================================================================

// fakeGovernancer implements mcpTenantGovernancer with scripted responses.
// Each instance captures the arguments from the most recent call so subtests
// can assert correct argument propagation without shared state across calls.
type fakeGovernancer struct {
	updateQuotasErr       error
	updateEntitlementsErr error

	// observed arguments from the most recent call
	gotTenantID     string
	gotQuotas       qrdbsdk.Quotas
	gotEntitlements qrdbsdk.Entitlements
}

func (f *fakeGovernancer) UpdateEngineQuotas(_ context.Context, id string, q qrdbsdk.Quotas) error {
	f.gotTenantID = id
	f.gotQuotas = q
	return f.updateQuotasErr
}

func (f *fakeGovernancer) UpdateEngineEntitlements(_ context.Context, id string, e qrdbsdk.Entitlements) error {
	f.gotTenantID = id
	f.gotEntitlements = e
	return f.updateEntitlementsErr
}

// newBarrierTestRegistry builds a ToolRegistry wired to a fake barrier
// service. Returns both so individual subtests can inspect observed args.
func newBarrierTestRegistry(t *testing.T, fake service.BarrierServicer) *ToolRegistry {
	t.Helper()
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test-barrier",
	})
	require.NoError(t, err)
	r := NewToolRegistry(&MockQuicBaaS{}, log)
	r.SetBarrierService(fake)
	return r
}

// TestBarrierToolsRegistered asserts every documented barrier tool is present
// in the registry's tool list.
func TestCredentialArg_CopiesTheStringIntoBytes(t *testing.T) {
	args := map[string]interface{}{"secret": "s3cr3t ✓"}

	got := credentialArg(args, "secret")

	assert.Equal(t, []byte("s3cr3t ✓"), got)
}

func TestCredentialArg_AbsentOrNonStringIsNil(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"absent":     {},
		"empty":      {"secret": ""},
		"non-string": {"secret": 42},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Nil(t, credentialArg(args, "secret"))
		})
	}
}

func TestBarrierToolsRegistered(t *testing.T) {
	r := newBarrierTestRegistry(t, &fakeBarrierService{})
	got := map[string]bool{}
	for _, td := range r.ListTools() {
		got[td.Name] = true
	}
	for name := range barrierToolNames {
		assert.Truef(t, got[name], "tool %q not registered", name)
	}
}

// TestBarrierToolsExecuteSuccess walks every barrier tool through a happy
// path and verifies arguments + responses.
func TestBarrierToolsExecuteSuccess(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	fake := &fakeBarrierService{
		status: &service.BarrierStatusInfo{
			Sealed:         false,
			Strategy:       "shamir",
			HardwareBacked: true,
			InitializedAt:  now,
		},
		shamirInit: &service.ShamirInitInfo{
			Threshold:   2,
			TotalShares: 3,
			ShareCount:  3,
		},
		unsealShare: &service.UnsealShareProgress{Required: 2, Submitted: 1, Complete: false},
		rekey:       &service.ShamirInitInfo{Threshold: 2, TotalShares: 2, ShareCount: 2},
		recoveryKeys: &service.RecoveryKeysInfo{
			Threshold:  2,
			Total:      2,
			ShareCount: 2,
		},
		hasRecoveryKeys: true,
		rootToken:       &service.RootTokenInfo{Token: "tok-123", CreatedAt: now},
	}
	r := newBarrierTestRegistry(t, fake)
	ctx := context.Background()

	t.Run("status", func(t *testing.T) {
		out, err := r.ExecuteTool(ctx, "qrdb_barrier_status", nil)
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.Equal(t, "shamir", m["strategy"])
		assert.Equal(t, false, m["sealed"])
		assert.Equal(t, true, m["hardware_backed"])
		assert.Equal(t, "2026-01-01T12:00:00Z", m["initialized_at"])
	})

	t.Run("initialize", func(t *testing.T) {
		out, err := r.ExecuteTool(ctx, "qrdb_barrier_initialize", map[string]interface{}{"secret": "s1"})
		require.NoError(t, err)
		assert.Equal(t, "s1", fake.gotInitSecret)
		assert.Equal(t, "initialized", out.(map[string]interface{})["status"])
	})

	t.Run("unseal", func(t *testing.T) {
		_, err := r.ExecuteTool(ctx, "qrdb_barrier_unseal", map[string]interface{}{"secret": "s2"})
		require.NoError(t, err)
		assert.Equal(t, "s2", fake.gotUnsealSecret)
	})

	t.Run("seal", func(t *testing.T) {
		out, err := r.ExecuteTool(ctx, "qrdb_barrier_seal", nil)
		require.NoError(t, err)
		assert.Equal(t, "sealed", out.(map[string]interface{})["status"])
	})

	t.Run("shamir_initialize", func(t *testing.T) {
		out, err := r.ExecuteTool(ctx, "qrdb_barrier_shamir_initialize", map[string]interface{}{
			"secret":       "boot",
			"threshold":    2,
			"total_shares": 3,
		})
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.Equal(t, 2, m["threshold"])
		assert.Equal(t, 3, m["total_shares"])
		assert.Equal(t, 3, m["share_count"])
		// Share-free contract: share material must never appear in tool output.
		assert.NotContains(t, m, "shares")
		assert.Equal(t, "boot", fake.gotShamirSecret)
	})

	t.Run("shamir_unseal_share", func(t *testing.T) {
		out, err := r.ExecuteTool(ctx, "qrdb_barrier_shamir_unseal_share", map[string]interface{}{"share": "abc"})
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.Equal(t, 2, m["required"])
		assert.Equal(t, 1, m["submitted"])
		assert.Equal(t, false, m["complete"])
		assert.Equal(t, "abc", fake.gotShare)
	})

	t.Run("shamir_unseal_shares_array", func(t *testing.T) {
		_, err := r.ExecuteTool(ctx, "qrdb_barrier_shamir_unseal_shares", map[string]interface{}{
			"shares": []interface{}{"x", "y", "z"},
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"x", "y", "z"}, fake.gotShares)
	})

	t.Run("shamir_unseal_shares_csv", func(t *testing.T) {
		_, err := r.ExecuteTool(ctx, "qrdb_barrier_shamir_unseal_shares", map[string]interface{}{
			"shares": "p, q ,r",
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"p", "q", "r"}, fake.gotShares)
	})

	t.Run("rekey", func(t *testing.T) {
		out, err := r.ExecuteTool(ctx, "qrdb_barrier_rekey", map[string]interface{}{
			"threshold": 2,
			"total":     2,
		})
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.Equal(t, 2, m["threshold"])
		assert.Equal(t, 2, m["total_shares"])
		assert.Equal(t, 2, m["share_count"])
		// Share-free contract: share material must never appear in tool output.
		assert.NotContains(t, m, "shares")
		assert.Equal(t, 2, fake.gotRekeyThr)
		assert.Equal(t, 2, fake.gotRekeyTotal)
	})

	t.Run("recovery_generate", func(t *testing.T) {
		out, err := r.ExecuteTool(ctx, "qrdb_barrier_recovery_generate", map[string]interface{}{
			"threshold": 2,
			"total":     2,
		})
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.Equal(t, 2, m["threshold"])
		assert.Equal(t, 2, m["total"])
		assert.Equal(t, 2, m["share_count"])
		// Share-free contract: recovery key material must never appear in tool output.
		assert.NotContains(t, m, "keys")
		assert.Equal(t, 2, fake.gotRecGenThr)
		assert.Equal(t, 2, fake.gotRecGenTotal)
	})

	t.Run("recovery_recover", func(t *testing.T) {
		_, err := r.ExecuteTool(ctx, "qrdb_barrier_recovery_recover", map[string]interface{}{
			"keys": []interface{}{"r1", "r2"},
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"r1", "r2"}, fake.gotRecKeys)
	})

	t.Run("recovery_delete", func(t *testing.T) {
		_, err := r.ExecuteTool(ctx, "qrdb_barrier_recovery_delete", nil)
		require.NoError(t, err)
	})

	t.Run("recovery_status", func(t *testing.T) {
		out, err := r.ExecuteTool(ctx, "qrdb_barrier_recovery_status", nil)
		require.NoError(t, err)
		assert.Equal(t, true, out.(map[string]interface{})["has_recovery_keys"])
	})

	t.Run("root_token", func(t *testing.T) {
		out, err := r.ExecuteTool(ctx, "qrdb_barrier_root_token", map[string]interface{}{
			"shares": []interface{}{"s1", "s2"},
		})
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.Equal(t, "tok-123", m["token"])
		assert.Equal(t, "2026-01-01T12:00:00Z", m["created_at"])
		assert.Equal(t, []string{"s1", "s2"}, fake.gotRootShares)
	})

	t.Run("enable_encryption_system", func(t *testing.T) {
		out, err := r.ExecuteTool(ctx, "qrdb_barrier_enable_encryption", map[string]interface{}{
			"target_strategy": "passphrase",
			"passphrase":      "hunter2",
		})
		require.NoError(t, err)
		assert.Equal(t, "encryption enabled", out.(map[string]interface{})["status"])
		// System barrier: empty tenantID.
		assert.Equal(t, "", fake.gotEnableEncTenantID)
		assert.Equal(t, qrdbsdk.StrategyID("passphrase"), fake.gotEnableEncOpts.TargetStrategy)
		assert.Equal(t, qrdbsdk.MechanismSwap, fake.gotEnableEncOpts.Mechanism)
		assert.Equal(t, qrdbsdk.AvailabilityOnline, fake.gotEnableEncOpts.Availability)
		assert.Equal(t, []byte("hunter2"), fake.gotEnableEncSecret)
	})

	t.Run("enable_encryption_tenant", func(t *testing.T) {
		out, err := r.ExecuteTool(ctx, "qrdb_tenant_barrier_enable_encryption", map[string]interface{}{
			"tenant_id":       "t1",
			"target_strategy": "passphrase",
			"passphrase":      "hunter2",
			"mechanism":       "copy",
		})
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.Equal(t, "encryption enabled", m["status"])
		assert.Equal(t, "t1", m["tenant_id"])
		assert.Equal(t, "t1", fake.gotEnableEncTenantID)
		assert.Equal(t, qrdbsdk.StrategyID("passphrase"), fake.gotEnableEncOpts.TargetStrategy)
		assert.Equal(t, qrdbsdk.MigrationMechanism("copy"), fake.gotEnableEncOpts.Mechanism)
	})

	t.Run("update_tenant_quotas", func(t *testing.T) {
		fakeGov := &fakeGovernancer{}
		r.SetTenantGovernance(fakeGov)
		out, err := r.ExecuteTool(ctx, "qrdb_tenant_update_quotas", map[string]interface{}{
			"tenant_id":            "t-quotas",
			"max_databases":        10,
			"max_keys":             500,
			"system_barrier_quota": -1,
		})
		require.NoError(t, err)
		assert.Equal(t, "ok", out.(map[string]interface{})["status"])
		assert.Equal(t, "t-quotas", fakeGov.gotTenantID)
		assert.Equal(t, int64(10), fakeGov.gotQuotas.MaxDatabases)
		assert.Equal(t, int64(500), fakeGov.gotQuotas.MaxKeys)
		assert.Equal(t, int64(-1), fakeGov.gotQuotas.SystemBarrierQuota)
	})

	t.Run("update_tenant_entitlements", func(t *testing.T) {
		fakeGov := &fakeGovernancer{}
		r.SetTenantGovernance(fakeGov)
		out, err := r.ExecuteTool(ctx, "qrdb_tenant_update_entitlements", map[string]interface{}{
			"tenant_id":                    "t-ents",
			"allowed_isolation_modes":      []interface{}{"barrier", "raft"},
			"allow_dedicated_raft":         true,
			"allow_volume_storage":         false,
			"allow_system_managed_barrier": true,
			"allow_dedicated_wal":          true,
			"allowed_barrier_modes":        []interface{}{"passphrase", "shamir"},
		})
		require.NoError(t, err)
		assert.Equal(t, "ok", out.(map[string]interface{})["status"])
		assert.Equal(t, "t-ents", fakeGov.gotTenantID)
		assert.Equal(t, []string{"barrier", "raft"}, fakeGov.gotEntitlements.AllowedIsolationModes)
		assert.True(t, fakeGov.gotEntitlements.AllowDedicatedRaft)
		assert.False(t, fakeGov.gotEntitlements.AllowVolumeStorage)
		assert.True(t, fakeGov.gotEntitlements.AllowSystemManagedBarrier)
		assert.True(t, fakeGov.gotEntitlements.AllowDedicatedWAL)
		assert.Equal(t, []string{"passphrase", "shamir"}, fakeGov.gotEntitlements.AllowedBarrierModes)
	})
}

// TestBarrierToolsValidation covers required-argument and threshold checks.
func TestBarrierToolsValidation(t *testing.T) {
	r := newBarrierTestRegistry(t, &fakeBarrierService{
		status: &service.BarrierStatusInfo{},
	})

	type tc struct {
		name string
		tool string
		args map[string]interface{}
		// expectedErr is the error type the tool should return.
		expectedErr error
	}

	cases := []tc{
		{
			name:        "unseal requires secret",
			tool:        "qrdb_barrier_unseal",
			args:        map[string]interface{}{},
			expectedErr: &BarrierThresholdInvalidError{},
		},
		{
			name:        "shamir_initialize threshold zero",
			tool:        "qrdb_barrier_shamir_initialize",
			args:        map[string]interface{}{"threshold": 0, "total_shares": 3},
			expectedErr: &BarrierThresholdInvalidError{},
		},
		{
			name:        "shamir_initialize total zero",
			tool:        "qrdb_barrier_shamir_initialize",
			args:        map[string]interface{}{"threshold": 1, "total_shares": 0},
			expectedErr: &BarrierThresholdInvalidError{},
		},
		{
			name:        "shamir_initialize threshold>total",
			tool:        "qrdb_barrier_shamir_initialize",
			args:        map[string]interface{}{"threshold": 5, "total_shares": 3},
			expectedErr: &BarrierThresholdInvalidError{},
		},
		{
			name:        "shamir_unseal_share missing",
			tool:        "qrdb_barrier_shamir_unseal_share",
			args:        map[string]interface{}{},
			expectedErr: &BarrierShareRequiredError{},
		},
		{
			name:        "shamir_unseal_shares missing",
			tool:        "qrdb_barrier_shamir_unseal_shares",
			args:        map[string]interface{}{},
			expectedErr: &BarrierSharesRequiredError{},
		},
		{
			name:        "rekey threshold zero",
			tool:        "qrdb_barrier_rekey",
			args:        map[string]interface{}{"threshold": 0, "total": 3},
			expectedErr: &BarrierThresholdInvalidError{},
		},
		{
			name:        "recovery_generate threshold>total",
			tool:        "qrdb_barrier_recovery_generate",
			args:        map[string]interface{}{"threshold": 5, "total": 3},
			expectedErr: &BarrierThresholdInvalidError{},
		},
		{
			name:        "recovery_recover missing keys",
			tool:        "qrdb_barrier_recovery_recover",
			args:        map[string]interface{}{},
			expectedErr: &BarrierRecoveryKeysRequiredError{},
		},
		{
			name:        "root_token missing shares",
			tool:        "qrdb_barrier_root_token",
			args:        map[string]interface{}{},
			expectedErr: &BarrierSharesRequiredError{},
		},
		{
			name:        "enable_encryption missing strategy",
			tool:        "qrdb_barrier_enable_encryption",
			args:        map[string]interface{}{},
			expectedErr: &BarrierTargetStrategyRequiredError{},
		},
		{
			name:        "tenant_enable_encryption missing tenant_id",
			tool:        "qrdb_tenant_barrier_enable_encryption",
			args:        map[string]interface{}{"target_strategy": "passphrase"},
			expectedErr: &BarrierTenantIDRequiredError{},
		},
		{
			name:        "tenant_enable_encryption missing strategy",
			tool:        "qrdb_tenant_barrier_enable_encryption",
			args:        map[string]interface{}{"tenant_id": "t1"},
			expectedErr: &BarrierTargetStrategyRequiredError{},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := r.ExecuteTool(context.Background(), c.tool, c.args)
			require.Error(t, err)
			assert.IsTypef(t, c.expectedErr, err, "wrong error type for %s: got %T", c.tool, err)
		})
	}

	// Governance tool validation: argument checks are only reached once the
	// mcpTenantGovernancer is wired (otherwise the tool short-circuits with
	// GovernanceServiceUnavailableError before inspecting args).
	govRegistry := newBarrierTestRegistry(t, &fakeBarrierService{status: &service.BarrierStatusInfo{}})
	govRegistry.SetTenantGovernance(&fakeGovernancer{})
	govValidationCases := []tc{
		{
			name:        "update_quotas missing tenant_id",
			tool:        "qrdb_tenant_update_quotas",
			args:        map[string]interface{}{},
			expectedErr: &BarrierTenantIDRequiredError{},
		},
		{
			name:        "update_entitlements missing tenant_id",
			tool:        "qrdb_tenant_update_entitlements",
			args:        map[string]interface{}{},
			expectedErr: &BarrierTenantIDRequiredError{},
		},
	}
	for _, c := range govValidationCases {
		t.Run(c.name, func(t *testing.T) {
			_, err := govRegistry.ExecuteTool(context.Background(), c.tool, c.args)
			require.Error(t, err)
			assert.IsTypef(t, c.expectedErr, err, "wrong error type for %s: got %T", c.tool, err)
		})
	}
}

// TestBarrierToolsUnavailable asserts every barrier tool returns the typed
// unavailable error when neither BarrierServicer nor mcpTenantGovernancer is
// wired. Barrier tools return BarrierServiceUnavailableError; governance tools
// (qrdb_tenant_update_quotas, qrdb_tenant_update_entitlements) return
// GovernanceServiceUnavailableError. Both signal the same fail-closed condition.
func TestBarrierToolsUnavailable(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test-barrier",
	})
	require.NoError(t, err)
	r := NewToolRegistry(&MockQuicBaaS{}, log)
	// Intentionally do NOT call SetBarrierService or SetTenantGovernance.

	for name := range barrierToolNames {
		t.Run(name, func(t *testing.T) {
			_, err := r.ExecuteTool(context.Background(), name, map[string]interface{}{
				"secret":       "x",
				"share":        "x",
				"shares":       []interface{}{"x"},
				"keys":         []interface{}{"x"},
				"index":        1,
				"threshold":    1,
				"total":        1,
				"total_shares": 1,
			})
			require.Error(t, err)
			var barrierNotReady *BarrierServiceUnavailableError
			var govNotReady *GovernanceServiceUnavailableError
			isUnavailable := stderrors.As(err, &barrierNotReady) || stderrors.As(err, &govNotReady)
			assert.Truef(t, isUnavailable, "expected service unavailable error, got %T: %v", err, err)
		})
	}
}

// TestBarrierToolsPropagatesServiceError ensures executor functions surface
// errors returned by the underlying BarrierServicer unchanged so MCP error
// mapping can promote them to JSON-RPC error codes.
func TestBarrierToolsPropagatesServiceError(t *testing.T) {
	sentinel := stderrors.New("scripted-failure")
	fake := &fakeBarrierService{
		status:              &service.BarrierStatusInfo{},
		statusErr:           sentinel,
		initErr:             sentinel,
		unsealErr:           sentinel,
		sealErr:             sentinel,
		shamirInitErr:       sentinel,
		unsealShareErr:      sentinel,
		unsealSharesErr:     sentinel,
		rekeyErr:            sentinel,
		recoveryKeysErr:     sentinel,
		recoverErr:          sentinel,
		deleteRecErr:        sentinel,
		hasRecoveryErr:      sentinel,
		rootTokenErr:        sentinel,
		enableEncryptionErr: sentinel,
	}
	r := newBarrierTestRegistry(t, fake)

	// Tool-arg pairs that should reach the service layer (i.e., pass
	// argument validation) and surface the scripted sentinel error.
	calls := []struct {
		name string
		args map[string]interface{}
	}{
		{"qrdb_barrier_status", nil},
		{"qrdb_barrier_initialize", map[string]interface{}{"secret": "x"}},
		{"qrdb_barrier_unseal", map[string]interface{}{"secret": "x"}},
		{"qrdb_barrier_seal", nil},
		{"qrdb_barrier_shamir_initialize", map[string]interface{}{"threshold": 1, "total_shares": 2}},
		{"qrdb_barrier_shamir_unseal_share", map[string]interface{}{"share": "x"}},
		{"qrdb_barrier_shamir_unseal_shares", map[string]interface{}{"shares": []interface{}{"x"}}},
		{"qrdb_barrier_rekey", map[string]interface{}{"threshold": 1, "total": 2}},
		{"qrdb_barrier_recovery_generate", map[string]interface{}{"threshold": 1, "total": 2}},
		{"qrdb_barrier_recovery_recover", map[string]interface{}{"keys": []interface{}{"k"}}},
		{"qrdb_barrier_recovery_delete", nil},
		{"qrdb_barrier_recovery_status", nil},
		{"qrdb_barrier_root_token", map[string]interface{}{"shares": []interface{}{"x"}}},
		{"qrdb_barrier_enable_encryption", map[string]interface{}{"target_strategy": "passphrase"}},
		{"qrdb_tenant_barrier_enable_encryption", map[string]interface{}{"tenant_id": "t1", "target_strategy": "passphrase"}},
	}
	for _, c := range calls {
		t.Run(c.name, func(t *testing.T) {
			_, err := r.ExecuteTool(context.Background(), c.name, c.args)
			require.Error(t, err)
			assert.ErrorIs(t, err, sentinel)
		})
	}

	// Governance tools propagate errors from the mcpTenantGovernancer.
	govSentinel := stderrors.New("scripted-governance-failure")
	r.SetTenantGovernance(&fakeGovernancer{
		updateQuotasErr:       govSentinel,
		updateEntitlementsErr: govSentinel,
	})
	govCalls := []struct {
		name string
		args map[string]interface{}
	}{
		{"qrdb_tenant_update_quotas", map[string]interface{}{"tenant_id": "t1"}},
		{"qrdb_tenant_update_entitlements", map[string]interface{}{"tenant_id": "t1"}},
	}
	for _, c := range govCalls {
		t.Run(c.name, func(t *testing.T) {
			_, err := r.ExecuteTool(context.Background(), c.name, c.args)
			require.Error(t, err)
			assert.ErrorIs(t, err, govSentinel)
		})
	}
}

// TestBarrierToolsErrorMessages locks in the human-readable error messages
// surfaced by each typed error so callers can rely on them.
func TestBarrierToolsErrorMessages(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{&BarrierServiceUnavailableError{}, "barrier service unavailable: not configured"},
		{&GovernanceServiceUnavailableError{}, "governance service unavailable: not configured"},
		{&BarrierShareRequiredError{}, "share is required"},
		{&BarrierSharesRequiredError{}, "shares is required"},
		{&BarrierRecoveryKeysRequiredError{}, "keys is required"},
		{&BarrierThresholdInvalidError{Reason: "x"}, "invalid threshold/total: x"},
		{&BarrierTargetStrategyRequiredError{}, "target_strategy is required"},
		{&BarrierTenantIDRequiredError{}, "tenant_id is required"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, c.err.Error())
	}
}

// TestBarrierStatusWiredExecutes verifies that once a BarrierServicer is wired
// into the registry (mirroring lifecycle_mcp.go's SetBarrierService call),
// qrdb_barrier_status executes against the service and returns the status map
// rather than the unavailable error.
func TestBarrierStatusWiredExecutes(t *testing.T) {
	fake := &fakeBarrierService{
		status: &service.BarrierStatusInfo{
			Sealed:         false,
			Strategy:       "passphrase",
			HardwareBacked: false,
		},
	}
	r := newBarrierTestRegistry(t, fake)

	out, err := r.ExecuteTool(context.Background(), "qrdb_barrier_status", nil)
	require.NoError(t, err)
	result, ok := out.(map[string]interface{})
	require.Truef(t, ok, "expected map result, got %T", out)
	assert.Equal(t, false, result["sealed"])
	assert.Equal(t, "passphrase", result["strategy"])
}

// TestBarrierStatusUnavailableMapsToServiceUnavailable verifies that when the
// barrier service is NOT wired, qrdb_barrier_status returns the typed
// BarrierServiceUnavailableError rather than a bare/opaque failure, so
// callers (and the unified server's tool-error text) can distinguish "no
// service" from a transient failure.
func TestBarrierStatusUnavailableMapsToServiceUnavailable(t *testing.T) {
	server, _ := newTestServer(t)
	// Intentionally do NOT call server.SetBarrierService — the surface stays
	// offline, reproducing the pre-fix quicbaasd MCP startup state.

	_, err := server.tools.ExecuteTool(context.Background(), "qrdb_barrier_status", nil)
	require.Error(t, err)

	// The unwired sentinel must still be recoverable through the QBError envelope.
	var notReady *BarrierServiceUnavailableError
	assert.Truef(t, stderrors.As(err, &notReady), "expected BarrierServiceUnavailableError, got %T", err)
}

// TestBarrierStatusWiredMapsCleanly verifies that once the service is wired the
// status call no longer produces any error to map.
func TestBarrierStatusWiredMapsCleanly(t *testing.T) {
	server, _ := newTestServer(t)
	server.SetBarrierService(&fakeBarrierService{status: &service.BarrierStatusInfo{Strategy: "shamir"}})

	out, err := server.tools.ExecuteTool(context.Background(), "qrdb_barrier_status", nil)
	require.NoError(t, err)
	result, ok := out.(map[string]interface{})
	require.Truef(t, ok, "expected map result, got %T", out)
	assert.Equal(t, "shamir", result["strategy"])
}
