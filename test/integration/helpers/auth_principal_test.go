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

//go:build integration

package helpers

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

const testBaseURL = "https://node:8443"

var (
	// errDeleteRefused is the failure fakeTenantDeletes returns when told to.
	errDeleteRefused = errors.New("delete refused")

	// errEnrollRefused is the failure fakeEnrollment returns when told to.
	errEnrollRefused = errors.New("enroll refused")
)

// fakeTenantDeletes stands in for the server's tenant delete and records
// each tenant it was asked to delete.
type fakeTenantDeletes struct {
	deleted []string
	fail    bool
}

func (f *fakeTenantDeletes) delete(_ context.Context, _, tenantID string) error {
	if f.fail {
		return errDeleteRefused
	}
	f.deleted = append(f.deleted, tenantID)
	return nil
}

// fakeEnrollment stands in for the server's tenant provisioning and PIV
// login, counting each call.
type fakeEnrollment struct {
	enrolls    int
	logins     int
	enrollFail bool
	loginFail  bool
	expiresIn  int
}

func (f *fakeEnrollment) enroll(_ context.Context, _ string) (*enrolledPrincipal, error) {
	f.enrolls++
	if f.enrollFail {
		return nil, errEnrollRefused
	}
	return &enrolledPrincipal{tenantID: "tenant-1"}, nil
}

func (f *fakeEnrollment) login(_ context.Context, _ string, _, _ []byte) (string, int, error) {
	f.logins++
	if f.loginFail {
		return "", 0, errLoginRefused
	}
	return "bearer-1", f.expiresIn, nil
}

func newFakeRegistry(fake *fakeEnrollment) *principalRegistry {
	return newPrincipalRegistry("test", fake.enroll, fake.login)
}

func TestPrincipalBearer_EnrollsOnceAndReusesBearer(t *testing.T) {
	fake := &fakeEnrollment{expiresIn: 3600}
	registry := newFakeRegistry(fake)

	first, err := registry.bearer(context.Background(), testBaseURL+"/")
	require.NoError(t, err)
	second, err := registry.bearer(context.Background(), testBaseURL)
	require.NoError(t, err)

	require.Equal(t, "bearer-1", first)
	require.Equal(t, first, second)
	require.Equal(t, 1, fake.enrolls, "the principal is provisioned once per cluster")
	require.Equal(t, 1, fake.logins, "an unexpired bearer is reused")
}

func TestPrincipalBearer_EnrollFailureIsNotRemembered(t *testing.T) {
	fake := &fakeEnrollment{enrollFail: true}
	registry := newFakeRegistry(fake)

	_, err := registry.bearer(context.Background(), testBaseURL)
	require.ErrorIs(t, err, errEnrollRefused)

	fake.enrollFail = false
	bearer, err := registry.bearer(context.Background(), testBaseURL)
	require.NoError(t, err)
	require.Equal(t, "bearer-1", bearer)
	require.Equal(t, 2, fake.enrolls, "a failed enrollment is retried")
}

func TestPrincipalGRPCCredentials_CarriesTheBearer(t *testing.T) {
	registry := newFakeRegistry(&fakeEnrollment{})

	creds, err := registry.newGRPCCredentials(context.Background())
	require.NoError(t, err)
	md, err := creds.GetRequestMetadata(context.Background())
	require.NoError(t, err)
	require.Equal(t, map[string]string{"authorization": "Bearer bearer-1"}, md)
}

// A session goroutine cannot call t.Fatalf (#1593), so the enrollment
// failure must come back as the returned error, with no credentials.
func TestPrincipalGRPCCredentials_EnrollFailureIsReturned(t *testing.T) {
	registry := newFakeRegistry(&fakeEnrollment{enrollFail: true})

	creds, err := registry.newGRPCCredentials(context.Background())
	require.ErrorIs(t, err, errEnrollRefused)
	require.Nil(t, creds)
}

func TestPrincipalBearer_LoginFailureIsTyped(t *testing.T) {
	fake := &fakeEnrollment{loginFail: true}
	registry := newFakeRegistry(fake)

	_, err := registry.bearer(context.Background(), testBaseURL)

	var authErr *PrincipalAuthError
	require.ErrorAs(t, err, &authErr)
	require.Equal(t, "PIV login", authErr.Step)
	require.Equal(t, "tenant-1", authErr.TenantID)
	require.ErrorIs(t, err, errLoginRefused)
}

func TestPrincipalTenantID_EmptyUntilEnrolled(t *testing.T) {
	registry := newFakeRegistry(&fakeEnrollment{expiresIn: 3600})
	require.Empty(t, registry.tenantID(testBaseURL))

	_, err := registry.bearer(context.Background(), testBaseURL)
	require.NoError(t, err)
	require.Equal(t, "tenant-1", registry.tenantID(testBaseURL+"/"))
}

func TestPrincipalRelease_DeletesProvisionedTenantOnce(t *testing.T) {
	registry := newFakeRegistry(&fakeEnrollment{})
	registry.principals[testBaseURL] = &enrolledPrincipal{tenantID: "dataplane-1"}
	fake := &fakeTenantDeletes{}

	require.NoError(t, registry.release(context.Background(), fake.delete))
	require.NoError(t, registry.release(context.Background(), fake.delete))

	require.Equal(t, []string{"dataplane-1"}, fake.deleted, "a released tenant is deleted exactly once")
	require.Empty(t, registry.principals)
}

func TestPrincipalRelease_NothingProvisionedIsNoOp(t *testing.T) {
	registry := newFakeRegistry(&fakeEnrollment{})
	fake := &fakeTenantDeletes{}

	require.NoError(t, registry.release(context.Background(), fake.delete))
	require.Empty(t, fake.deleted)
}

func TestPrincipalRelease_DeleteFailureIsTyped(t *testing.T) {
	registry := newFakeRegistry(&fakeEnrollment{})
	registry.principals[testBaseURL] = &enrolledPrincipal{tenantID: "dataplane-1"}
	fake := &fakeTenantDeletes{fail: true}

	err := registry.release(context.Background(), fake.delete)

	var authErr *PrincipalAuthError
	require.ErrorAs(t, err, &authErr)
	require.Equal(t, "dataplane-1", authErr.TenantID)
	require.ErrorIs(t, err, errDeleteRefused)
	require.Len(t, registry.principals, 1, "a tenant whose delete failed is kept")
}

// The tenant operator lives in the tenant principal's tenant: releasing it
// must forget its enrolment, so a later bearer enrols afresh, and must not
// delete the tenant, which the tenant principal's release deletes.
func TestPrincipalRelease_ForgetTenantForgetsWithoutDeleting(t *testing.T) {
	fake := &fakeEnrollment{expiresIn: 3600}
	registry := newFakeRegistry(fake)
	_, err := registry.bearer(context.Background(), testBaseURL)
	require.NoError(t, err)

	require.NoError(t, registry.release(context.Background(), forgetTenant))
	require.Empty(t, registry.principals, "a released principal is forgotten")

	_, err = registry.bearer(context.Background(), testBaseURL)
	require.NoError(t, err)
	require.Equal(t, 2, fake.enrolls, "a forgotten principal enrols again on next use")
}

// forgetTenant refuses nothing: whatever tenant it is handed, it reports
// success without touching the cluster.
func TestForgetTenant_NeverFails(t *testing.T) {
	require.NoError(t, forgetTenant(context.Background(), "", ""))
	require.NoError(t, forgetTenant(context.Background(), testBaseURL, "itest-tenant-1"))
}

// newFakeNamedRegistries returns named registries whose principal called
// name enrolls into tenant "tenant-<name>".
func newFakeNamedRegistries() *namedPrincipalRegistries {
	return newNamedPrincipalRegistries(func(name string) *principalRegistry {
		enroll := func(context.Context, string) (*enrolledPrincipal, error) {
			return &enrolledPrincipal{tenantID: "tenant-" + name}, nil
		}
		return newPrincipalRegistry("test "+name, enroll, (&fakeEnrollment{}).login)
	})
}

func TestNamedPrincipal_EachNameIsItsOwnTenant(t *testing.T) {
	named := newFakeNamedRegistries()

	alpha, err := named.registry("alpha")
	require.NoError(t, err)
	beta, err := named.registry("beta")
	require.NoError(t, err)
	again, err := named.registry("alpha")
	require.NoError(t, err)
	require.Same(t, alpha, again, "a name keeps its principal across calls")

	_, err = alpha.bearer(context.Background(), testBaseURL)
	require.NoError(t, err)
	_, err = beta.bearer(context.Background(), testBaseURL)
	require.NoError(t, err)
	require.Equal(t, "tenant-alpha", alpha.tenantID(testBaseURL))
	require.Equal(t, "tenant-beta", beta.tenantID(testBaseURL))

	fake := &fakeTenantDeletes{}
	require.NoError(t, named.release(context.Background(), fake.delete))
	require.ElementsMatch(t, []string{"tenant-alpha", "tenant-beta"}, fake.deleted,
		"release deletes the tenant of every named principal")
}

func TestNamedPrincipal_RefusesNamesThatCannotNameATenant(t *testing.T) {
	named := newFakeNamedRegistries()

	for _, name := range []string{"", "Tenant-A", "tenant/a", "tenant a"} {
		registry, err := named.registry(name)
		require.Nil(t, registry, "name %q", name)
		var nameErr *PrincipalNameError
		require.ErrorAs(t, err, &nameErr, "name %q", name)
		require.Equal(t, name, nameErr.Name)
	}
	require.Empty(t, named.registries, "a refused name creates no principal")
}
