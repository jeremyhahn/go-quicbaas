//go:build rest

package rest

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// TestRegisterSelf_ARetryStepsPastTheIDADiscardedRegistrationRetired: the
// engine retires a deleted tenant id for good, so after a registration under a
// tenant name is discarded, a retry under the same name is given the next
// candidate rather than refused at provisioning.
func TestRegisterSelf_ARetryStepsPastTheIDADiscardedRegistrationRetired(t *testing.T) {
	handler := setupSelfRegHandler(t, brokerSelfRegConfig())
	svc := handler.server.service.(*selfRegService)
	svc.tenants.retireOnDelete = true
	newXKMSCeremony(svc, false)

	first := selfRegBegin(t, handler, "Acme Inc", "jane@acme.example")
	handler.reapRegistration(context.Background(), first)
	require.Empty(t, tenantIDs(t, handler), "the reaper removed the abandoned tenant")
	require.True(t, svc.tenants.retired["acme-inc"], "the engine retired the deleted id")

	retry := selfRegBegin(t, handler, "Acme Inc", "jane@acme.example")
	assert.Equal(t, []string{"acme-inc-2"}, tenantIDs(t, handler), "the retry is given the next candidate")
	require.Len(t, usersIn(t, handler, "acme-inc-2"), 1)

	w := selfRegFinish(t, handler, retry)
	require.Equal(t, http.StatusOK, w.Code, "finish body: %s", w.Body.String())
	users := usersIn(t, handler, "acme-inc-2")
	require.Len(t, users, 1)
	assert.Equal(t, iam.LifecycleActive, users[0].Lifecycle)
}

// TestRegisterSelf_ReaperKeepsARegistrationNoLongerItsOwn: the session's
// expiry discards only what is still the registration's own. An account an
// operator approved while the ceremony was open, or a tenant another user
// joined, is kept with everything in it.
func TestRegisterSelf_ReaperKeepsARegistrationNoLongerItsOwn(t *testing.T) {
	tests := []struct {
		name  string
		after func(t *testing.T, svc *selfRegService, registrant *iam.User)
		users int
	}{
		{
			name: "the registrant activated",
			after: func(t *testing.T, svc *selfRegService, registrant *iam.User) {
				active := iam.LifecycleActive
				_, err := svc.store.UpdateUser(context.Background(), "acme-inc", registrant.ID,
					iam.UpdateUserOptions{Lifecycle: &active})
				require.NoError(t, err)
			},
			users: 1,
		},
		{
			name: "a second account",
			after: func(t *testing.T, svc *selfRegService, _ *iam.User) {
				_, err := svc.store.CreateUser(context.Background(), "acme-inc", iam.CreateUserOptions{
					Email: "bob@acme.example", Username: "bob", Name: "Bob",
				})
				require.NoError(t, err)
			},
			users: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := setupSelfRegHandler(t, brokerSelfRegConfig())
			svc := handler.server.service.(*selfRegService)
			newXKMSCeremony(svc, false)
			sessionID := selfRegBegin(t, handler, "Acme Inc", "jane@acme.example")
			users := usersIn(t, handler, "acme-inc")
			require.Len(t, users, 1)
			tt.after(t, svc, users[0])

			handler.reapRegistration(context.Background(), sessionID)

			assert.Equal(t, []string{"acme-inc"}, tenantIDs(t, handler), "the tenant is kept")
			assert.Len(t, usersIn(t, handler, "acme-inc"), tt.users, "every account is kept")
		})
	}
}

// TestRegisterSelf_ARefusedFinishKeepsAnApprovedAccount: a finish the
// ceremony refuses discards only a registration that is still its own; an
// account an operator approved meanwhile is kept.
func TestRegisterSelf_ARefusedFinishKeepsAnApprovedAccount(t *testing.T) {
	handler := setupSelfRegHandler(t, brokerSelfRegConfig())
	svc := handler.server.service.(*selfRegService)
	ceremony := newXKMSCeremony(svc, false)
	ceremony.failFinish = true
	sessionID := selfRegBegin(t, handler, "Acme Inc", "jane@acme.example")
	users := usersIn(t, handler, "acme-inc")
	require.Len(t, users, 1)
	active := iam.LifecycleActive
	_, err := svc.store.UpdateUser(context.Background(), "acme-inc", users[0].ID,
		iam.UpdateUserOptions{Lifecycle: &active})
	require.NoError(t, err)

	w := selfRegFinish(t, handler, sessionID)
	require.NotEqual(t, http.StatusOK, w.Code, "finish body: %s", w.Body.String())

	assert.Equal(t, []string{"acme-inc"}, tenantIDs(t, handler))
	assert.Len(t, usersIn(t, handler, "acme-inc"), 1)
}

// TestRegisterSelf_TheRecordOutlivesAnUnactivatedAccount: with no gate holding
// the account, the tenant's registration record is removed only after the
// account is active. A failure to remove it leaves the active account and its
// tenant in place, and the sweep keeps them; it is never an account pending
// with a bound credential and no record for anything to recover it by.
func TestRegisterSelf_TheRecordOutlivesAnUnactivatedAccount(t *testing.T) {
	handler := setupSelfRegHandler(t, brokerSelfRegConfig())
	svc := handler.server.service.(*selfRegService)
	newXKMSCeremony(svc, false)
	svc.tenants.finishErr = errors.New("tenant store unavailable")
	sessionID := selfRegBegin(t, handler, "Acme Inc", "jane@acme.example")

	w := selfRegFinish(t, handler, sessionID)
	require.Equal(t, http.StatusOK, w.Code, "finish body: %s", w.Body.String())

	users := usersIn(t, handler, "acme-inc")
	require.Len(t, users, 1)
	assert.Equal(t, iam.LifecycleActive, users[0].Lifecycle, "the account was activated before the record went")
	tenant, err := svc.tenants.GetTenant(context.Background(), "acme-inc")
	require.NoError(t, err)
	assert.NotNil(t, tenant.SelfRegistration, "the record removal failed")

	sweeper, _ := newTestSweeper(handler, &testClock{at: afterExpiry()})
	var inUse *SelfRegistrationTenantInUseError
	require.ErrorAs(t, sweeper.sweep(context.Background()), &inUse)
	assert.Equal(t, []string{"acme-inc"}, tenantIDs(t, handler), "the sweep keeps the activated tenant")
	assert.Len(t, usersIn(t, handler, "acme-inc"), 1)
}
