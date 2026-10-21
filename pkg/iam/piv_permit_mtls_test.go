package iam

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

// newPIVPolicyTest is a rollback fixture whose pending user carries the given
// permitted-method policy before activation.
func newPIVPolicyTest(t *testing.T, methods []string) *pivRollbackTest {
	t.Helper()
	rt := newPIVRollbackTest(t, newMockUserStorage(), nil)
	setPIVTestAuthMethods(t, rt, methods)
	return rt
}

func setPIVTestAuthMethods(t *testing.T, rt *pivRollbackTest, methods []string) {
	t.Helper()
	ctx := context.Background()
	user, err := rt.users.GetUser(ctx, "t1", 1)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	user.AuthMethods = methods
	user.Updated = user.Updated.Add(time.Second)
	if err := rt.users.UpdateUser(ctx, user); err != nil {
		t.Fatalf("set auth methods: %v", err)
	}
}

func pivTestAuthMethods(t *testing.T, rt *pivRollbackTest) []string {
	t.Helper()
	user, err := rt.users.GetUser(context.Background(), "t1", 1)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	return user.AuthMethods
}

// An activated PIV user must be able to log in with the activated
// certificate, which go-xkms admits only when the policy permits mtls.
func TestPIVActivator_PermitsMTLSBesidePIV(t *testing.T) {
	rt := newPIVPolicyTest(t, []string{AuthMethodPIV})
	_, certPEM := rt.leaf(t)
	if err := rt.act.Activate(context.Background(), "t1", rt.token, certPEM); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	want := []string{AuthMethodPIV, AuthMethodMTLS}
	if got := pivTestAuthMethods(t, rt); !slices.Equal(got, want) {
		t.Errorf("AuthMethods = %v, want %v", got, want)
	}
}

func TestPIVActivator_PermitMTLSIsIdempotent(t *testing.T) {
	ctx := context.Background()
	rt := newPIVPolicyTest(t, []string{AuthMethodPIV, AuthMethodMTLS})
	_, firstPEM := rt.leaf(t)
	if err := rt.act.Activate(ctx, "t1", rt.token, firstPEM); err != nil {
		t.Fatalf("first Activate: %v", err)
	}

	// Return the user to pending and activate again through a new invite.
	user, err := rt.users.GetUser(ctx, "t1", 1)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	user.Lifecycle = LifecyclePending
	user.Updated = user.Updated.Add(time.Second)
	if err := rt.users.UpdateUser(ctx, user); err != nil {
		t.Fatalf("reset to pending: %v", err)
	}
	token, _, err := rt.invites.Create(ctx, "t1", 1, time.Hour)
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	_, secondPEM := rt.leaf(t)
	if err := rt.act.Activate(ctx, "t1", token, secondPEM); err != nil {
		t.Fatalf("second Activate: %v", err)
	}

	want := []string{AuthMethodPIV, AuthMethodMTLS}
	if got := pivTestAuthMethods(t, rt); !slices.Equal(got, want) {
		t.Errorf("AuthMethods = %v, want %v", got, want)
	}
}

// An empty policy permits every method; adding mtls would narrow it.
func TestPIVActivator_EmptyPolicyStaysEmpty(t *testing.T) {
	rt := newPIVPolicyTest(t, nil)
	_, certPEM := rt.leaf(t)
	if err := rt.act.Activate(context.Background(), "t1", rt.token, certPEM); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if got := pivTestAuthMethods(t, rt); len(got) != 0 {
		t.Errorf("AuthMethods = %v, want empty", got)
	}
}

func TestPIVActivator_FailedActivationPermitsNothing(t *testing.T) {
	tests := []struct {
		name     string
		activate func(t *testing.T, rt *pivRollbackTest) error
		wantErr  error
	}{
		{
			name: "bad token",
			activate: func(t *testing.T, rt *pivRollbackTest) error {
				_, certPEM := rt.leaf(t)
				return rt.act.Activate(context.Background(), "t1", "not-a-token", certPEM)
			},
			wantErr: ErrInviteNotFound,
		},
		{
			name: "untrusted certificate",
			activate: func(t *testing.T, rt *pivRollbackTest) error {
				otherCA, otherKey := pivTestCA(t)
				_, certPEM := pivTestLeaf(t, otherCA, otherKey, true)
				return rt.act.Activate(context.Background(), "t1", rt.token, certPEM)
			},
			wantErr: ErrPIVCertRejected,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := newPIVPolicyTest(t, []string{AuthMethodPIV})
			if err := tt.activate(t, rt); !errors.Is(err, tt.wantErr) {
				t.Fatalf("Activate error = %v, want %v", err, tt.wantErr)
			}
			want := []string{AuthMethodPIV}
			if got := pivTestAuthMethods(t, rt); !slices.Equal(got, want) {
				t.Errorf("AuthMethods = %v, want %v", got, want)
			}
		})
	}
}
