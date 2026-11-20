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

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
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

// TestSecretsService_RepeatedRotationsStayBounded runs many scheduled
// rotations of one secret: each adds a version, and the rotation prunes the
// secret to its policy's retained versions plus the current one (ten when
// the policy names none), so the versions stay bounded however long the
// policy runs.
func TestSecretsService_RepeatedRotationsStayBounded(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		retain int
		want   int
	}{
		{name: "policy retention", retain: 2, want: 3},
		{name: "default retention", retain: 0, want: defaultRotationRetainVersions + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc, mgr := newRotationTestService(t)
			acme := tenantctx.WithTenant(context.Background(), "acme")
			start := time.Now().UTC().Truncate(time.Second)
			storeWithPolicy(t, svc, acme, "db/rotating", &SecretRotationPolicy{
				Enabled: true, Interval: time.Hour, NextRotation: start.Add(-time.Minute), RetainVersions: tc.retain,
			})
			const path = "tenant/acme/db/rotating"
			sysCtx := secretsRotationWriteContext(auth.NewSystemContext(context.Background()), path)

			valueBefore, _, err := mgr.Get(context.Background(), path)
			require.NoError(t, err)
			const rotations = 25
			for i := 0; i < rotations; i++ {
				data, meta, err := mgr.Get(context.Background(), path)
				require.NoError(t, err)
				require.NoError(t, svc.rotateDueSecret(sysCtx, path, data, meta, start.Add(time.Duration(i)*time.Hour)), "rotation %d", i+1)
			}

			versions, err := mgr.ListVersions(context.Background(), path)
			require.NoError(t, err)
			var live []int
			for _, v := range versions {
				if !v.Deleted {
					live = append(live, v.Version)
				}
			}
			require.Len(t, live, tc.want, "the rotated secret keeps a bounded number of versions")
			newest := live[len(live)-1]
			assert.Greater(t, newest, rotations, "every rotation wrote a version")
			for i, v := range live {
				assert.Equal(t, newest-len(live)+1+i, v, "the newest versions are the ones kept")
			}
			value, meta, err := mgr.Get(context.Background(), path)
			require.NoError(t, err)
			assert.Equal(t, valueBefore, value, "the current version keeps the value")
			assert.Equal(t, "scheduled", meta["rotation_reason"])
		})
	}
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

// TestSecretsService_RotateDueSecretsSkipsDisabled: a disabled secret whose
// policy is due is not rotated; GetSecret refuses it, and the scan reads
// beneath that check.
func TestSecretsService_RotateDueSecretsSkipsDisabled(t *testing.T) {
	t.Parallel()
	svc, mgr := newRotationTestService(t)
	sys := tenantctx.WithSystem(context.Background())
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	require.NoError(t, mgr.Store(sys, "sys/off", map[string][]byte{"data": []byte("x")},
		map[string]string{"status": string(SecretStatusDisabled), "rotation_enabled": "true",
			"rotation_interval": "1h", "rotation_next": past}))

	result, err := svc.RotateDueSecrets(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, result.Due)
	assert.Equal(t, 0, result.Rotated)
	versions, err := mgr.ListVersions(sys, "sys/off")
	require.NoError(t, err)
	assert.Len(t, versions, 1, "a disabled secret gets no new version")
}

// TestSecretsService_RotateDueSecretsCountsUnreadable: a listed secret the
// scan cannot read is counted, while one deleted since the list is not.
func TestSecretsService_RotateDueSecretsCountsUnreadable(t *testing.T) {
	t.Parallel()
	_, mgr := newRotationTestService(t)
	sys := tenantctx.WithSystem(context.Background())
	for _, p := range []string{"sys/broken", "sys/gone"} {
		require.NoError(t, mgr.Store(sys, p, map[string][]byte{"data": []byte("x")}, nil))
	}
	svc := newSecretsService(getFailingSecretsManager{SecretsManager: mgr, fail: map[string]error{
		"sys/broken": errors.New("storage unavailable"),
		"sys/gone":   secrets.ErrSecretNotFound,
	}}, newMockLogger())

	result, err := svc.RotateDueSecrets(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, result.Unreadable)
	assert.Equal(t, 0, result.Due)
}

// getFailingSecretsManager fails Get for the paths it names.
type getFailingSecretsManager struct {
	interfaces.SecretsManager
	fail map[string]error
}

func (m getFailingSecretsManager) Get(ctx context.Context, path string) (map[string][]byte, map[string]string, error) {
	if err, ok := m.fail[path]; ok {
		return nil, nil, err
	}
	return m.SecretsManager.Get(ctx, path)
}

// interleavingSecretsManager runs before just ahead of each conditional
// write: a tenant's write landing between the scan's read and its write.
type interleavingSecretsManager struct {
	*secrets.Manager
	before func()
}

func (m *interleavingSecretsManager) StoreVersionIfCurrent(ctx context.Context, path string, expected secrets.ReadVersion, data map[string][]byte, metadata map[string]string) (int, error) {
	m.before()
	return m.Manager.StoreVersionIfCurrent(ctx, path, expected, data, metadata)
}

// TestSecretsService_RotationLosesToAnInterleavedWrite: a scheduled rotation
// supersedes only the version it read. An update, a policy disable or a
// delete that lands between the read and the write wins; the rotation
// stores nothing and counts a failure.
func TestSecretsService_RotationLosesToAnInterleavedWrite(t *testing.T) {
	t.Parallel()
	const path = "tenant/acme/db/due"
	sys := tenantctx.WithSystem(context.Background())

	cases := []struct {
		name  string
		write func(t *testing.T, svc *secretsService, mgr *secrets.Manager)
		check func(t *testing.T, svc *secretsService, mgr *secrets.Manager)
	}{
		{
			name: "update",
			write: func(t *testing.T, _ *secretsService, mgr *secrets.Manager) {
				require.NoError(t, mgr.Store(sys, path, map[string][]byte{"data": []byte("updated")}, map[string]string{}))
			},
			check: func(t *testing.T, _ *secretsService, mgr *secrets.Manager) {
				data, meta, err := mgr.Get(sys, path)
				require.NoError(t, err)
				assert.Equal(t, []byte("updated"), data["data"], "the update is not overwritten")
				assert.Empty(t, meta["rotation_reason"])
			},
		},
		{
			name: "disable",
			write: func(t *testing.T, svc *secretsService, _ *secrets.Manager) {
				_, err := svc.SetRotationPolicy(tenantctx.WithTenant(context.Background(), "acme"),
					&SetRotationPolicyRequest{SecretID: "db/due", Policy: &SecretRotationPolicy{Enabled: false, Interval: time.Hour}})
				require.NoError(t, err)
			},
			check: func(t *testing.T, svc *secretsService, _ *secrets.Manager) {
				got, err := svc.GetRotationPolicy(tenantctx.WithTenant(context.Background(), "acme"), &GetRotationPolicyRequest{SecretID: "db/due"})
				require.NoError(t, err)
				assert.False(t, got.Policy.Enabled, "the disable is not overwritten")
			},
		},
		{
			name: "delete",
			write: func(t *testing.T, _ *secretsService, mgr *secrets.Manager) {
				require.NoError(t, mgr.Delete(sys, path))
			},
			check: func(t *testing.T, _ *secretsService, mgr *secrets.Manager) {
				_, _, err := mgr.Get(sys, path)
				assert.Error(t, err, "the deleted secret is not revived")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mgr, err := secrets.NewManager(&secrets.ManagerOptions{Backend: secrets.NewMemoryBackend()})
			require.NoError(t, err)
			wrapped := &interleavingSecretsManager{Manager: mgr}
			svc := newSecretsService(wrapped, newMockLogger())
			storeWithPolicy(t, svc, tenantctx.WithTenant(context.Background(), "acme"), "db/due",
				&SecretRotationPolicy{Enabled: true, Interval: time.Hour, NextRotation: time.Now().Add(-time.Hour).UTC()})
			wrapped.before = func() { tc.write(t, svc, mgr) }

			result, err := svc.RotateDueSecrets(context.Background())
			require.NoError(t, err)
			assert.Equal(t, 1, result.Due)
			assert.Equal(t, 0, result.Rotated)
			assert.Equal(t, 1, result.Failed)
			tc.check(t, svc, mgr)
		})
	}
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
		{"never scheduled", time.Time{}, now.Add(time.Hour)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := nextSecretRotation(tc.previous, time.Hour, now)
			assert.Equal(t, tc.want, got)
			assert.True(t, got.After(now))
		})
	}
}

// TestSecretsService_RotationPolicyWithoutDeadline: the MCP tool declares no
// next_rotation, so an enabled policy arrives with a zero deadline. The
// service schedules the first one interval out, and a secret already stored
// with an enabled policy and no deadline is due on the next scan.
func TestSecretsService_RotationPolicyWithoutDeadline(t *testing.T) {
	t.Parallel()
	svc, mgr := newRotationTestService(t)
	acme := tenantctx.WithTenant(context.Background(), "acme")
	before := time.Now().UTC().Truncate(time.Second)

	storeWithPolicy(t, svc, acme, "db/mcp", &SecretRotationPolicy{Enabled: true, Interval: time.Hour})
	got, err := svc.GetRotationPolicy(acme, &GetRotationPolicyRequest{SecretID: "db/mcp"})
	require.NoError(t, err)
	next := got.Policy.NextRotation.UTC()
	assert.False(t, next.Before(before.Add(time.Hour)), "the first deadline is one interval out, got %s", next)
	assert.False(t, next.After(time.Now().UTC().Add(time.Hour)), "the first deadline is one interval out, got %s", next)

	// A secret stored with an enabled policy and no deadline at all.
	sys := tenantctx.WithSystem(context.Background())
	require.NoError(t, mgr.Store(sys, "tenant/acme/db/legacy",
		map[string][]byte{"data": []byte("x")},
		map[string]string{"rotation_enabled": "true", "rotation_interval": "1h", "rotation_next": time.Time{}.Format(time.RFC3339)}))

	result, err := svc.RotateDueSecrets(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, result.Due, "only the secret without a deadline is due")
	assert.Equal(t, 1, result.Rotated)

	_, meta, err := mgr.Get(sys, "tenant/acme/db/legacy")
	require.NoError(t, err)
	scheduled, err := time.Parse(time.RFC3339, meta["rotation_next"])
	require.NoError(t, err)
	assert.True(t, scheduled.After(time.Now().UTC()), "the rotation schedules the next deadline, got %s", scheduled)
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

// A policy read back carries its stored retention, so a read-then-save keeps
// it and the rotations that follow prune to it.
func TestSecretsService_RotationPolicyRoundTripKeepsRetention(t *testing.T) {
	t.Parallel()
	svc, mgr := newRotationTestService(t)
	acme := tenantctx.WithTenant(context.Background(), "acme")
	start := time.Now().UTC().Truncate(time.Second)
	storeWithPolicy(t, svc, acme, "db/kept", &SecretRotationPolicy{
		Enabled: true, Interval: time.Hour, NextRotation: start.Add(time.Hour), RetainVersions: 2,
	})

	got, err := svc.GetRotationPolicy(acme, &GetRotationPolicyRequest{SecretID: "db/kept"})
	require.NoError(t, err)
	assert.Equal(t, 2, got.Policy.RetainVersions, "the read returns the stored retention")

	// Read, change the interval and deadline, save the policy back.
	updated := *got.Policy
	updated.Interval = 2 * time.Hour
	updated.NextRotation = start.Add(-time.Minute)
	_, err = svc.SetRotationPolicy(acme, &SetRotationPolicyRequest{SecretID: "db/kept", Policy: &updated})
	require.NoError(t, err)

	got, err = svc.GetRotationPolicy(acme, &GetRotationPolicyRequest{SecretID: "db/kept"})
	require.NoError(t, err)
	assert.Equal(t, 2, got.Policy.RetainVersions, "a read-then-save keeps the retention")
	assert.Equal(t, 2*time.Hour, got.Policy.Interval)

	const path = "tenant/acme/db/kept"
	sysCtx := secretsRotationWriteContext(auth.NewSystemContext(context.Background()), path)
	for i := 0; i < 5; i++ {
		data, meta, err := mgr.Get(context.Background(), path)
		require.NoError(t, err)
		require.NoError(t, svc.rotateDueSecret(sysCtx, path, data, meta, start.Add(time.Duration(i)*2*time.Hour)), "rotation %d", i+1)
	}
	versions, err := mgr.ListVersions(context.Background(), path)
	require.NoError(t, err)
	var live []int
	for _, v := range versions {
		if !v.Deleted {
			live = append(live, v.Version)
		}
	}
	assert.Len(t, live, 3, "rotations keep the retained versions plus the current one")
}
