// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

// Package main: command-handler coverage for dynamic_secrets.go.
package main

import (
	"bytes"
	"context"
	stderrors "errors"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	dsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/dynamicsecrets"
)

// =============================================================================
// Mock
// =============================================================================

// mockDynamicSecretsClient implements quicbaas.DynamicSecretsClient.
type mockDynamicSecretsClient struct {
	createReq   *dsclient.CreateRoleRequest
	getReq      *dsclient.GetRoleRequest
	listReq     *dsclient.ListRolesRequest
	deleteReq   *dsclient.DeleteRoleRequest
	generateReq *dsclient.GenerateCredentialRequest
	renewReq    *dsclient.RenewLeaseRequest
	revokeReq   *dsclient.RevokeLeaseRequest
	leasesReq   *dsclient.ListLeasesRequest

	err error
}

func (m *mockDynamicSecretsClient) CreateRole(_ context.Context, req *dsclient.CreateRoleRequest) (*dsclient.CreateRoleResponse, error) {
	m.createReq = req
	if m.err != nil {
		return nil, m.err
	}
	return &dsclient.CreateRoleResponse{
		Role: &dsclient.Role{
			Name:          req.Name,
			GeneratorType: req.GeneratorType,
			Config:        req.Config,
			DefaultTTL:    req.DefaultTTL,
			MaxTTL:        req.MaxTTL,
		},
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}, nil
}

func (m *mockDynamicSecretsClient) GetRole(_ context.Context, req *dsclient.GetRoleRequest) (*dsclient.GetRoleResponse, error) {
	m.getReq = req
	if m.err != nil {
		return nil, m.err
	}
	return &dsclient.GetRoleResponse{Role: &dsclient.Role{Name: req.Name, GeneratorType: "postgres"}}, nil
}

func (m *mockDynamicSecretsClient) ListRoles(_ context.Context, req *dsclient.ListRolesRequest) (*dsclient.ListRolesResponse, error) {
	m.listReq = req
	if m.err != nil {
		return nil, m.err
	}
	return &dsclient.ListRolesResponse{
		Roles: []*dsclient.Role{{Name: "readonly-db", GeneratorType: "postgres", DefaultTTL: time.Hour, MaxTTL: 24 * time.Hour}},
		Count: 1,
	}, nil
}

func (m *mockDynamicSecretsClient) DeleteRole(_ context.Context, req *dsclient.DeleteRoleRequest) (*dsclient.DeleteRoleResponse, error) {
	m.deleteReq = req
	if m.err != nil {
		return nil, m.err
	}
	return &dsclient.DeleteRoleResponse{DeletedAt: time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC)}, nil
}

func (m *mockDynamicSecretsClient) GenerateCredential(_ context.Context, req *dsclient.GenerateCredentialRequest) (*dsclient.GenerateCredentialResponse, error) {
	m.generateReq = req
	if m.err != nil {
		return nil, m.err
	}
	return &dsclient.GenerateCredentialResponse{
		Credential: &dsclient.Credential{
			CredentialID: "cred_1",
			Data:         map[string]string{"username": "u1", "password": "SUPER-SECRET"},
			LeaseID:      "lease_1",
			ExpiresAt:    time.Date(2026, 3, 3, 0, 0, 0, 0, time.UTC),
		},
		Lease:   &dsclient.Lease{LeaseID: "lease_1", TTL: time.Hour, Renewable: true},
		LeaseID: "lease_1",
	}, nil
}

func (m *mockDynamicSecretsClient) RenewLease(_ context.Context, req *dsclient.RenewLeaseRequest) (*dsclient.RenewLeaseResponse, error) {
	m.renewReq = req
	if m.err != nil {
		return nil, m.err
	}
	return &dsclient.RenewLeaseResponse{
		Lease:     &dsclient.Lease{LeaseID: req.LeaseID, TTL: 2 * time.Hour, RenewCount: 1, Renewable: true},
		RenewedAt: time.Date(2026, 4, 4, 0, 0, 0, 0, time.UTC),
	}, nil
}

func (m *mockDynamicSecretsClient) RevokeLease(_ context.Context, req *dsclient.RevokeLeaseRequest) (*dsclient.RevokeLeaseResponse, error) {
	m.revokeReq = req
	if m.err != nil {
		return nil, m.err
	}
	return &dsclient.RevokeLeaseResponse{RevokedAt: time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)}, nil
}

func (m *mockDynamicSecretsClient) ListLeases(_ context.Context, req *dsclient.ListLeasesRequest) (*dsclient.ListLeasesResponse, error) {
	m.leasesReq = req
	if m.err != nil {
		return nil, m.err
	}
	return &dsclient.ListLeasesResponse{
		Leases: []*dsclient.Lease{{LeaseID: "lease_1", SecretPath: "database/readonly", TTL: time.Hour, Renewable: true}},
		Count:  1,
	}, nil
}

// =============================================================================
// Helpers
// =============================================================================

// withMockDynamicSecrets installs a dynamicSecretsClientFactory returning mc.
func withMockDynamicSecrets(t *testing.T, mc *mockDynamicSecretsClient) {
	t.Helper()
	origFactory := dynamicSecretsClientFactory
	origTimeout := globalTimeout
	origOutput := dynSecretsOutputFmt

	dynamicSecretsClientFactory = func() (quicbaas.DynamicSecretsClient, error) { return mc, nil }
	if globalTimeout == 0 {
		globalTimeout = 5 * time.Second
	}
	dynSecretsOutputFmt = "table"

	t.Cleanup(func() {
		dynamicSecretsClientFactory = origFactory
		globalTimeout = origTimeout
		dynSecretsOutputFmt = origOutput
		resetDynamicSecretsFlags()
	})
}

// withFailingDynamicSecretsFactory installs a factory that always fails.
func withFailingDynamicSecretsFactory(t *testing.T, err error) {
	t.Helper()
	orig := dynamicSecretsClientFactory
	dynamicSecretsClientFactory = func() (quicbaas.DynamicSecretsClient, error) { return nil, err }
	if globalTimeout == 0 {
		globalTimeout = 5 * time.Second
	}
	t.Cleanup(func() {
		dynamicSecretsClientFactory = orig
		resetDynamicSecretsFlags()
	})
}

// resetDynamicSecretsFlags clears every package-level dynamic-secrets flag.
func resetDynamicSecretsFlags() {
	dynSecretsGeneratorType = ""
	dynSecretsConfig = nil
	dynSecretsDefaultTTL = 0
	dynSecretsMaxTTL = 0
	dynSecretsPrefix = ""
	dynSecretsTTL = 0
	dynSecretsIncrement = 0
	dynSecretsRevealSecret = false
}

// =============================================================================
// Command construction
// =============================================================================

func TestNewDynamicSecretsCommand_VerbTree(t *testing.T) {
	cmd := newDynamicSecretsCommand()
	require.Equal(t, "dynamic-secrets", cmd.Use)
	assert.Contains(t, cmd.Aliases, "dynsec")
	require.NotNil(t, cmd.PersistentFlags().Lookup("output"))

	groups := make(map[string]*cobra.Command)
	for _, c := range cmd.Commands() {
		groups[c.Name()] = c
	}
	require.Contains(t, groups, "role")
	require.Contains(t, groups, "generate")
	require.Contains(t, groups, "lease")
	assert.Len(t, cmd.Commands(), 3)

	roleVerbs := map[string]bool{}
	for _, c := range groups["role"].Commands() {
		roleVerbs[c.Name()] = true
	}
	for _, verb := range []string{"create", "get", "list", "delete"} {
		assert.True(t, roleVerbs[verb], "role is missing %q", verb)
	}

	leaseVerbs := map[string]bool{}
	for _, c := range groups["lease"].Commands() {
		leaseVerbs[c.Name()] = true
	}
	for _, verb := range []string{"renew", "revoke", "list"} {
		assert.True(t, leaseVerbs[verb], "lease is missing %q", verb)
	}
}

// =============================================================================
// Argument / flag validation
// =============================================================================

func TestDynamicSecretsArg(t *testing.T) {
	value, err := dynamicSecretsArg("role get", "name", []string{"  readonly-db "})
	require.NoError(t, err)
	assert.Equal(t, "readonly-db", value)
}

func TestDynamicSecretsArg_Invalid(t *testing.T) {
	for name, args := range map[string][]string{
		"no args":   nil,
		"blank arg": {"   "},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := dynamicSecretsArg("role get", "name", args)
			require.Error(t, err)
			var typed *DynamicSecretsArgRequiredError
			require.True(t, stderrors.As(err, &typed))
		})
	}
}

func TestParseDynamicSecretsConfig(t *testing.T) {
	config, err := parseDynamicSecretsConfig([]string{"host=db.internal", "role=readonly", "empty="})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"host": "db.internal", "role": "readonly", "empty": ""}, config)

	config, err = parseDynamicSecretsConfig(nil)
	require.NoError(t, err)
	assert.Nil(t, config)
}

func TestParseDynamicSecretsConfig_Invalid(t *testing.T) {
	for _, bad := range []string{"hostdb.internal", "=value", ""} {
		t.Run(bad, func(t *testing.T) {
			_, err := parseDynamicSecretsConfig([]string{bad})
			require.Error(t, err)
			var typed *DynamicSecretsConfigFormatError
			require.True(t, stderrors.As(err, &typed))
		})
	}
}

func TestRunDynamicSecretsRoleCreate_FlagValidation(t *testing.T) {
	tests := []struct {
		name       string
		generator  string
		defaultTTL time.Duration
		maxTTL     time.Duration
		wantFlag   string
	}{
		{name: "missing generator", defaultTTL: time.Hour, maxTTL: time.Hour, wantFlag: "generator-type"},
		{name: "missing default ttl", generator: "postgres", maxTTL: time.Hour, wantFlag: "default-ttl"},
		{name: "missing max ttl", generator: "postgres", defaultTTL: time.Hour, wantFlag: "max-ttl"},
		{name: "default exceeds max", generator: "postgres", defaultTTL: 2 * time.Hour, maxTTL: time.Hour, wantFlag: "default-ttl"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withMockDynamicSecrets(t, &mockDynamicSecretsClient{})
			dynSecretsGeneratorType = tt.generator
			dynSecretsDefaultTTL = tt.defaultTTL
			dynSecretsMaxTTL = tt.maxTTL

			err := runDynamicSecretsRoleCreate(captureCmd(&bytes.Buffer{}), []string{"readonly-db"})
			require.Error(t, err)
			var typed *DynamicSecretsFlagInvalidError
			require.True(t, stderrors.As(err, &typed))
			assert.Equal(t, tt.wantFlag, typed.Flag)
		})
	}
}

func TestRunDynamicSecretsRoleCreate_RejectsBadConfig(t *testing.T) {
	withMockDynamicSecrets(t, &mockDynamicSecretsClient{})
	dynSecretsGeneratorType = "postgres"
	dynSecretsDefaultTTL = time.Hour
	dynSecretsMaxTTL = 24 * time.Hour
	dynSecretsConfig = []string{"not-a-pair"}

	err := runDynamicSecretsRoleCreate(captureCmd(&bytes.Buffer{}), []string{"readonly-db"})
	require.Error(t, err)
	var typed *DynamicSecretsConfigFormatError
	require.True(t, stderrors.As(err, &typed))
}

func TestRunDynamicSecretsGenerate_RejectsNegativeTTL(t *testing.T) {
	withMockDynamicSecrets(t, &mockDynamicSecretsClient{})
	dynSecretsTTL = -time.Second

	err := runDynamicSecretsGenerate(captureCmd(&bytes.Buffer{}), []string{"readonly-db"})
	require.Error(t, err)
	var typed *DynamicSecretsFlagInvalidError
	require.True(t, stderrors.As(err, &typed))
	assert.Equal(t, "ttl", typed.Flag)
}

func TestRunDynamicSecretsLeaseRenew_RejectsNegativeIncrement(t *testing.T) {
	withMockDynamicSecrets(t, &mockDynamicSecretsClient{})
	dynSecretsIncrement = -time.Minute

	err := runDynamicSecretsLeaseRenew(captureCmd(&bytes.Buffer{}), []string{"lease_1"})
	require.Error(t, err)
	var typed *DynamicSecretsFlagInvalidError
	require.True(t, stderrors.As(err, &typed))
	assert.Equal(t, "increment", typed.Flag)
}

// =============================================================================
// Success paths
// =============================================================================

func TestRunDynamicSecretsRoleCreate_Success(t *testing.T) {
	mc := &mockDynamicSecretsClient{}
	withMockDynamicSecrets(t, mc)
	dynSecretsGeneratorType = "postgres"
	dynSecretsDefaultTTL = time.Hour
	dynSecretsMaxTTL = 24 * time.Hour
	dynSecretsConfig = []string{"host=db.internal", "role=readonly"}

	var buf bytes.Buffer
	require.NoError(t, runDynamicSecretsRoleCreate(captureCmd(&buf), []string{"readonly-db"}))

	require.NotNil(t, mc.createReq)
	assert.Equal(t, "readonly-db", mc.createReq.Name)
	assert.Equal(t, "postgres", mc.createReq.GeneratorType)
	assert.Equal(t, time.Hour, mc.createReq.DefaultTTL)
	assert.Equal(t, 24*time.Hour, mc.createReq.MaxTTL)
	assert.Equal(t, "db.internal", mc.createReq.Config["host"])
	assert.Contains(t, buf.String(), "readonly-db")
	assert.Contains(t, buf.String(), "Config[host]:")
}

func TestRunDynamicSecretsRoleGetListDelete(t *testing.T) {
	mc := &mockDynamicSecretsClient{}
	withMockDynamicSecrets(t, mc)

	var buf bytes.Buffer
	require.NoError(t, runDynamicSecretsRoleGet(captureCmd(&buf), []string{"readonly-db"}))
	require.NotNil(t, mc.getReq)
	assert.Equal(t, "readonly-db", mc.getReq.Name)
	assert.Contains(t, buf.String(), "postgres")

	buf.Reset()
	dynSecretsPrefix = "readonly-"
	require.NoError(t, runDynamicSecretsRoleList(captureCmd(&buf), nil))
	require.NotNil(t, mc.listReq)
	assert.Equal(t, "readonly-", mc.listReq.Prefix)
	assert.Contains(t, buf.String(), "1 role(s)")

	buf.Reset()
	require.NoError(t, runDynamicSecretsRoleDelete(captureCmd(&buf), []string{"readonly-db"}))
	require.NotNil(t, mc.deleteReq)
	assert.Contains(t, buf.String(), "role readonly-db deleted")
}

func TestRunDynamicSecretsGenerate_RedactsByDefault(t *testing.T) {
	mc := &mockDynamicSecretsClient{}
	withMockDynamicSecrets(t, mc)
	dynSecretsTTL = 30 * time.Minute

	var buf bytes.Buffer
	require.NoError(t, runDynamicSecretsGenerate(captureCmd(&buf), []string{"readonly-db"}))

	require.NotNil(t, mc.generateReq)
	assert.Equal(t, "readonly-db", mc.generateReq.RoleName)
	assert.Equal(t, 30*time.Minute, mc.generateReq.TTL)
	assert.NotContains(t, buf.String(), "SUPER-SECRET",
		"table output must redact credential material unless --reveal is set")
	assert.Contains(t, buf.String(), "redacted")
	assert.Contains(t, buf.String(), "lease_1")
}

func TestRunDynamicSecretsGenerate_RevealPrintsSecret(t *testing.T) {
	withMockDynamicSecrets(t, &mockDynamicSecretsClient{})
	dynSecretsRevealSecret = true

	var buf bytes.Buffer
	require.NoError(t, runDynamicSecretsGenerate(captureCmd(&buf), []string{"readonly-db"}))
	assert.Contains(t, buf.String(), "SUPER-SECRET")
}

func TestRunDynamicSecretsLeaseVerbs(t *testing.T) {
	mc := &mockDynamicSecretsClient{}
	withMockDynamicSecrets(t, mc)

	var buf bytes.Buffer
	dynSecretsIncrement = time.Hour
	require.NoError(t, runDynamicSecretsLeaseRenew(captureCmd(&buf), []string{"lease_1"}))
	require.NotNil(t, mc.renewReq)
	assert.Equal(t, time.Hour, mc.renewReq.Increment)
	assert.Contains(t, buf.String(), "Renew Count:")

	buf.Reset()
	require.NoError(t, runDynamicSecretsLeaseRevoke(captureCmd(&buf), []string{"lease_1"}))
	require.NotNil(t, mc.revokeReq)
	assert.Contains(t, buf.String(), "lease lease_1 revoked")

	buf.Reset()
	dynSecretsPrefix = "database/"
	require.NoError(t, runDynamicSecretsLeaseList(captureCmd(&buf), nil))
	require.NotNil(t, mc.leasesReq)
	assert.Equal(t, "database/", mc.leasesReq.Prefix)
	assert.Contains(t, buf.String(), "1 lease(s)")
}

// =============================================================================
// Error propagation
// =============================================================================

func TestRunDynamicSecrets_FactoryErrorPropagates(t *testing.T) {
	sentinel := stderrors.New("no transport")
	withFailingDynamicSecretsFactory(t, sentinel)

	require.ErrorIs(t, runDynamicSecretsRoleGet(captureCmd(&bytes.Buffer{}), []string{"r"}), sentinel)
	require.ErrorIs(t, runDynamicSecretsRoleList(captureCmd(&bytes.Buffer{}), nil), sentinel)
	require.ErrorIs(t, runDynamicSecretsRoleDelete(captureCmd(&bytes.Buffer{}), []string{"r"}), sentinel)
	require.ErrorIs(t, runDynamicSecretsGenerate(captureCmd(&bytes.Buffer{}), []string{"r"}), sentinel)
	require.ErrorIs(t, runDynamicSecretsLeaseRenew(captureCmd(&bytes.Buffer{}), []string{"l"}), sentinel)
	require.ErrorIs(t, runDynamicSecretsLeaseRevoke(captureCmd(&bytes.Buffer{}), []string{"l"}), sentinel)
	require.ErrorIs(t, runDynamicSecretsLeaseList(captureCmd(&bytes.Buffer{}), nil), sentinel)
}

func TestRunDynamicSecrets_ServerErrorPropagates(t *testing.T) {
	sentinel := stderrors.New("dynsec boom")
	withMockDynamicSecrets(t, &mockDynamicSecretsClient{err: sentinel})

	require.ErrorIs(t, runDynamicSecretsRoleGet(captureCmd(&bytes.Buffer{}), []string{"r"}), sentinel)
	require.ErrorIs(t, runDynamicSecretsGenerate(captureCmd(&bytes.Buffer{}), []string{"r"}), sentinel)
	require.ErrorIs(t, runDynamicSecretsLeaseList(captureCmd(&bytes.Buffer{}), nil), sentinel)
}

// =============================================================================
// Formatting
// =============================================================================

func TestFormatDynamicSecretsRole_Formats(t *testing.T) {
	role := &dsclient.Role{Name: "readonly-db", GeneratorType: "postgres", DefaultTTL: time.Hour, MaxTTL: 24 * time.Hour}
	for _, format := range []string{"table", "json", "yaml", ""} {
		t.Run("format="+format, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, formatDynamicSecretsRole(role, format, &buf))
			assert.Contains(t, buf.String(), "readonly-db")
		})
	}
}

func TestFormatDynamicSecretsRole_NilAndBadFormat(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, formatDynamicSecretsRole(nil, "table", &buf))
	assert.Contains(t, buf.String(), "(no role)")

	buf.Reset()
	require.Error(t, formatDynamicSecretsRole(&dsclient.Role{}, "toml", &buf))
}

func TestFormatDynamicSecretsLists_Empty(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, formatDynamicSecretsRoleList(&dsclient.ListRolesResponse{}, "table", &buf))
	assert.Contains(t, buf.String(), "No roles found")

	buf.Reset()
	require.NoError(t, formatDynamicSecretsLeaseList(nil, "table", &buf))
	assert.Contains(t, buf.String(), "No leases found")
}

func TestFormatDynamicSecretsCredential_NilAndBadFormat(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, formatDynamicSecretsCredential(nil, false, "table", &buf))
	assert.Contains(t, buf.String(), "(no credential)")

	buf.Reset()
	require.Error(t, formatDynamicSecretsCredential(&dsclient.GenerateCredentialResponse{}, false, "toml", &buf))
}

func TestFormatDynamicSecretsCredential_JSONCarriesFullPayload(t *testing.T) {
	resp := &dsclient.GenerateCredentialResponse{
		Credential: &dsclient.Credential{CredentialID: "cred_1", Data: map[string]string{"password": "SUPER-SECRET"}},
		LeaseID:    "lease_1",
	}
	var buf bytes.Buffer
	require.NoError(t, formatDynamicSecretsCredential(resp, false, "json", &buf))
	assert.Contains(t, buf.String(), "SUPER-SECRET",
		"json output exists to be piped into a consumer and must not redact")
}

func TestFormatDynamicSecretsLease_NilAndBadFormat(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, formatDynamicSecretsLease(nil, "table", &buf))
	assert.Contains(t, buf.String(), "(no lease)")

	buf.Reset()
	require.Error(t, formatDynamicSecretsLease(&dsclient.Lease{}, "toml", &buf))
}
