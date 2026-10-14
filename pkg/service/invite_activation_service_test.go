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
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam/iamtest"
)

// inviteTestSource is an InviteActivationSource over fixed components.
type inviteTestSource struct {
	invites  iam.InviteStore
	identity *iam.IdentityStore
	manager  WebAuthnManager
}

func (s *inviteTestSource) GetInviteStore(context.Context) (iam.InviteStore, error) {
	return s.invites, nil
}

func (s *inviteTestSource) GetIdentityStore(context.Context) (*iam.IdentityStore, error) {
	return s.identity, nil
}

func (s *inviteTestSource) GetWebAuthnManager(context.Context) (WebAuthnManager, error) {
	return s.manager, nil
}

func (s *inviteTestSource) GetPIVActivator(context.Context) (*iam.PIVActivator, error) {
	return nil, nil
}

func (s *inviteTestSource) GetAuditLogger(context.Context) (iam.AuditLogger, error) {
	return nil, nil
}

// recordingWebAuthnManager records the email each ceremony was addressed by
// and fails FinishInviteRegistration with finishErr.
type recordingWebAuthnManager struct {
	stubWebAuthnManager
	beginEmail  string
	finishEmail string
	finishErr   error
}

func (m *recordingWebAuthnManager) BeginInviteRegistration(_ context.Context, _, email string) (map[string]interface{}, error) {
	m.beginEmail = email
	return map[string]interface{}{"challenge": "c"}, nil
}

func (m *recordingWebAuthnManager) FinishInviteRegistration(_ context.Context, _, email string, _ map[string]interface{}) error {
	m.finishEmail = email
	return m.finishErr
}

// fixedInviteStore returns one record from Lookup whatever it holds, and
// fails Redeem with redeemErr. It models a store that hands back a spent or
// expired record instead of refusing, and one whose redeem write fails.
type fixedInviteStore struct {
	record    iam.InviteRecord
	redeemErr error
}

func (s *fixedInviteStore) Create(context.Context, string, uint64, time.Duration) (string, *iam.InviteRecord, error) {
	return "", nil, nil
}

func (s *fixedInviteStore) Lookup(context.Context, string, string) (*iam.InviteRecord, error) {
	copied := s.record
	return &copied, nil
}

func (s *fixedInviteStore) Redeem(context.Context, string, string) (*iam.InviteRecord, error) {
	return nil, s.redeemErr
}

var errInviteTestFailure = errors.New("injected failure")

// inviteFixture is a pending user with a live invite in tenant t1.
type inviteFixture struct {
	source  *inviteTestSource
	manager *recordingWebAuthnManager
	invites *iamtest.MemoryInviteStore
	userID  uint64
	token   string
}

func newInviteFixture(t *testing.T) *inviteFixture {
	t.Helper()
	identity, userID := newCredentialTestStore(t)
	invites := iamtest.NewMemoryInviteStore()
	token, _, err := invites.Create(context.Background(), "t1", userID, time.Hour)
	if err != nil {
		t.Fatalf("Create invite: %v", err)
	}
	manager := &recordingWebAuthnManager{}
	return &inviteFixture{
		source:  &inviteTestSource{invites: invites, identity: identity, manager: manager},
		manager: manager,
		invites: invites,
		userID:  userID,
		token:   token,
	}
}

func (f *inviteFixture) service() InviteActivationService {
	return NewInviteActivationService(f.source)
}

// TestInviteActivationServiceScopeValidation fails closed on an incomplete
// scope on every verb. The errors are compared by identity because errors.Is
// compares QBError codes, and all four required-field errors share one.
func TestInviteActivationServiceScopeValidation(t *testing.T) {
	svc := newInviteFixture(t).service()
	ctx := context.Background()
	credential := map[string]interface{}{"id": "x"}

	calls := map[string]func(tenantID, token string) error{
		"GetInvite": func(tenantID, token string) error {
			_, err := svc.GetInvite(ctx, tenantID, token)
			return err
		},
		"ActivateFIDO2Begin": func(tenantID, token string) error {
			_, err := svc.ActivateFIDO2Begin(ctx, tenantID, token)
			return err
		},
		"ActivateFIDO2Finish": func(tenantID, token string) error {
			_, err := svc.ActivateFIDO2Finish(ctx, tenantID, token, credential)
			return err
		},
		"ActivatePIV": func(tenantID, token string) error {
			_, err := svc.ActivatePIV(ctx, tenantID, token, []byte("pem"))
			return err
		},
	}
	for name, call := range calls {
		if err := call("", "tok"); err != ErrInviteActivationTenantRequired {
			t.Errorf("%s missing tenant: err = %v, want ErrInviteActivationTenantRequired", name, err)
		}
		if err := call("t1", ""); err != ErrInviteActivationTokenRequired {
			t.Errorf("%s missing token: err = %v, want ErrInviteActivationTokenRequired", name, err)
		}
	}
}

// TestInviteActivationServiceInviteStoreNotWired surfaces a missing invite
// store as a wiring fault rather than as an unusable invite: a node without
// its store must not tell every invitee their link is bad.
func TestInviteActivationServiceInviteStoreNotWired(t *testing.T) {
	f := newInviteFixture(t)
	f.source.invites = nil

	_, err := f.service().GetInvite(context.Background(), "t1", f.token)
	if err != ErrInviteActivationInviteStoreNotWired {
		t.Fatalf("err = %v, want ErrInviteActivationInviteStoreNotWired", err)
	}
	if iam.IsInviteUnusable(err) {
		t.Error("a wiring fault was reported as an unusable invite")
	}
}

// TestInviteActivationServiceGetInvite returns the invited user's display
// fields, defaulting the activation method to a passkey.
func TestInviteActivationServiceGetInvite(t *testing.T) {
	f := newInviteFixture(t)

	meta, err := f.service().GetInvite(context.Background(), "t1", f.token)
	if err != nil {
		t.Fatalf("GetInvite: %v", err)
	}
	if meta.UserID != f.userID || meta.Email != "alice@example.com" {
		t.Errorf("meta = %+v, want user %d with alice@example.com", meta, f.userID)
	}
	if len(meta.AuthMethods) != 1 || meta.AuthMethods[0] != "webauthn" {
		t.Errorf("AuthMethods = %v, want [webauthn]", meta.AuthMethods)
	}
}

// TestInviteActivationServiceTenantIsolation does not resolve a token in a
// tenant other than the one that issued it.
func TestInviteActivationServiceTenantIsolation(t *testing.T) {
	f := newInviteFixture(t)

	_, err := f.service().GetInvite(context.Background(), "t2", f.token)
	if !iam.IsInviteUnusable(err) {
		t.Fatalf("err = %v, want an unusable invite", err)
	}
}

// TestInviteActivationServiceRefusesReturnedUnusableRecord treats a spent or
// expired record a store hands back exactly like one the store refused.
func TestInviteActivationServiceRefusesReturnedUnusableRecord(t *testing.T) {
	records := map[string]iam.InviteRecord{
		"spent":   {TenantID: "t1", UserID: 10001, Used: true, ExpiresAt: time.Now().Add(time.Hour)},
		"expired": {TenantID: "t1", UserID: 10001, ExpiresAt: time.Now().Add(-time.Hour)},
	}
	for name, record := range records {
		f := newInviteFixture(t)
		f.source.invites = &fixedInviteStore{record: record}

		if _, err := f.service().ActivateFIDO2Begin(context.Background(), "t1", "tok"); !iam.IsInviteUnusable(err) {
			t.Errorf("%s: err = %v, want an unusable invite", name, err)
		}
	}
}

// TestInviteActivationServiceFIDO2Begin addresses the ceremony by the
// invited user's email, the key go-xkms stores the registration under.
func TestInviteActivationServiceFIDO2Begin(t *testing.T) {
	f := newInviteFixture(t)

	options, err := f.service().ActivateFIDO2Begin(context.Background(), "t1", f.token)
	if err != nil {
		t.Fatalf("ActivateFIDO2Begin: %v", err)
	}
	if options["challenge"] != "c" {
		t.Errorf("options = %v, want the manager's options", options)
	}
	if f.manager.beginEmail != "alice@example.com" {
		t.Errorf("ceremony email = %q, want alice@example.com", f.manager.beginEmail)
	}
}

// TestInviteActivationServiceFIDO2BeginUnconfigured reports each missing
// component by its own typed error.
func TestInviteActivationServiceFIDO2BeginUnconfigured(t *testing.T) {
	f := newInviteFixture(t)
	f.source.manager = nil
	if _, err := f.service().ActivateFIDO2Begin(context.Background(), "t1", f.token); err != ErrInviteActivationWebAuthnNotConfigured {
		t.Errorf("no manager: err = %v, want ErrInviteActivationWebAuthnNotConfigured", err)
	}

	f = newInviteFixture(t)
	f.source.identity = nil
	if _, err := f.service().ActivateFIDO2Begin(context.Background(), "t1", f.token); err != ErrInviteActivationUserNoEmail {
		t.Errorf("no identity store: err = %v, want ErrInviteActivationUserNoEmail", err)
	}
}

// TestInviteActivationServiceFIDO2Finish activates the user and spends the
// invite, so the same link cannot activate a second authenticator.
func TestInviteActivationServiceFIDO2Finish(t *testing.T) {
	f := newInviteFixture(t)
	ctx := context.Background()

	activation, err := f.service().ActivateFIDO2Finish(ctx, "t1", f.token, map[string]interface{}{"id": "x"})
	if err != nil {
		t.Fatalf("ActivateFIDO2Finish: %v", err)
	}
	if activation.UserID != f.userID || activation.Warning != "" {
		t.Errorf("activation = %+v, want user %d without a warning", activation, f.userID)
	}
	if f.manager.finishEmail != "alice@example.com" {
		t.Errorf("ceremony email = %q, want alice@example.com", f.manager.finishEmail)
	}
	user, err := f.source.identity.GetUser(ctx, "t1", f.userID)
	if err != nil || user.Lifecycle != iam.LifecycleActive {
		t.Errorf("user lifecycle = %v (err %v), want active", user, err)
	}
	if _, err := f.invites.Lookup(ctx, "t1", f.token); !iam.IsInviteUnusable(err) {
		t.Errorf("invite still usable after activation: err = %v", err)
	}
}

// TestInviteActivationServiceFIDO2FinishMissingCredential checks the invite
// before the credential, so an unusable token gets the opaque answer whatever
// the body carried, and a usable one is told what is missing.
func TestInviteActivationServiceFIDO2FinishMissingCredential(t *testing.T) {
	f := newInviteFixture(t)
	ctx := context.Background()

	if _, err := f.service().ActivateFIDO2Finish(ctx, "t1", "no-such-token", nil); !iam.IsInviteUnusable(err) {
		t.Errorf("unknown token: err = %v, want an unusable invite", err)
	}
	if _, err := f.service().ActivateFIDO2Finish(ctx, "t1", f.token, nil); err != ErrInviteActivationCredentialRequired {
		t.Errorf("usable token: err = %v, want ErrInviteActivationCredentialRequired", err)
	}
}

// TestInviteActivationServiceFIDO2FinishCeremonyFailure keeps the invite
// when the ceremony fails, so the account stays activatable.
func TestInviteActivationServiceFIDO2FinishCeremonyFailure(t *testing.T) {
	f := newInviteFixture(t)
	f.manager.finishErr = errInviteTestFailure
	ctx := context.Background()

	if _, err := f.service().ActivateFIDO2Finish(ctx, "t1", f.token, map[string]interface{}{"id": "x"}); err != errInviteTestFailure {
		t.Fatalf("err = %v, want the ceremony failure", err)
	}
	if _, err := f.invites.Lookup(ctx, "t1", f.token); err != nil {
		t.Errorf("invite consumed by a failed ceremony: err = %v", err)
	}
}

// TestInviteActivationServiceFIDO2FinishRedeemFailure reports a redeem that
// failed rather than succeeding with a one-time token left redeemable.
func TestInviteActivationServiceFIDO2FinishRedeemFailure(t *testing.T) {
	f := newInviteFixture(t)
	f.source.invites = &fixedInviteStore{
		record:    iam.InviteRecord{TenantID: "t1", UserID: f.userID, ExpiresAt: time.Now().Add(time.Hour)},
		redeemErr: errInviteTestFailure,
	}

	if _, err := f.service().ActivateFIDO2Finish(context.Background(), "t1", "tok", map[string]interface{}{"id": "x"}); err != errInviteTestFailure {
		t.Fatalf("err = %v, want the redeem failure", err)
	}
}

// TestInviteActivationServicePIVValidation refuses an empty certificate and
// reports a node without a PIV trust pool as not configured.
func TestInviteActivationServicePIVValidation(t *testing.T) {
	f := newInviteFixture(t)
	ctx := context.Background()

	if _, err := f.service().ActivatePIV(ctx, "t1", f.token, nil); err != ErrInviteActivationCertRequired {
		t.Errorf("empty cert: err = %v, want ErrInviteActivationCertRequired", err)
	}
	if _, err := f.service().ActivatePIV(ctx, "t1", f.token, []byte("pem")); err != ErrInviteActivationPIVNotConfigured {
		t.Errorf("no activator: err = %v, want ErrInviteActivationPIVNotConfigured", err)
	}
}
