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

package sdlc

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// errReservationStore is the fault the failing store reports.
var errReservationStore = errors.New("store unavailable")

// failingReservationKV is a store whose guarded create always fails.
type failingReservationKV struct {
	interfaces.KVStore
}

// PutIfAbsent implements the guarded create and always fails.
func (failingReservationKV) PutIfAbsent(context.Context, string, []byte) (bool, error) {
	return false, errReservationStore
}

// CompareAndDelete implements the guarded delete and always fails.
func (failingReservationKV) CompareAndDelete(context.Context, string, []byte) (bool, error) {
	return false, errReservationStore
}

// plainReservationKV offers none of the guarded verbs.
type plainReservationKV struct {
	interfaces.KVStore
}

// reservingReconciler builds a second sweep over the rig's forge, hooks and
// task records, reserving through reserver.
func reservingReconciler(t *testing.T, rig *reconcileRig, reserver OutcomeReserver, logger *slog.Logger) *LabelReconciler {
	t.Helper()
	reconciler, err := NewLabelReconciler(LabelReconcilerConfig{
		Tenants: rig.tenants, Bindings: rig.store, Tasks: rig.tasks, Hooks: rig.reconciler.hooks,
		Pacer: &countingPacer{}, Reservations: reserver, Logger: logger,
	})
	if err != nil {
		t.Fatal(err)
	}
	return reconciler
}

func newReserver(t *testing.T, kv interfaces.KVStore, holder string, now func() time.Time) OutcomeReserver {
	t.Helper()
	reserver, err := NewOutcomeReserver(kv, holder, now)
	if err != nil {
		t.Fatal(err)
	}
	return reserver
}

// TestLabelReconciler_TwoNodesRacingPostOnce releases two sweeps that share one
// store through a channel at the same instant: both read the discussion with
// no marker in it, and the reservation lets exactly one of them post.
func TestLabelReconciler_TwoNodesRacingPostOnce(t *testing.T) {
	rig := newReconcileRig(t, []string{"factory:in-progress"},
		parkedTask(orchestration.TaskBlockKindBlocked, "the account pool is empty"))
	kv := orchestration.NewMemoryTaskKV()
	nodes := []*LabelReconciler{
		reservingReconciler(t, rig, newReserver(t, kv, "node-a", nil), nil),
		reservingReconciler(t, rig, newReserver(t, kv, "node-b", nil), nil),
	}

	release := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, len(nodes))
	for i, node := range nodes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-release
			_, errs[i] = node.ReconcileProject(context.Background(), testTenant, testProject)
		}()
	}
	close(release)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("node %d: %v", i, err)
		}
	}
	if got := len(rig.outcomeComments()); got != 1 {
		t.Fatalf("want exactly one outcome comment from two racing nodes, got %d", got)
	}
}

// TestLabelReconciler_ReservationStoreFaultSkipsThePostAndIsLogged proves the
// reservation fails closed: nothing is posted, the finding is deferred with the
// store's error, and the fault is logged.
func TestLabelReconciler_ReservationStoreFaultSkipsThePostAndIsLogged(t *testing.T) {
	rig := newReconcileRig(t, []string{"factory:in-progress"},
		parkedTask(orchestration.TaskBlockKindBlocked, "the account pool is empty"))
	var logs bytes.Buffer
	node := reservingReconciler(t, rig,
		newReserver(t, failingReservationKV{KVStore: orchestration.NewMemoryTaskKV()}, "node-a", nil),
		slog.New(slog.NewTextHandler(&logs, nil)))

	report, err := node.ReconcileProject(context.Background(), testTenant, testProject)
	if err != nil {
		t.Fatal(err)
	}
	finding := only(t, report)
	if finding.Verdict != LabelReconcileDeferred {
		t.Fatalf("verdict = %q (%s), want deferred", finding.Verdict, finding.Evidence)
	}
	if !strings.Contains(finding.Evidence, errReservationStore.Error()) {
		t.Fatalf("the finding does not carry the store's fault: %s", finding.Evidence)
	}
	if got := len(rig.outcomeComments()); got != 0 {
		t.Fatalf("a failed reservation must not post, got %d comments", got)
	}
	if !strings.Contains(logs.String(), "outcome reservation failed") {
		t.Fatalf("the fault was not logged: %q", logs.String())
	}
}

// TestLabelReconciler_ReservationHeldElsewhereSkipsAndIsLogged proves a node
// that loses the reservation posts nothing.
func TestLabelReconciler_ReservationHeldElsewhereSkipsAndIsLogged(t *testing.T) {
	rig := newReconcileRig(t, []string{"factory:in-progress"},
		parkedTask(orchestration.TaskBlockKindBlocked, "the account pool is empty"))
	kv := orchestration.NewMemoryTaskKV()
	marker := blockedMarker(testTask, 2, OutcomeBlocked)
	holder := newReserver(t, kv, "node-a", nil)
	if _, err := holder.Reserve(context.Background(), testTenant, testProject, reconcileIssue, marker, time.Hour); err != nil {
		t.Fatalf("setup reservation: %v", err)
	}
	var logs bytes.Buffer
	node := reservingReconciler(t, rig, newReserver(t, kv, "node-b", nil), slog.New(slog.NewTextHandler(&logs, nil)))

	finding := only(t, mustReconcile(t, node))
	if finding.Verdict != LabelReconcileDeferred {
		t.Fatalf("verdict = %q (%s), want deferred", finding.Verdict, finding.Evidence)
	}
	if got := len(rig.outcomeComments()); got != 0 {
		t.Fatalf("a node that lost the reservation must not post, got %d comments", got)
	}
	if !strings.Contains(logs.String(), "reserved by another node") {
		t.Fatalf("the skip was not logged: %q", logs.String())
	}
}

func mustReconcile(t *testing.T, node *LabelReconciler) LabelReconcileReport {
	t.Helper()
	report, err := node.ReconcileProject(context.Background(), testTenant, testProject)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

// reserveFor takes the reservation for the shared fixture issue and returns the
// lease, or nil when another node holds it.
func reserveFor(t *testing.T, r OutcomeReserver, tenant string, ttl time.Duration) OutcomeLease {
	t.Helper()
	lease, err := r.Reserve(context.Background(), tenant, testProject, reconcileIssue, "marker", ttl)
	var elsewhere *OutcomeReservedElsewhereError
	if errors.As(err, &elsewhere) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return lease
}

// routingReservationKV stands in for the server's tenant-routing store: it picks
// the physical store from the tenant stamped on the context, and the system
// store when none is.
type routingReservationKV struct {
	system  interfaces.KVStore
	tenants map[string]interfaces.KVStore
}

func (r *routingReservationKV) route(ctx context.Context) interfaces.KVStore {
	if store, ok := r.tenants[orchestration.TenantFromContext(ctx)]; ok {
		return store
	}
	return r.system
}

func (r *routingReservationKV) Put(ctx context.Context, k string, v []byte) error {
	return r.route(ctx).Put(ctx, k, v)
}

func (r *routingReservationKV) Get(ctx context.Context, k string) ([]byte, error) {
	return r.route(ctx).Get(ctx, k)
}

func (r *routingReservationKV) Delete(ctx context.Context, k string) error {
	return r.route(ctx).Delete(ctx, k)
}

func (r *routingReservationKV) Scan(ctx context.Context, p string, fn func(string, []byte) error) error {
	return r.route(ctx).Scan(ctx, p, fn)
}

func (r *routingReservationKV) List(ctx context.Context, p string) ([]string, error) {
	return r.route(ctx).List(ctx, p)
}

func (r *routingReservationKV) Exists(ctx context.Context, k string) (bool, error) {
	return r.route(ctx).Exists(ctx, k)
}

func (r *routingReservationKV) PutIfAbsent(ctx context.Context, k string, v []byte) (bool, error) {
	return r.route(ctx).(outcomeReservationGuard).PutIfAbsent(ctx, k, v)
}

func (r *routingReservationKV) CompareAndDelete(ctx context.Context, k string, v []byte) (bool, error) {
	return r.route(ctx).(outcomeReservationGuard).CompareAndDelete(ctx, k, v)
}

// TestOutcomeReserver_ReservationIsWrittenToTheTenantsOwnStore proves every
// operation — create, read, renew — is scoped to the reservation's tenant, so a
// barrier-isolated tenant's reservation never lands in the shared system store.
func TestOutcomeReserver_ReservationIsWrittenToTheTenantsOwnStore(t *testing.T) {
	ctx := context.Background()
	routing := &routingReservationKV{
		system:  orchestration.NewMemoryTaskKV(),
		tenants: map[string]interfaces.KVStore{"tenant-a": orchestration.NewMemoryTaskKV()},
	}
	reserver := newReserver(t, routing, "node-a", nil)

	lease := reserveFor(t, reserver, "tenant-a", time.Minute)
	if lease == nil {
		t.Fatal("a free reservation was not won")
	}
	if err := lease.Hold(ctx); err != nil {
		t.Fatal(err)
	}

	tenantKeys, err := routing.tenants["tenant-a"].List(ctx, outcomeReservationPrefix)
	if err != nil {
		t.Fatal(err)
	}
	if len(tenantKeys) != 2 {
		t.Fatalf("tenant store holds %v, want the reservation and its renewal", tenantKeys)
	}
	systemKeys, err := routing.system.List(ctx, outcomeReservationPrefix)
	if err != nil {
		t.Fatal(err)
	}
	if len(systemKeys) != 0 {
		t.Fatalf("the system store holds %v, want nothing of tenant-a's", systemKeys)
	}
}

// displacingForge displaces the winner's reservation during its first post
// attempt, which then fails, so the retry loop reaches its second attempt with
// another node holding the reservation.
type displacingForge struct {
	*fakeForge
	displace func()
	calls    int
}

func (f *displacingForge) CommentOnIssue(ctx context.Context, repo RepoRef, number int, body string) (*ForgeComment, error) {
	f.calls++
	if f.calls == 1 {
		f.displace()
		return nil, errReservationStore
	}
	return f.fakeForge.CommentOnIssue(ctx, repo, number, body)
}

// TestPostWithRetry_DisplacedWinnerDoesNotPost proves the fence: a winner whose
// reservation another node took over between two attempts stops instead of
// posting beside it.
func TestPostWithRetry_DisplacedWinnerDoesNotPost(t *testing.T) {
	kv := orchestration.NewMemoryTaskKV()
	reserver := newReserver(t, kv, "node-a", nil)
	lease := reserveFor(t, reserver, testTenant, time.Minute).(*kvOutcomeLease)

	forge := &displacingForge{fakeForge: newFakeForge()}
	forge.displace = func() {
		ctx := orchestration.ContextWithTenant(context.Background(), testTenant)
		guard := kv.(outcomeReservationGuard)
		if deleted, err := guard.CompareAndDelete(ctx, lease.key, lease.value); err != nil || !deleted {
			t.Errorf("displacement delete: deleted=%v err=%v", deleted, err)
		}
		if won, err := guard.PutIfAbsent(ctx, lease.key, []byte(`{"holder":"node-b"}`)); err != nil || !won {
			t.Errorf("displacement create: won=%v err=%v", won, err)
		}
	}
	policy := SurfacePolicy{RetryAttempts: 3, RetryInterval: time.Nanosecond, PostTimeout: time.Second}

	_, err := postWithRetry(withOutcomeLease(context.Background(), lease), forge, testRepoRef, reconcileIssue, "body", policy)
	var lost *OutcomeReservationLostError
	if !errors.As(err, &lost) {
		t.Fatalf("err = %v, want OutcomeReservationLostError", err)
	}
	if forge.calls != 1 {
		t.Fatalf("the displaced winner made %d post attempts, want only the first", forge.calls)
	}
	if got := len(forge.commentsOn(reconcileIssue)); got != 0 {
		t.Fatalf("the displaced winner posted %d comments", got)
	}
}

// TestOutcomeLease_RenewalExtendsTheReservation proves Hold renews: a
// reservation that would have lapsed is still held by its owner afterwards.
func TestOutcomeLease_RenewalExtendsTheReservation(t *testing.T) {
	kv := orchestration.NewMemoryTaskKV()
	clock := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return clock }
	owner := newReserver(t, kv, "node-a", now)
	other := newReserver(t, kv, "node-b", now)

	lease := reserveFor(t, owner, testTenant, time.Minute)
	clock = clock.Add(50 * time.Second)
	if err := lease.Hold(context.Background()); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(50 * time.Second) // past the original expiry, inside the renewal
	if reserveFor(t, other, testTenant, time.Minute) != nil {
		t.Fatal("a renewed reservation was taken over at its original expiry")
	}
	clock = clock.Add(2 * time.Minute)
	if reserveFor(t, other, testTenant, time.Minute) == nil {
		t.Fatal("the renewal must still lapse")
	}
}

// TestOutcomeReservationTTL_CoversTheSurfacingBudget proves the TTL follows the
// policy rather than a constant.
func TestOutcomeReservationTTL_CoversTheSurfacingBudget(t *testing.T) {
	policy := SurfacePolicy{RetryAttempts: 2, RetryInterval: 20 * time.Minute, PostTimeout: time.Minute}
	if got, want := OutcomeReservationTTL(policy), 42*time.Minute+OutcomeReservationMargin; got != want {
		t.Fatalf("ttl = %v, want %v", got, want)
	}
	if OutcomeReservationTTL(SurfacePolicy{}) <= OutcomeReservationMargin {
		t.Fatal("the zero policy must resolve to the default budget plus the margin")
	}
}

// TestOutcomeReserver_RefusesANonPositiveTTL covers the invalid input.
func TestOutcomeReserver_RefusesANonPositiveTTL(t *testing.T) {
	reserver := newReserver(t, orchestration.NewMemoryTaskKV(), "node-a", nil)
	_, err := reserver.Reserve(context.Background(), testTenant, testProject, reconcileIssue, "marker", 0)
	var invalid *InvalidInputError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want InvalidInputError", err)
	}
}

// TestOutcomeReserver_ExpiredHolderIsDisplaced proves the TTL: a live holder
// blocks the others, a lapsed one is displaced by exactly one of them.
func TestOutcomeReserver_ExpiredHolderIsDisplaced(t *testing.T) {
	kv := orchestration.NewMemoryTaskKV()
	clock := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return clock }
	first := newReserver(t, kv, "node-a", now)
	second := newReserver(t, kv, "node-b", now)
	third := newReserver(t, kv, "node-c", now)
	reserve := func(r OutcomeReserver) bool {
		t.Helper()
		return reserveFor(t, r, testTenant, time.Minute) != nil
	}

	if !reserve(first) {
		t.Fatal("the first node should win a free reservation")
	}
	if reserve(second) {
		t.Fatal("a live reservation must block the second node")
	}
	clock = clock.Add(2 * time.Minute)
	if !reserve(second) {
		t.Fatal("the lapsed reservation should be taken over")
	}
	if reserve(third) {
		t.Fatal("the takeover is itself a live reservation")
	}
}

// TestNewOutcomeReserver_RefusesWhatCannotArbitrate covers the invalid inputs.
func TestNewOutcomeReserver_RefusesWhatCannotArbitrate(t *testing.T) {
	kv := orchestration.NewMemoryTaskKV()
	var invalid *InvalidInputError
	for name, build := range map[string]func() error{
		"nil store": func() error { _, err := NewOutcomeReserver(nil, "n", nil); return err },
		"no holder": func() error { _, err := NewOutcomeReserver(kv, "", nil); return err },
	} {
		if err := build(); !errors.As(err, &invalid) {
			t.Errorf("%s: err = %v, want InvalidInputError", name, err)
		}
	}

	_, err := NewOutcomeReserver(plainReservationKV{KVStore: kv}, "n", nil)
	var unguarded *OutcomeReservationUnguardedError
	if !errors.As(err, &unguarded) {
		t.Fatalf("a store with no guarded verbs: err = %v, want OutcomeReservationUnguardedError", err)
	}
}
