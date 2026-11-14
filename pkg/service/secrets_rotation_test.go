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

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

func newRotationTestService(t *testing.T) (*secretsService, interfaces.SecretsManager) {
	t.Helper()
	mgr, err := secrets.NewManager(&secrets.ManagerOptions{Backend: secrets.NewMemoryBackend()})
	require.NoError(t, err)
	return newSecretsService(mgr, newMockLogger()), mgr
}

func storeWithPolicy(t *testing.T, svc *secretsService, ctx context.Context, id string, policy *SecretRotationPolicy) {
	t.Helper()
	_, err := svc.StoreSecret(ctx, &StoreSecretRequest{SecretID: id, Name: id, Data: []byte("value-of-" + id)})
	require.NoError(t, err)
	if policy != nil {
		_, err = svc.SetRotationPolicy(ctx, &SetRotationPolicyRequest{SecretID: id, Policy: policy})
		require.NoError(t, err)
	}
}

// A rotation policy is performed, not only stored (#726): the scan writes a
// due secret's value as a new version, records the scheduled rotation and
// advances the next rotation; secrets not yet due, with rotation disabled,
// or with no policy are untouched.
func TestSecretsService_RotateDueSecretsPerformsDuePolicies(t *testing.T) {
	t.Parallel()
	svc, mgr := newRotationTestService(t)
	acme := tenantctx.WithTenant(context.Background(), "acme")
	now := time.Now().UTC().Truncate(time.Second)

	storeWithPolicy(t, svc, acme, "db/due", &SecretRotationPolicy{Enabled: true, Interval: time.Hour, NextRotation: now.Add(-90 * time.Minute)})
	storeWithPolicy(t, svc, acme, "db/later", &SecretRotationPolicy{Enabled: true, Interval: time.Hour, NextRotation: now.Add(time.Hour)})
	storeWithPolicy(t, svc, acme, "db/off", &SecretRotationPolicy{Enabled: false, Interval: time.Hour, NextRotation: now.Add(-time.Hour)})
	storeWithPolicy(t, svc, acme, "db/none", nil)

	versionsOf := func(id string) int {
		v, err := mgr.ListVersions(context.Background(), "tenant/acme/"+id)
		require.NoError(t, err)
		return len(v)
	}
	before := map[string]int{}
	for _, id := range []string{"db/due", "db/later", "db/off", "db/none"} {
		before[id] = versionsOf(id)
	}
	dataBefore, _, err := mgr.Get(context.Background(), "tenant/acme/db/due")
	require.NoError(t, err)

	result, err := svc.RotateDueSecrets(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, result.Due)
	assert.Equal(t, 1, result.Rotated)
	assert.Equal(t, 0, result.Failed)

	assert.Equal(t, before["db/due"]+1, versionsOf("db/due"), "the due secret gets a new version")
	for _, id := range []string{"db/later", "db/off", "db/none"} {
		assert.Equal(t, before[id], versionsOf(id), "%s is not rotated", id)
	}

	data, meta, err := mgr.Get(context.Background(), "tenant/acme/db/due")
	require.NoError(t, err)
	assert.Equal(t, dataBefore, data, "a rotation keeps the value")
	assert.Equal(t, "scheduled", meta["rotation_reason"])
	assert.NotEmpty(t, meta["rotation_last"])
	next, err := time.Parse(time.RFC3339, meta["rotation_next"])
	require.NoError(t, err)
	assert.Equal(t, now.Add(30*time.Minute), next, "the next rotation keeps the cadence past the missed period")

	// The tenant reads the advanced policy back.
	got, err := svc.GetRotationPolicy(acme, &GetRotationPolicyRequest{SecretID: "db/due"})
	require.NoError(t, err)
	assert.True(t, got.Policy.Enabled)
	assert.Equal(t, next, got.Policy.NextRotation.UTC())

	// A second scan finds nothing due.
	result, err = svc.RotateDueSecrets(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, result.Due)
}

// A due policy whose interval cannot schedule a next rotation fails and
// stays due; it does not stop the scan.
func TestSecretsService_RotateDueSecretsCountsFailures(t *testing.T) {
	t.Parallel()
	svc, mgr := newRotationTestService(t)
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	require.NoError(t, mgr.Store(tenantctx.WithSystem(context.Background()), "sys/bad",
		map[string][]byte{"data": []byte("x")},
		map[string]string{"rotation_enabled": "true", "rotation_interval": "bogus", "rotation_next": past}))

	result, err := svc.RotateDueSecrets(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, result.Due)
	assert.Equal(t, 0, result.Rotated)
	assert.Equal(t, 1, result.Failed)
}

type listFailingSecretsManager struct {
	interfaces.SecretsManager
	err error
}

func (m listFailingSecretsManager) List(context.Context, string) ([]string, error) {
	return nil, m.err
}

func TestSecretsService_RotateDueSecretsScanError(t *testing.T) {
	t.Parallel()
	cause := errors.New("storage unavailable")
	svc := newSecretsService(listFailingSecretsManager{err: cause}, newMockLogger())

	result, err := svc.RotateDueSecrets(context.Background())

	assert.Nil(t, result)
	var scanErr *SecretsRotationScanError
	require.ErrorAs(t, err, &scanErr)
	assert.ErrorIs(t, err, cause)
}

func TestNextSecretRotation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		previous time.Time
		want     time.Time
	}{
		{"one period", now.Add(-10 * time.Minute), now.Add(50 * time.Minute)},
		{"exactly due", now, now.Add(time.Hour)},
		{"missed periods", now.Add(-150 * time.Minute), now.Add(30 * time.Minute)},
		{"on a boundary", now.Add(-2 * time.Hour), now.Add(time.Hour)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := nextSecretRotation(tc.previous, time.Hour, now)
			assert.Equal(t, tc.want, got)
			assert.True(t, got.After(now))
		})
	}
}

func TestSecretsRotationWriteContext(t *testing.T) {
	t.Parallel()
	tenant, ok := tenantctx.Lookup(secretsRotationWriteContext(context.Background(), "tenant/acme/db/pass"))
	require.True(t, ok)
	assert.Equal(t, "acme", tenant)

	system, ok := tenantctx.Lookup(secretsRotationWriteContext(context.Background(), "db/pass"))
	require.True(t, ok)
	assert.True(t, tenantctx.IsSystem(system))
}
