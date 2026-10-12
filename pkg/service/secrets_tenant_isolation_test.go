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

package service

import (
	"errors"
	"testing"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// The tests in this file prove tenant isolation of the secrets vault the way
// a caller experiences it: two tenants address the SAME path, and the only
// thing telling them apart is the tenant bound to the request context (the
// JWT tenant_id claim, at the transport). Writing two different paths would
// pass with every bit of tenant scoping deleted (go-quicbaas#281).

const (
	isolationTenantA = "tenant-alpha"
	isolationTenantB = "tenant-beta"
	isolationPath    = "app/api-key"
)

// requireSecretNotFound asserts err is the not-found refusal a tenant gets
// for a path that exists only in another tenant's namespace.
func requireSecretNotFound(t *testing.T, err error, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: want not-found, got nil error", what)
	}
	var qb *qberrors.QBError
	if !errors.As(err, &qb) {
		t.Fatalf("%s: want *errors.QBError, got %T: %v", what, err, err)
	}
	if qb.Code != qberrors.ErrNotFound {
		t.Fatalf("%s: want code %v, got %v (%v)", what, qberrors.ErrNotFound, qb.Code, err)
	}
}

// storeAs stores value at path as tenantID and returns the version written.
func storeAs(t *testing.T, svc *secretsService, tenantID, path, value string) uint64 {
	t.Helper()
	resp, err := svc.StoreSecret(secretsScopeCtx(tenantID), &StoreSecretRequest{
		SecretID: path,
		Data:     []byte(value),
	})
	if err != nil {
		t.Fatalf("StoreSecret as %q: %v", tenantID, err)
	}
	return resp.Version
}

// readAs returns the latest value at path as seen by tenantID.
func readAs(t *testing.T, svc *secretsService, tenantID, path string) string {
	t.Helper()
	resp, err := svc.GetSecret(secretsScopeCtx(tenantID), &GetSecretRequest{SecretID: path})
	if err != nil {
		t.Fatalf("GetSecret as %q: %v", tenantID, err)
	}
	return string(resp.Data)
}

// listAs returns the secret IDs tenantID enumerates.
func listAs(t *testing.T, svc *secretsService, tenantID string) []string {
	t.Helper()
	resp, err := svc.ListSecrets(secretsScopeCtx(tenantID), &ListSecretsRequest{})
	if err != nil {
		t.Fatalf("ListSecrets as %q: %v", tenantID, err)
	}
	ids := make([]string, 0, len(resp.Secrets))
	for _, s := range resp.Secrets {
		ids = append(ids, s.SecretID)
	}
	return ids
}

func TestSecretsSamePathIsolation_SamePathHoldsOneValuePerTenant(t *testing.T) {
	svc := newServiceForScopeTest(t)

	if v := storeAs(t, svc, isolationTenantA, isolationPath, "alpha-value"); v != 1 {
		t.Fatalf("tenant A first write: want version 1, got %d", v)
	}
	// B's first write to the same path must start B's own history, not
	// become version 2 of A's secret.
	if v := storeAs(t, svc, isolationTenantB, isolationPath, "beta-value"); v != 1 {
		t.Fatalf("tenant B first write to A's path: want version 1, got %d", v)
	}

	if got := readAs(t, svc, isolationTenantA, isolationPath); got != "alpha-value" {
		t.Fatalf("tenant A read %q, want its own value", got)
	}
	if got := readAs(t, svc, isolationTenantB, isolationPath); got != "beta-value" {
		t.Fatalf("tenant B read %q, want its own value", got)
	}

	for _, tenantID := range []string{isolationTenantA, isolationTenantB} {
		ids := listAs(t, svc, tenantID)
		if len(ids) != 1 || ids[0] != isolationPath {
			t.Fatalf("tenant %q lists %v, want exactly [%s]", tenantID, ids, isolationPath)
		}
	}
}

func TestSecretsSamePathIsolation_OtherTenantCannotReachSecret(t *testing.T) {
	svc := newServiceForScopeTest(t)
	storeAs(t, svc, isolationTenantA, isolationPath, "alpha-value")
	ctxB := secretsScopeCtx(isolationTenantB)

	_, err := svc.GetSecret(ctxB, &GetSecretRequest{SecretID: isolationPath})
	requireSecretNotFound(t, err, "tenant B GetSecret")

	_, err = svc.GetSecretVersion(ctxB, &GetSecretVersionRequest{SecretID: isolationPath, Version: 1})
	requireSecretNotFound(t, err, "tenant B GetSecretVersion")

	if ids := listAs(t, svc, isolationTenantB); len(ids) != 0 {
		t.Fatalf("tenant B lists %v, want nothing of tenant A's", ids)
	}

	_, err = svc.RotateSecret(ctxB, &RotateSecretRequest{SecretID: isolationPath, NewData: []byte("hijacked")})
	requireSecretNotFound(t, err, "tenant B RotateSecret")

	_, err = svc.UpdateSecretStatus(ctxB, &UpdateSecretStatusRequest{
		SecretID: isolationPath,
		Status:   SecretStatusDisabled,
	})
	requireSecretNotFound(t, err, "tenant B UpdateSecretStatus")

	_, err = svc.DeleteSecret(ctxB, &DeleteSecretRequest{SecretID: isolationPath, PermanentlyDelete: true, AllVersions: true})
	requireSecretNotFound(t, err, "tenant B DeleteSecret (permanent)")

	_, err = svc.DeleteSecret(ctxB, &DeleteSecretRequest{SecretID: isolationPath})
	requireSecretNotFound(t, err, "tenant B DeleteSecret (soft)")

	resp, err := svc.GetSecret(secretsScopeCtx(isolationTenantA), &GetSecretRequest{SecretID: isolationPath})
	if err != nil {
		t.Fatalf("tenant A GetSecret after tenant B's attempts: %v", err)
	}
	if string(resp.Data) != "alpha-value" || resp.Version != 1 {
		t.Fatalf("tenant A's secret changed: data=%q version=%d", resp.Data, resp.Version)
	}
	if status := resp.Metadata["status"]; status == string(SecretStatusDisabled) {
		t.Fatalf("tenant B disabled tenant A's secret")
	}
}

func TestSecretsSamePathIsolation_OverwriteAndDeleteStayInOwnNamespace(t *testing.T) {
	svc := newServiceForScopeTest(t)
	storeAs(t, svc, isolationTenantA, isolationPath, "alpha-value")
	storeAs(t, svc, isolationTenantB, isolationPath, "beta-value")
	ctxB := secretsScopeCtx(isolationTenantB)

	// B's second write updates B's secret only.
	if v := storeAs(t, svc, isolationTenantB, isolationPath, "beta-value-2"); v != 2 {
		t.Fatalf("tenant B second write: want version 2, got %d", v)
	}
	if got := readAs(t, svc, isolationTenantA, isolationPath); got != "alpha-value" {
		t.Fatalf("tenant B's update reached tenant A: A reads %q", got)
	}

	if _, err := svc.DeleteSecret(ctxB, &DeleteSecretRequest{
		SecretID:          isolationPath,
		PermanentlyDelete: true,
		AllVersions:       true,
	}); err != nil {
		t.Fatalf("tenant B DeleteSecret of its own secret: %v", err)
	}
	_, err := svc.GetSecret(ctxB, &GetSecretRequest{SecretID: isolationPath})
	requireSecretNotFound(t, err, "tenant B GetSecret after its delete")

	if got := readAs(t, svc, isolationTenantA, isolationPath); got != "alpha-value" {
		t.Fatalf("tenant B's delete reached tenant A: A reads %q", got)
	}
}
