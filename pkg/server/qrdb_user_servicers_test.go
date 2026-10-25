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

package server

import (
	"context"
	"errors"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// recordingUserServicer records which delegate methods were invoked so the
// proxy tests can assert per-call resolution and argument passthrough.
type recordingUserServicer struct {
	qrdbsdk.UserServicer

	calls []string
	user  *qrdbsdk.User
}

func (r *recordingUserServicer) GetUserByUsername(_ context.Context, tenantID, username string) (*qrdbsdk.User, error) {
	r.calls = append(r.calls, "GetUserByUsername:"+tenantID+":"+username)
	return r.user, nil
}

func (r *recordingUserServicer) GetUserByEmail(_ context.Context, tenantID, email string) (*qrdbsdk.User, error) {
	r.calls = append(r.calls, "GetUserByEmail:"+tenantID+":"+email)
	return r.user, nil
}

func (r *recordingUserServicer) GetUser(_ context.Context, id uint64) (*qrdbsdk.User, error) {
	r.calls = append(r.calls, "GetUser")
	return r.user, nil
}

func (r *recordingUserServicer) DeleteUser(_ context.Context, id uint64) error {
	r.calls = append(r.calls, "DeleteUser")
	return nil
}

// recordingUserAuthFactorServicer records delegate invocations for the
// auth-factor proxy tests.
type recordingUserAuthFactorServicer struct {
	calls  []string
	factor *qrdbsdk.AuthFactor
}

func (r *recordingUserAuthFactorServicer) Add(_ context.Context, userID uint64, factor *qrdbsdk.AuthFactor) error {
	r.calls = append(r.calls, "Add")
	return nil
}

func (r *recordingUserAuthFactorServicer) AddConditional(_ context.Context, userID uint64, factor *qrdbsdk.AuthFactor, cond qrdbsdk.FactorAddCondition) (uint64, error) {
	r.calls = append(r.calls, "AddConditional")
	return 1, nil
}

func (r *recordingUserAuthFactorServicer) FactorSetVersion(_ context.Context, userID uint64) (uint64, error) {
	r.calls = append(r.calls, "FactorSetVersion")
	return 1, nil
}

func (r *recordingUserAuthFactorServicer) List(_ context.Context, userID uint64) ([]*qrdbsdk.AuthFactor, error) {
	r.calls = append(r.calls, "List")
	return []*qrdbsdk.AuthFactor{r.factor}, nil
}

func (r *recordingUserAuthFactorServicer) Get(_ context.Context, factorID string) (*qrdbsdk.AuthFactor, error) {
	r.calls = append(r.calls, "Get")
	return r.factor, nil
}

func (r *recordingUserAuthFactorServicer) LookupByCredentialKey(_ context.Context, t qrdbsdk.AuthFactorType, key []byte) (*qrdbsdk.AuthFactor, error) {
	r.calls = append(r.calls, "LookupByCredentialKey")
	return r.factor, nil
}

func (r *recordingUserAuthFactorServicer) Remove(_ context.Context, factorID string, force bool) error {
	r.calls = append(r.calls, "Remove")
	return nil
}

func (r *recordingUserAuthFactorServicer) Delete(_ context.Context, factorID string, force bool) error {
	r.calls = append(r.calls, "Delete")
	return nil
}

func (r *recordingUserAuthFactorServicer) Rotate(_ context.Context, oldID string, newFactor *qrdbsdk.AuthFactor) error {
	r.calls = append(r.calls, "Rotate")
	return nil
}

func (r *recordingUserAuthFactorServicer) Relabel(_ context.Context, factorID string, label string) error {
	r.calls = append(r.calls, "Relabel")
	return nil
}

func (r *recordingUserAuthFactorServicer) MarkUsed(_ context.Context, factorID string, ts time.Time) error {
	r.calls = append(r.calls, "MarkUsed")
	return nil
}

// TestQRDBUserServicerProxy_UnavailableFailsClosed proves every proxy method
// fails closed with the typed unavailability error while the embedded node is
// absent or sealed (resolver returns nil), instead of panicking on a nil
// servicer — the seal-enabled boot window.
func TestQRDBUserServicerProxy_UnavailableFailsClosed(t *testing.T) {
	ctx := context.Background()
	p := &qrdbUserServicerProxy{resolve: func() qrdbsdk.UserServicer { return nil }}

	var unavailable *QRDBUserServiceUnavailableError

	if _, err := p.GetUserByUsername(ctx, "system", "admin"); !errors.As(err, &unavailable) {
		t.Fatalf("GetUserByUsername: expected QRDBUserServiceUnavailableError, got %v", err)
	}
	if unavailable.Surface != "user" {
		t.Fatalf("expected user surface, got %q", unavailable.Surface)
	}
	if _, err := p.GetUser(ctx, 1); !errors.As(err, &unavailable) {
		t.Fatalf("GetUser: expected unavailability error, got %v", err)
	}
	if _, err := p.GetUserByEmail(ctx, "system", "admin@example.com"); !errors.As(err, &unavailable) {
		t.Fatalf("GetUserByEmail: expected unavailability error, got %v", err)
	}
	if _, err := p.CreateUser(ctx, &qrdbsdk.CreateUserRequest{}); !errors.As(err, &unavailable) {
		t.Fatalf("CreateUser: expected unavailability error, got %v", err)
	}
	if _, err := p.UserByCertThumbprint(ctx, "tp"); !errors.As(err, &unavailable) {
		t.Fatalf("UserByCertThumbprint: expected unavailability error, got %v", err)
	}
	if _, err := p.AddCertBinding(ctx, 1, "tp", 0); !errors.As(err, &unavailable) {
		t.Fatalf("AddCertBinding: expected unavailability error, got %v", err)
	}
	if _, err := p.UpdateUser(ctx, &qrdbsdk.UpdateUserRequest{}); !errors.As(err, &unavailable) {
		t.Fatalf("UpdateUser: expected unavailability error, got %v", err)
	}
	if err := p.DeleteUser(ctx, 1); !errors.As(err, &unavailable) {
		t.Fatalf("DeleteUser: expected unavailability error, got %v", err)
	}
	if _, err := p.ListUsers(ctx, &qrdbsdk.ListUsersRequest{}); !errors.As(err, &unavailable) {
		t.Fatalf("ListUsers: expected unavailability error, got %v", err)
	}
	if err := p.EnableUser(ctx, 1); !errors.As(err, &unavailable) {
		t.Fatalf("EnableUser: expected unavailability error, got %v", err)
	}
	if err := p.DisableUser(ctx, 1); !errors.As(err, &unavailable) {
		t.Fatalf("DisableUser: expected unavailability error, got %v", err)
	}

	// A nil resolver (zero-value proxy) must also fail closed.
	zero := &qrdbUserServicerProxy{}
	if _, err := zero.GetUser(ctx, 1); !errors.As(err, &unavailable) {
		t.Fatalf("zero-value proxy: expected unavailability error, got %v", err)
	}
}

// TestQRDBUserServicerProxy_ResolvesPerCall proves the proxy resolves the
// delegate on every call, so a servicer that becomes available AFTER
// registration (post-Unseal) starts serving without re-registration.
func TestQRDBUserServicerProxy_ResolvesPerCall(t *testing.T) {
	ctx := context.Background()

	var live *recordingUserServicer // nil until "unseal"
	p := &qrdbUserServicerProxy{resolve: func() qrdbsdk.UserServicer {
		if live == nil {
			return nil
		}
		return live
	}}

	// Sealed window: fail closed.
	if _, err := p.GetUserByUsername(ctx, "system", "admin"); err == nil {
		t.Fatal("expected failure while servicer is unavailable")
	}

	// "Unseal": the same registered proxy now serves.
	live = &recordingUserServicer{user: &qrdbsdk.User{ID: 42, Username: "admin"}}
	u, err := p.GetUserByUsername(ctx, "system", "admin")
	if err != nil {
		t.Fatalf("post-unseal lookup failed: %v", err)
	}
	if u.ID != 42 {
		t.Fatalf("expected user 42, got %+v", u)
	}
	if len(live.calls) != 1 || live.calls[0] != "GetUserByUsername:system:admin" {
		t.Fatalf("expected delegated call with passthrough args, got %v", live.calls)
	}

	// The email login key resolves through the same live delegate.
	if _, err := p.GetUserByEmail(ctx, "system", "admin@example.com"); err != nil {
		t.Fatalf("post-unseal email lookup failed: %v", err)
	}
	if len(live.calls) != 2 || live.calls[1] != "GetUserByEmail:system:admin@example.com" {
		t.Fatalf("expected delegated email lookup with passthrough args, got %v", live.calls)
	}
}

// TestQRDBUserAuthFactorServicerProxy_UnavailableFailsClosed proves every
// auth-factor proxy method fails closed with the typed unavailability error
// while the node servicer is absent.
func TestQRDBUserAuthFactorServicerProxy_UnavailableFailsClosed(t *testing.T) {
	ctx := context.Background()
	p := &qrdbUserAuthFactorServicerProxy{resolve: func() qrdbsdk.UserAuthFactorServicer { return nil }}

	var unavailable *QRDBUserServiceUnavailableError

	if err := p.Add(ctx, 1, &qrdbsdk.AuthFactor{}); !errors.As(err, &unavailable) {
		t.Fatalf("Add: expected QRDBUserServiceUnavailableError, got %v", err)
	}
	if unavailable.Surface != "user_authfactor" {
		t.Fatalf("expected user_authfactor surface, got %q", unavailable.Surface)
	}
	if _, err := p.List(ctx, 1); !errors.As(err, &unavailable) {
		t.Fatalf("List: expected unavailability error, got %v", err)
	}
	if _, err := p.Get(ctx, "f1"); !errors.As(err, &unavailable) {
		t.Fatalf("Get: expected unavailability error, got %v", err)
	}
	if _, err := p.LookupByCredentialKey(ctx, qrdbsdk.AuthFactorMTLS, []byte{1}); !errors.As(err, &unavailable) {
		t.Fatalf("LookupByCredentialKey: expected unavailability error, got %v", err)
	}
	if err := p.Remove(ctx, "f1", false); !errors.As(err, &unavailable) {
		t.Fatalf("Remove: expected unavailability error, got %v", err)
	}
	if err := p.Rotate(ctx, "f1", &qrdbsdk.AuthFactor{}); !errors.As(err, &unavailable) {
		t.Fatalf("Rotate: expected unavailability error, got %v", err)
	}
	if err := p.Relabel(ctx, "f1", "x"); !errors.As(err, &unavailable) {
		t.Fatalf("Relabel: expected unavailability error, got %v", err)
	}
	if err := p.MarkUsed(ctx, "f1", time.Now()); !errors.As(err, &unavailable) {
		t.Fatalf("MarkUsed: expected unavailability error, got %v", err)
	}
	cond := qrdbsdk.FactorAddCondition{NoActiveOfType: true}
	if _, err := p.AddConditional(ctx, 1, &qrdbsdk.AuthFactor{}, cond); !errors.As(err, &unavailable) {
		t.Fatalf("AddConditional: expected unavailability error, got %v", err)
	}
	if _, err := p.FactorSetVersion(ctx, 1); !errors.As(err, &unavailable) {
		t.Fatalf("FactorSetVersion: expected unavailability error, got %v", err)
	}
	if err := p.Delete(ctx, "f1", false); !errors.As(err, &unavailable) {
		t.Fatalf("Delete: expected unavailability error, got %v", err)
	}
}

// TestQRDBUserAuthFactorServicerProxy_ConditionalVerbsDelegate proves the
// conditional add, the factor-set version read and the hard delete reach the
// live servicer once it is available, returning its results unchanged.
func TestQRDBUserAuthFactorServicerProxy_ConditionalVerbsDelegate(t *testing.T) {
	ctx := context.Background()
	live := &recordingUserAuthFactorServicer{}
	p := &qrdbUserAuthFactorServicerProxy{resolve: func() qrdbsdk.UserAuthFactorServicer { return live }}

	cond := qrdbsdk.FactorAddCondition{NoActiveOfType: true}
	version, err := p.AddConditional(ctx, 42, &qrdbsdk.AuthFactor{Type: qrdbsdk.AuthFactorMTLS}, cond)
	if err != nil {
		t.Fatalf("AddConditional failed: %v", err)
	}
	if version != 1 {
		t.Fatalf("AddConditional: expected version 1, got %d", version)
	}
	if version, err = p.FactorSetVersion(ctx, 42); err != nil || version != 1 {
		t.Fatalf("FactorSetVersion: expected (1, nil), got (%d, %v)", version, err)
	}
	if err := p.Delete(ctx, "f1", true); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	want := []string{"AddConditional", "FactorSetVersion", "Delete"}
	if len(live.calls) != len(want) {
		t.Fatalf("expected %v delegations, got %v", want, live.calls)
	}
	for i := range want {
		if live.calls[i] != want[i] {
			t.Fatalf("expected %v delegations, got %v", want, live.calls)
		}
	}
}

// TestQRDBUserAuthFactorServicerProxy_AddConditionalRefusesAssertedAttestation
// proves the conditional add enforces the same attestation contract as Add: a
// caller-asserted attestation level is refused before the node is asked.
func TestQRDBUserAuthFactorServicerProxy_AddConditionalRefusesAssertedAttestation(t *testing.T) {
	live := &recordingUserAuthFactorServicer{}
	p := &qrdbUserAuthFactorServicerProxy{resolve: func() qrdbsdk.UserAuthFactorServicer { return live }}

	factor := &qrdbsdk.AuthFactor{
		Type:     qrdbsdk.AuthFactorFIDO2,
		Metadata: map[string]string{iam.AuthFactorMetadataAttestLevel: "hardware"},
	}
	cond := qrdbsdk.FactorAddCondition{NoActiveOfType: true}
	_, err := p.AddConditional(context.Background(), 42, factor, cond)
	var typed *iam.AuthFactorAttestationMetadataError
	if !errors.As(err, &typed) {
		t.Fatalf("want *iam.AuthFactorAttestationMetadataError, got %v", err)
	}
	if len(live.calls) != 0 {
		t.Fatalf("a refused AddConditional still reached qrdb: %v", live.calls)
	}
}

// TestQRDBUserAuthFactorServicerProxy_ResolvesPerCall proves the auth-factor
// proxy delegates to the live servicer once it becomes available.
func TestQRDBUserAuthFactorServicerProxy_ResolvesPerCall(t *testing.T) {
	ctx := context.Background()

	var live *recordingUserAuthFactorServicer
	p := &qrdbUserAuthFactorServicerProxy{resolve: func() qrdbsdk.UserAuthFactorServicer {
		if live == nil {
			return nil
		}
		return live
	}}

	if err := p.Add(ctx, 1, &qrdbsdk.AuthFactor{}); err == nil {
		t.Fatal("expected failure while servicer is unavailable")
	}

	live = &recordingUserAuthFactorServicer{factor: &qrdbsdk.AuthFactor{ID: "f1", UserID: 42}}
	if err := p.Add(ctx, 42, &qrdbsdk.AuthFactor{Type: qrdbsdk.AuthFactorMTLS}); err != nil {
		t.Fatalf("post-unseal Add failed: %v", err)
	}
	factors, err := p.List(ctx, 42)
	if err != nil {
		t.Fatalf("post-unseal List failed: %v", err)
	}
	if len(factors) != 1 || factors[0].ID != "f1" {
		t.Fatalf("unexpected factors: %+v", factors)
	}
	if len(live.calls) != 2 || live.calls[0] != "Add" || live.calls[1] != "List" {
		t.Fatalf("expected [Add List] delegations, got %v", live.calls)
	}
}

// TestBuildQRDBRegisterOptions_AlwaysIncludesUserSurface proves the qrdb.v1
// gRPC registration always carries the user + user-auth-factor register
// options (via the lazy proxies) even when the credential / ceremony /
// barrier services are not wired — the exact Phase 8 transport-parity gap
// that left the SDK factor surface without a wire on gRPC and Unix.
func TestBuildQRDBRegisterOptions_AlwaysIncludesUserSurface(t *testing.T) {
	s := &Server{}

	opts := s.buildQRDBRegisterOptions()
	if len(opts) != 2 {
		t.Fatalf("expected exactly the 2 user-surface options on a bare server, got %d", len(opts))
	}
	for i, opt := range opts {
		if opt == nil {
			t.Fatalf("option %d is nil", i)
		}
	}
}

// TestQRDBUserServiceUnavailableError_Message pins the operator-facing
// message format.
func TestQRDBUserServiceUnavailableError_Message(t *testing.T) {
	err := &QRDBUserServiceUnavailableError{Surface: "user"}
	want := "qrdb user service unavailable (embedded node absent or sealed)"
	if err.Error() != want {
		t.Fatalf("expected %q, got %q", want, err.Error())
	}
}
