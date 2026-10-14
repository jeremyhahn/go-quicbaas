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

package qrdbadapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/qrdbnode"
)

// These tests boot a real qrdb node under the consensus-arbitration carve-out
// (see pkg/testutil/qrdbnode). The property they pin is that the STORE decides
// which certificate binding resolves a user after concurrent updates. A fake
// servicer agrees with whatever the adapter expects and so proves nothing
// about that; only qrdb's own cert-thumbprint index, written in the same
// transaction as the record, can.

// TestMain tears the shared qrdb node down once the package is finished with
// it. Booting is lazy, so a run that touches none of these tests starts
// nothing.
func TestMain(m *testing.M) {
	code := m.Run()
	qrdbnode.CloseShared()
	os.Exit(code)
}

// nodeUserServicerResolver hands the shared node's UserServicer to the storage.
type nodeUserServicerResolver struct {
	svc qrdbsdk.UserServicer
}

func (r nodeUserServicerResolver) UserServicer(context.Context) (qrdbsdk.UserServicer, error) {
	return r.svc, nil
}

// newNodeUserStorage builds a QRDBUserStorage over the shared real node.
func newNodeUserStorage(t *testing.T) *QRDBUserStorage {
	t.Helper()
	n := qrdbnode.Shared(t)
	return NewQRDBUserStorage(nodeUserServicerResolver{svc: n.Node.UserService()})
}

// nodeSubjects hands out distinct users and thumbprints so tests sharing the
// node cannot collide on one email, username or binding.
var nodeSubjects atomic.Uint64

// uniqueSubject returns a suffix no other test in this package uses.
func uniqueSubject() string {
	return fmt.Sprintf("%d", nodeSubjects.Add(1))
}

// legacyDigest returns a hex SHA-256 in the legacy PIV thumbprint shape.
func legacyDigest(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

// newNodeUser returns a user in testTenant with a unique login key.
func newNodeUser(subject string) *iam.User {
	return &iam.User{
		TenantID:  testTenant,
		Email:     "user-" + subject + "@acme.test",
		Username:  "user-" + subject,
		Name:      "User " + subject,
		Lifecycle: iam.LifecycleActive,
	}
}

// tenantContext binds the tenant a PIV lookup is made in, as login does.
func tenantContext(tenantID string) context.Context {
	return qrdbsdk.ContextWithTenant(context.Background(), tenantID)
}

// pivThumbprintsOf returns the PIV-attested thumbprints on a record.
func pivThumbprintsOf(u *iam.User) []string {
	var out []string
	for _, b := range u.CertBindings {
		if b.Kind == qrdbsdk.CertBindingPIVAttested {
			out = append(out, b.Thumbprint)
		}
	}
	return out
}

func TestUpdateUser_RacingBindingUpdatesLeaveOnlyTheWinnerResolving(t *testing.T) {
	store := newNodeUserStorage(t)
	ctx := tenantContext(testTenant)

	const rounds = 20
	for round := range rounds {
		subject := uniqueSubject()
		a := legacyDigest("A-" + subject)
		b := legacyDigest("B-" + subject)
		c := legacyDigest("C-" + subject)

		u := newNodeUser(subject)
		u.SetPIVThumbprints(a)
		if err := store.CreateUser(ctx, u); err != nil {
			t.Fatalf("round %d: CreateUser: %v", round, err)
		}

		start := make(chan struct{})
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for _, next := range []string{b, c} {
			wg.Add(1)
			go func(thumbprint string) {
				defer wg.Done()
				update, err := store.GetUser(ctx, testTenant, u.ID)
				if err != nil {
					errs <- err
					return
				}
				update.SetPIVThumbprints(thumbprint)
				<-start
				errs <- store.UpdateUser(ctx, update)
			}(next)
		}
		close(start)
		wg.Wait()
		close(errs)
		// qrdb may refuse the slower writer outright (a concurrent-update
		// conflict) or apply both in turn; either way at least one must land.
		failed := 0
		for err := range errs {
			if err != nil {
				failed++
			}
		}
		if failed == 2 {
			t.Fatalf("round %d: both racing updates failed", round)
		}

		committed, err := store.GetUser(ctx, testTenant, u.ID)
		if err != nil {
			t.Fatalf("round %d: GetUser: %v", round, err)
		}
		bound := pivThumbprintsOf(committed)
		if len(bound) != 1 || (bound[0] != b && bound[0] != c) {
			t.Fatalf("round %d: committed bindings = %v, want exactly one of B or C", round, bound)
		}
		winner, loser := bound[0], c
		if winner == c {
			loser = b
		}

		got, err := store.GetUserByPIVThumbprint(ctx, winner)
		if err != nil {
			t.Fatalf("round %d: the winning binding must resolve: %v", round, err)
		}
		if got.ID != u.ID {
			t.Fatalf("round %d: winner resolved %d, want %d", round, got.ID, u.ID)
		}
		for name, tp := range map[string]string{"loser": loser, "A": a} {
			if _, err := store.GetUserByPIVThumbprint(ctx, tp); !errors.Is(err, iam.ErrUserNotFound) {
				t.Fatalf("round %d: the %s binding must resolve nobody, err = %v", round, name, err)
			}
		}
	}
}

func TestGetUserByPIVThumbprint_LegacyDigestResolvesThroughQRDBAndRevokes(t *testing.T) {
	store := newNodeUserStorage(t)
	ctx := tenantContext(testTenant)

	subject := uniqueSubject()
	spki := legacyDigest("spki-" + subject)[:43]
	legacy := legacyDigest("legacy-" + subject)

	u := newNodeUser(subject)
	u.SetPIVThumbprints(spki, legacy)
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	for name, tp := range map[string]string{"spki": spki, "legacy": legacy} {
		got, err := store.GetUserByPIVThumbprint(ctx, tp)
		if err != nil {
			t.Fatalf("the %s thumbprint must resolve through qrdb's index: %v", name, err)
		}
		if got.ID != u.ID {
			t.Fatalf("%s resolved %d, want %d", name, got.ID, u.ID)
		}
	}

	u.SetPIVThumbprints()
	if err := store.UpdateUser(ctx, u); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	for name, tp := range map[string]string{"spki": spki, "legacy": legacy} {
		if _, err := store.GetUserByPIVThumbprint(ctx, tp); !errors.Is(err, iam.ErrUserNotFound) {
			t.Fatalf("the revoked %s thumbprint must resolve nobody, err = %v", name, err)
		}
	}
}

func TestGetUserByPIVThumbprint_IsTenantScoped(t *testing.T) {
	store := newNodeUserStorage(t)

	subject := uniqueSubject()
	legacy := legacyDigest("tenant-" + subject)
	u := newNodeUser(subject)
	u.SetPIVThumbprints(legacy)
	if err := store.CreateUser(tenantContext(testTenant), u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if _, err := store.GetUserByPIVThumbprint(tenantContext("other-"+subject), legacy); !errors.Is(err, iam.ErrUserNotFound) {
		t.Fatalf("a binding in %q must not resolve in another tenant, err = %v", testTenant, err)
	}
}
