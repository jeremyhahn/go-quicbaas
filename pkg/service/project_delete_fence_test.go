package service

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
)

// bindAfterCheckReader runs a hook the instant DeleteProject's binding check
// has answered "not bound": the interleaving of go-quicbaas#1303, driven, not
// raced. The embedded store keeps every other method, the fence included.
type bindAfterCheckReader struct {
	*sdlc.Store
	afterCheck func()
}

func (r *bindAfterCheckReader) GetBinding(ctx context.Context, tenantID, projectID string) (*sdlc.ProjectBinding, error) {
	binding, err := r.Store.GetBinding(ctx, tenantID, projectID)
	if errors.Is(err, sdlc.ErrNotFound) && r.afterCheck != nil {
		hook := r.afterCheck
		r.afterCheck = nil
		hook()
	}
	return binding, err
}

// bindDeleteRig wires a project service and an SDLC service over ONE binding
// store, so a delete and a bind meet on the same fence, as on a node.
type bindDeleteRig struct {
	sdlc      *sdlcServiceRig
	projects  ProjectService
	projectID string
}

func newBindDeleteRig(t *testing.T) *bindDeleteRig {
	t.Helper()
	srig := newSDLCServiceRig(t)
	projects, _, _ := newManagedProjectService(t)
	projects.(*projectServiceImpl).SetBindingReader(srig.store)
	resp, err := projects.CreateProject(context.Background(), &CreateProjectRequest{
		TenantID: sdlcTestTenant,
		Name:     "bind-delete",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	// The SDLC service reads projects through its own fake; it holds the
	// same project, so the bind's project read happened before any delete.
	record := sdlcTestProjectRecord()
	record.ID = resp.Project.ID
	srig.projects.projects[record.ID] = record
	return &bindDeleteRig{sdlc: srig, projects: projects, projectID: resp.Project.ID}
}

func (r *bindDeleteRig) binding() *sdlc.ProjectBinding {
	binding := r.sdlc.binding()
	binding.ProjectID = r.projectID
	return binding
}

func (r *bindDeleteRig) bind(ctx context.Context) error {
	_, err := r.sdlc.svc.Bind(ctx, sdlcTestTenant, r.binding(), BindOptions{})
	return err
}

func (r *bindDeleteRig) delete() error {
	_, err := r.projects.DeleteProject(context.Background(), &DeleteProjectRequest{
		TenantID:  sdlcTestTenant,
		ProjectID: r.projectID,
	})
	return err
}

// hookCheck runs hook once the delete's binding check answered "not bound".
func (r *bindDeleteRig) hookCheck(hook func()) {
	r.projects.(*projectServiceImpl).SetBindingReader(&bindAfterCheckReader{Store: r.sdlc.store, afterCheck: hook})
}

// assertUnbound fails unless neither the binding nor its route remains.
func (r *bindDeleteRig) assertUnbound(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if _, err := r.sdlc.store.GetBinding(ctx, sdlcTestTenant, r.projectID); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("binding after the withdrawn bind: %v, want not found", err)
	}
	if _, err := r.sdlc.svc.Routes().Tenant(ctx, r.projectID); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("route after the withdrawn bind: %v, want not found", err)
	}
}

func assertProjectDeleting(t *testing.T, err error) {
	t.Helper()
	var deleting *sdlc.ProjectDeletingError
	if !errors.As(err, &deleting) || !errors.Is(err, sdlc.ErrVersionConflict) {
		t.Fatalf("Bind = %v, want *sdlc.ProjectDeletingError (a version conflict)", err)
	}
}

// TestDeleteProject_BindAfterTheCheckIsWithdrawn is #1303 driven through
// SDLCService.Bind: a bind that commits between the delete's binding check
// and the delete reads the fence after its write, withdraws, and is refused;
// the delete goes through and neither the binding nor the route remains.
func TestDeleteProject_BindAfterTheCheckIsWithdrawn(t *testing.T) {
	rig := newBindDeleteRig(t)
	var bindErr error
	ran := false
	rig.hookCheck(func() {
		ran = true
		bindErr = rig.bind(context.Background())
	})

	if err := rig.delete(); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	if !ran {
		t.Fatal("the hooked bind never ran")
	}
	assertProjectDeleting(t, bindErr)
	rig.assertUnbound(t)
	// The completed delete leaves a tombstone, not a cleared fence.
	if fenced, err := rig.sdlc.store.ProjectDeleteFenced(context.Background(), sdlcTestTenant, rig.projectID); err != nil || !fenced {
		t.Fatalf("after the delete: fenced=%v err=%v, want the tombstone", fenced, err)
	}
}

// sendTick hands one renewal tick to a heartbeat. The heartbeat starts a
// renewal on it, so a returned send does not prove any renewal finished; use
// renewOnce to wait for one.
func sendTick(t *testing.T, ticks chan time.Time) {
	t.Helper()
	select {
	case ticks <- time.Now():
	case <-time.After(5 * time.Second):
		t.Fatalf("no heartbeat took the renewal tick")
	}
}

// renewOnce hands one tick to a heartbeat and waits until the renewal it
// started has finished, so no renewal is running when it returns.
func renewOnce(t *testing.T, ticks chan time.Time, renewed <-chan error) {
	t.Helper()
	sendTick(t, ticks)
	select {
	case err := <-renewed:
		if err != nil {
			t.Fatalf("renewal: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("the heartbeat never finished its renewal")
	}
}

// TestDeleteProject_RenewedFenceRefusesABindAcrossALongTeardown: the hook
// stands in for a teardown that outlasts DeleteFenceTTL. The delete renews
// its fence while it works, so a bind past the original TTL still sees it,
// withdraws and is refused, and the delete completes.
func TestDeleteProject_RenewedFenceRefusesABindAcrossALongTeardown(t *testing.T) {
	rig := newBindDeleteRig(t)
	ticks := make(chan time.Time)
	rig.projects.(*projectServiceImpl).fenceRenewTicks = ticks
	renewed := make(chan error)
	rig.projects.(*projectServiceImpl).fenceRenewed = renewed
	var bindErr error
	rig.hookCheck(func() {
		ageSDLCRecord(t, rig.sdlc, "sdlc_delete_fence", rig.projectID, sdlc.DeleteFenceTTL-10*time.Second)
		renewOnce(t, ticks, renewed)
		// Without the renewal the fence would now be DeleteFenceTTL+1m old.
		ageSDLCRecord(t, rig.sdlc, "sdlc_delete_fence", rig.projectID, 70*time.Second)
		bindErr = rig.bind(context.Background())
	})

	if err := rig.delete(); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	assertProjectDeleting(t, bindErr)
	rig.assertUnbound(t)
	if fenced, err := rig.sdlc.store.ProjectDeleteFenced(context.Background(), sdlcTestTenant, rig.projectID); err != nil || !fenced {
		t.Fatalf("after the delete: fenced=%v err=%v, want the tombstone", fenced, err)
	}
}

// TestDeleteProject_ExpiredFenceStopsTheDeleteBeforeTeardown: the holder
// stalled past DeleteFenceTTL without renewing, so a bind got through. The
// delete finds its fence lost and stops before it destroys anything.
func TestDeleteProject_ExpiredFenceStopsTheDeleteBeforeTeardown(t *testing.T) {
	rig := newBindDeleteRig(t)
	rig.projects.(*projectServiceImpl).fenceRenewTicks = make(chan time.Time)
	var bindErr error
	rig.hookCheck(func() {
		ageSDLCRecord(t, rig.sdlc, "sdlc_delete_fence", rig.projectID, sdlc.DeleteFenceTTL+time.Second)
		bindErr = rig.bind(context.Background())
	})

	err := rig.delete()
	if bindErr != nil {
		t.Fatalf("bind past the stale fence: %v, want nil", bindErr)
	}
	var checkErr *ProjectBindingCheckError
	if !errors.Is(err, sdlc.ErrVersionConflict) || !errors.As(err, &checkErr) {
		t.Fatalf("DeleteProject after losing its fence = %v, want *ProjectBindingCheckError (a version conflict)", err)
	}
	ctx := context.Background()
	if _, err := rig.projects.GetProject(ctx, &GetProjectRequest{TenantID: sdlcTestTenant, ProjectID: rig.projectID}); err != nil {
		t.Fatalf("GetProject after the stopped delete: %v", err)
	}
	if _, err := rig.sdlc.store.GetBinding(ctx, sdlcTestTenant, rig.projectID); err != nil {
		t.Fatalf("binding after the stopped delete: %v", err)
	}
}

// TestBind_AfterACompletedDeleteSeesTheTombstone: the bind read the project
// before the delete, and its fence read lands after the delete finished. The
// tombstone refuses it, where a cleared fence would have let it stand.
func TestBind_AfterACompletedDeleteSeesTheTombstone(t *testing.T) {
	rig := newBindDeleteRig(t)
	if err := rig.delete(); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	assertProjectDeleting(t, rig.bind(context.Background()))
	rig.assertUnbound(t)
}

// failingTombstoneFence is the SDLC store with a tombstone write that always
// fails. Every other method is the store's own, so it is still a
// sdlc.BindingReader and a ProjectDeleteFence.
type failingTombstoneFence struct{ *sdlc.Store }

func (failingTombstoneFence) MarkProjectDeleted(context.Context, string, string, string) error {
	return errors.New("tombstone write lost")
}

// TestDeleteProject_UnsettledTombstoneIsReportedAndALateBindWithdraws: a
// delete that removed the project but could not write its tombstone reports
// it, and once the fence it left goes stale a bind that read the project
// before the delete re-reads it after its write and withdraws.
func TestDeleteProject_UnsettledTombstoneIsReportedAndALateBindWithdraws(t *testing.T) {
	rig := newBindDeleteRig(t)
	rig.projects.(*projectServiceImpl).SetBindingReader(&failingTombstoneFence{Store: rig.sdlc.store})
	ctx := context.Background()

	err := rig.delete()
	var unsettled *ProjectDeleteUnsettledError
	if !errors.As(err, &unsettled) {
		t.Fatalf("DeleteProject with a lost tombstone = %v, want *ProjectDeleteUnsettledError", err)
	}
	var notFound *ProjectNotFoundError
	if _, err := rig.projects.GetProject(ctx, &GetProjectRequest{TenantID: sdlcTestTenant, ProjectID: rig.projectID}); !errors.As(err, &notFound) {
		t.Fatalf("GetProject after the unsettled delete = %v, want *ProjectNotFoundError", err)
	}

	ageSDLCRecord(t, rig.sdlc, "sdlc_delete_fence", rig.projectID, sdlc.DeleteFenceTTL+time.Second)
	if fenced, err := rig.sdlc.store.ProjectDeleteFenced(ctx, sdlcTestTenant, rig.projectID); err != nil || fenced {
		t.Fatalf("after the fence went stale: fenced=%v err=%v, want no live fence", fenced, err)
	}

	// The bind read the project before the delete (the SDLC fake still holds
	// it); it is gone by the bind's re-read after its write.
	hookAfterBindWrite(t, rig.sdlc, func() { delete(rig.sdlc.projects.projects, rig.projectID) })
	assertProjectDeleting(t, rig.bind(ctx))
	rig.assertUnbound(t)
}

// TestDeleteProject_SecondDeleteRefusedWhileFirstInFlight: a second delete
// is refused with *sdlc.DeleteInProgressError while the first's fence is
// live, and the first completes.
func TestDeleteProject_SecondDeleteRefusedWhileFirstInFlight(t *testing.T) {
	rig := newBindDeleteRig(t)
	var secondErr error
	rig.hookCheck(func() { secondErr = rig.delete() })

	if err := rig.delete(); err != nil {
		t.Fatalf("first DeleteProject: %v", err)
	}
	var inProgress *sdlc.DeleteInProgressError
	if !errors.As(secondErr, &inProgress) || !errors.Is(secondErr, sdlc.ErrVersionConflict) {
		t.Fatalf("second DeleteProject = %v, want *sdlc.DeleteInProgressError", secondErr)
	}
}

// A refused delete lifts its fence, or the project could not be bound for
// DeleteFenceTTL.
func TestDeleteProject_RefusedDeleteClearsItsFence(t *testing.T) {
	rig := newBoundDeleteRig(t)
	rig.bind(t)

	var bound *ProjectBoundError
	if err := rig.delete(); !errors.As(err, &bound) {
		t.Fatalf("DeleteProject = %v, want *ProjectBoundError", err)
	}
	fenced, err := rig.bindings.ProjectDeleteFenced(context.Background(), sdlcTestTenant, rig.projectID)
	if err != nil || fenced {
		t.Fatalf("after the refusal: fenced=%v err=%v, want the fence cleared", fenced, err)
	}
	rig.assertIntact(t)
}

// TestSDLCService_BindBehindADeleteIsWithdrawn: a fresh bind that finds a
// delete's fence after writing withdraws the binding and the route.
func TestSDLCService_BindBehindADeleteIsWithdrawn(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	if _, err := rig.store.FenceProjectDelete(ctx, sdlcTestTenant, sdlcTestProject); err != nil {
		t.Fatal(err)
	}

	_, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding(), BindOptions{})
	assertProjectDeleting(t, err)
	if _, err := rig.store.GetBinding(ctx, sdlcTestTenant, sdlcTestProject); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("binding after the withdrawn bind: %v, want not found", err)
	}
	if _, err := rig.svc.Routes().Tenant(ctx, sdlcTestProject); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("route after the withdrawn bind: %v, want not found", err)
	}
}

// TestSDLCService_RebindUnderAFenceRestoresThePrior: a rebind is fenced too.
// It withdraws by restoring the record it replaced, and keeps the route the
// prior binding uses.
func TestSDLCService_RebindUnderAFenceRestoresThePrior(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding(), BindOptions{}); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	original, err := rig.store.GetBinding(ctx, sdlcTestTenant, sdlcTestProject)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rig.store.FenceProjectDelete(ctx, sdlcTestTenant, sdlcTestProject); err != nil {
		t.Fatal(err)
	}
	rebind := rig.binding()
	rebind.MaxReviewRounds = original.MaxReviewRounds + 2
	_, err = rig.svc.Bind(ctx, sdlcTestTenant, rebind, BindOptions{})
	assertProjectDeleting(t, err)

	got, err := rig.store.GetBinding(ctx, sdlcTestTenant, sdlcTestProject)
	if err != nil {
		t.Fatalf("binding after the withdrawn rebind: %v", err)
	}
	if got.WriteToken != original.WriteToken || got.MaxReviewRounds != original.MaxReviewRounds {
		t.Fatalf("binding after the withdrawn rebind = %+v, want the prior record back", got)
	}
	if _, err := rig.svc.Routes().Tenant(ctx, sdlcTestProject); err != nil {
		t.Fatalf("the withdrawn rebind dropped the prior binding's route: %v", err)
	}
}

// cancelAfterBindWriteKV cancels the request the moment a binding write
// lands, and then answers reads under a canceled context with its error, as
// a real backend does.
type cancelAfterBindWriteKV struct {
	qrdbsdk.AtomicKVStore
	cancel context.CancelFunc
}

func (k *cancelAfterBindWriteKV) wrote(key string, applied bool) {
	if applied && strings.HasPrefix(key, "sdlc_binding/") && k.cancel != nil {
		k.cancel()
		k.cancel = nil
	}
}

func (k *cancelAfterBindWriteKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	applied, err := k.AtomicKVStore.PutIfAbsent(ctx, key, value)
	k.wrote(key, applied)
	return applied, err
}

func (k *cancelAfterBindWriteKV) CompareAndSwap(ctx context.Context, key string, old, value []byte) (bool, error) {
	applied, err := k.AtomicKVStore.CompareAndSwap(ctx, key, old, value)
	k.wrote(key, applied)
	return applied, err
}

func (k *cancelAfterBindWriteKV) Get(ctx context.Context, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return k.AtomicKVStore.Get(ctx, key)
}

// TestSDLCService_WithdrawalSurvivesACanceledRequest: the request is
// canceled right after PutBinding, so the fence read fails. The bind cannot
// rule out a delete and withdraws, on a context detached from the canceled
// request, so neither the binding nor the route is left behind.
func TestSDLCService_WithdrawalSurvivesACanceledRequest(t *testing.T) {
	rig := newSDLCServiceRig(t)
	// Resolve once so the tenant store exists to be wrapped.
	if _, err := rig.store.ProjectDeleteFenced(context.Background(), sdlcTestTenant, sdlcTestProject); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	rig.resolver.mu.Lock()
	held, ok := rig.resolver.stores[sdlcTestTenant].(qrdbsdk.AtomicKVStore)
	if !ok {
		rig.resolver.mu.Unlock()
		t.Fatal("the rig's tenant store takes no guarded writes")
	}
	rig.resolver.stores[sdlcTestTenant] = &cancelAfterBindWriteKV{AtomicKVStore: held, cancel: cancel}
	rig.resolver.mu.Unlock()

	_, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding(), BindOptions{})
	if err == nil {
		t.Fatal("Bind succeeded with an unreadable fence")
	}
	var withdrawErr *sdlc.BindWithdrawError
	if errors.As(err, &withdrawErr) {
		t.Fatalf("Bind = %v: the withdrawal ran on the canceled request", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Bind = %v, want the fence read's cancellation", err)
	}
	bg := context.Background()
	if _, err := rig.store.GetBinding(bg, sdlcTestTenant, sdlcTestProject); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("binding after the canceled bind: %v, want not found", err)
	}
	if _, err := rig.svc.Routes().Tenant(bg, sdlcTestProject); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("route after the canceled bind: %v, want not found", err)
	}
}

// lostAckBindWriteKV applies the first binding write and then reports it
// failed, as a backend does when the response of a committed write is lost.
// The fence hook runs between the write and the error, so a delete can fence
// after the bind's pre-check and before its fence read. Other keys and later
// calls pass through.
type lostAckBindWriteKV struct {
	qrdbsdk.AtomicKVStore
	fence func()
	lost  bool
}

func (k *lostAckBindWriteKV) loseAck(key string, applied bool, err error) (bool, error) {
	if err != nil || !applied || k.lost || !strings.HasPrefix(key, "sdlc_binding/") {
		return applied, err
	}
	k.lost = true
	if k.fence != nil {
		k.fence()
	}
	return false, errors.New("proposal outcome unknown")
}

func (k *lostAckBindWriteKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	applied, err := k.AtomicKVStore.PutIfAbsent(ctx, key, value)
	return k.loseAck(key, applied, err)
}

func (k *lostAckBindWriteKV) CompareAndSwap(ctx context.Context, key string, old, value []byte) (bool, error) {
	applied, err := k.AtomicKVStore.CompareAndSwap(ctx, key, old, value)
	return k.loseAck(key, applied, err)
}

// installLostAckBindWrite wraps the rig's tenant store so its next binding
// write lands and reports an error, running fence in between.
func installLostAckBindWrite(t *testing.T, rig *sdlcServiceRig, fence func()) {
	t.Helper()
	// Resolve once so the tenant store exists to be wrapped.
	if _, err := rig.store.ProjectDeleteFenced(context.Background(), sdlcTestTenant, sdlcTestProject); err != nil {
		t.Fatal(err)
	}
	rig.resolver.mu.Lock()
	defer rig.resolver.mu.Unlock()
	held, ok := rig.resolver.stores[sdlcTestTenant].(qrdbsdk.AtomicKVStore)
	if !ok {
		t.Fatal("the rig's tenant store takes no guarded writes")
	}
	rig.resolver.stores[sdlcTestTenant] = &lostAckBindWriteKV{AtomicKVStore: held, fence: fence}
}

func fenceRigProject(t *testing.T, rig *sdlcServiceRig) func() {
	return func() {
		if _, err := rig.store.FenceProjectDelete(context.Background(), sdlcTestTenant, sdlcTestProject); err != nil {
			t.Errorf("FenceProjectDelete: %v", err)
		}
	}
}

// TestSDLCService_BindWithLostWriteAckBehindADeleteIsWithdrawn: the binding
// write lands but reports an error, and a delete fences in between. The bind
// still reads the fence, withdraws its write and forgets its route.
func TestSDLCService_BindWithLostWriteAckBehindADeleteIsWithdrawn(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	installLostAckBindWrite(t, rig, fenceRigProject(t, rig))

	_, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding(), BindOptions{})
	assertProjectDeleting(t, err)
	if _, err := rig.store.GetBinding(ctx, sdlcTestTenant, sdlcTestProject); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("binding after the withdrawn bind: %v, want not found", err)
	}
	if _, err := rig.svc.Routes().Tenant(ctx, sdlcTestProject); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("route after the withdrawn bind: %v, want not found", err)
	}
}

// deferredBindWriteKV does not apply the first binding write: it runs the
// fence hook and reports the outcome unknown, keeping the write in apply, as
// a proposal that timed out but is still in the log does. apply commits it
// later, against whatever the slot holds then.
type deferredBindWriteKV struct {
	qrdbsdk.AtomicKVStore
	fence func()
	apply func() (bool, error)
}

func (k *deferredBindWriteKV) hold(key string, write func() (bool, error)) (bool, error) {
	if k.apply != nil || !strings.HasPrefix(key, "sdlc_binding/") {
		return write()
	}
	k.apply = write
	if k.fence != nil {
		k.fence()
	}
	return false, errors.New("proposal outcome unknown")
}

func (k *deferredBindWriteKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	ctx = context.WithoutCancel(ctx)
	return k.hold(key, func() (bool, error) { return k.AtomicKVStore.PutIfAbsent(ctx, key, value) })
}

func (k *deferredBindWriteKV) CompareAndSwap(ctx context.Context, key string, old, value []byte) (bool, error) {
	ctx = context.WithoutCancel(ctx)
	return k.hold(key, func() (bool, error) { return k.AtomicKVStore.CompareAndSwap(ctx, key, old, value) })
}

// installDeferredBindWrite wraps the rig's tenant store so its next binding
// write is held back and reports an error, running fence in between.
func installDeferredBindWrite(t *testing.T, rig *sdlcServiceRig, fence func()) *deferredBindWriteKV {
	t.Helper()
	// Resolve once so the tenant store exists to be wrapped.
	if _, err := rig.store.ProjectDeleteFenced(context.Background(), sdlcTestTenant, sdlcTestProject); err != nil {
		t.Fatal(err)
	}
	rig.resolver.mu.Lock()
	defer rig.resolver.mu.Unlock()
	held, ok := rig.resolver.stores[sdlcTestTenant].(qrdbsdk.AtomicKVStore)
	if !ok {
		t.Fatal("the rig's tenant store takes no guarded writes")
	}
	deferred := &deferredBindWriteKV{AtomicKVStore: held, fence: fence}
	rig.resolver.stores[sdlcTestTenant] = deferred
	return deferred
}

// TestBind_PendingWriteAppliedAfterDeleteDoesNotRebind: the binding write
// times out without applying and a delete fences in between. The bind's
// withdrawal finds nothing to undo, the delete removes the binding, and only
// then does the pending write apply. It must fail: the withdrawal settled it,
// so the deleted project is left with neither a binding nor a route.
func TestBind_PendingWriteAppliedAfterDeleteDoesNotRebind(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	deferred := installDeferredBindWrite(t, rig, fenceRigProject(t, rig))

	_, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding(), BindOptions{})
	assertProjectDeleting(t, err)
	if deferred.apply == nil {
		t.Fatal("the bind's write was never held back")
	}
	// The delete's teardown removes the project's binding.
	if err := rig.store.DeleteBinding(ctx, sdlcTestTenant, sdlcTestProject); err != nil {
		t.Fatalf("DeleteBinding: %v", err)
	}
	if applied, err := deferred.apply(); err != nil || applied {
		t.Fatalf("pending bind write applied after the delete: %v, %v; want false, nil", applied, err)
	}
	if _, err := rig.store.GetBinding(ctx, sdlcTestTenant, sdlcTestProject); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("binding after the pending write: %v, want not found", err)
	}
	if _, err := rig.svc.Routes().Tenant(ctx, sdlcTestProject); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("route after the pending write: %v, want not found", err)
	}
}

// TestSDLCService_RebindWithLostWriteAckBehindADeleteRestoresThePrior: a
// rebind whose write lands but reports an error behind a delete restores
// the record it replaced, and keeps that record's route.
func TestSDLCService_RebindWithLostWriteAckBehindADeleteRestoresThePrior(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding(), BindOptions{}); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	original, err := rig.store.GetBinding(ctx, sdlcTestTenant, sdlcTestProject)
	if err != nil {
		t.Fatal(err)
	}
	installLostAckBindWrite(t, rig, fenceRigProject(t, rig))

	rebind := rig.binding()
	rebind.MaxReviewRounds = original.MaxReviewRounds + 2
	_, err = rig.svc.Bind(ctx, sdlcTestTenant, rebind, BindOptions{})
	assertProjectDeleting(t, err)

	got, err := rig.store.GetBinding(ctx, sdlcTestTenant, sdlcTestProject)
	if err != nil {
		t.Fatalf("binding after the withdrawn rebind: %v", err)
	}
	if got.WriteToken != original.WriteToken || got.MaxReviewRounds != original.MaxReviewRounds {
		t.Fatalf("binding after the withdrawn rebind = %+v, want the prior record back", got)
	}
	if _, err := rig.svc.Routes().Tenant(ctx, sdlcTestProject); err != nil {
		t.Fatalf("the withdrawn rebind dropped the prior binding's route: %v", err)
	}
}

// TestSDLCService_BindWithLostWriteAckAndNoDeleteReturnsTheWriteError: with
// no delete in flight, the fence read finds nothing to withdraw for and the
// bind reports the write's own error.
func TestSDLCService_BindWithLostWriteAckAndNoDeleteReturnsTheWriteError(t *testing.T) {
	rig := newSDLCServiceRig(t)
	installLostAckBindWrite(t, rig, func() {})

	_, err := rig.svc.Bind(context.Background(), sdlcTestTenant, rig.binding(), BindOptions{})
	if err == nil {
		t.Fatal("Bind succeeded on a write that reported an error")
	}
	var deleting *sdlc.ProjectDeletingError
	var withdrawErr *sdlc.BindWithdrawError
	if errors.As(err, &deleting) || errors.As(err, &withdrawErr) {
		t.Fatalf("Bind = %v, want the write's own error", err)
	}
	if !strings.Contains(err.Error(), "proposal outcome unknown") {
		t.Fatalf("Bind = %v, want the write's own error", err)
	}
}

// failingDeleteStore is a project store whose Delete fails: after removing
// the record (removeFirst, a failed name release) or without touching it.
// With failGet, every Get after the Delete fails too.
type failingDeleteStore struct {
	orchestration.ProjectStore
	removeFirst bool
	failGet     bool
	deleted     atomic.Bool
}

func (s *failingDeleteStore) Delete(ctx context.Context, tenantID, projectID string) error {
	s.deleted.Store(true)
	if s.removeFirst {
		if err := s.ProjectStore.Delete(ctx, tenantID, projectID); err != nil {
			return err
		}
	}
	return errors.New("project store: name release failed")
}

func (s *failingDeleteStore) Get(ctx context.Context, tenantID, projectID string) (*orchestration.Project, error) {
	if s.failGet && s.deleted.Load() {
		return nil, errors.New("project store: unavailable")
	}
	return s.ProjectStore.Get(ctx, tenantID, projectID)
}

// deleteThroughFailingStore creates a project on store, deletes it through a
// project service fenced by an SDLC store, and returns that SDLC store and
// the project id. The delete must fail.
func deleteThroughFailingStore(t *testing.T, store *failingDeleteStore) (*sdlc.Store, string) {
	t.Helper()
	ctx := context.Background()
	bindings := newSDLCServiceRig(t).store
	svc := NewProjectService(store)
	svc.(*projectServiceImpl).SetBindingReader(bindings)
	resp, err := svc.CreateProject(ctx, &CreateProjectRequest{TenantID: sdlcTestTenant, Name: "failing-delete"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	projectID := resp.Project.ID
	if _, err := svc.DeleteProject(ctx, &DeleteProjectRequest{TenantID: sdlcTestTenant, ProjectID: projectID}); err == nil {
		t.Fatal("DeleteProject succeeded through a failing store")
	}
	return bindings, projectID
}

// TestDeleteProject_DeleteThatRemovedThenFailedLeavesATombstone: the store
// removed the record and then failed. The project is gone, so the fence
// becomes its tombstone, not cleared for a late bind to land on.
func TestDeleteProject_DeleteThatRemovedThenFailedLeavesATombstone(t *testing.T) {
	bindings, projectID := deleteThroughFailingStore(t, &failingDeleteStore{
		ProjectStore: orchestration.NewInMemoryProjectStore(), removeFirst: true,
	})
	ctx := context.Background()
	if fenced, err := bindings.ProjectDeleteFenced(ctx, sdlcTestTenant, projectID); err != nil || !fenced {
		t.Fatalf("after the failed delete: fenced=%v err=%v, want a tombstone", fenced, err)
	}
	if _, err := bindings.FenceProjectDelete(ctx, sdlcTestTenant, projectID); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("FenceProjectDelete after the failed delete = %v, want the tombstone's not found", err)
	}
}

// TestDeleteProject_FailedDeleteWithTheProjectInPlaceClearsTheFence: the
// delete failed and the project is still there, so the fence is cleared.
func TestDeleteProject_FailedDeleteWithTheProjectInPlaceClearsTheFence(t *testing.T) {
	bindings, projectID := deleteThroughFailingStore(t, &failingDeleteStore{
		ProjectStore: orchestration.NewInMemoryProjectStore(),
	})
	if fenced, err := bindings.ProjectDeleteFenced(context.Background(), sdlcTestTenant, projectID); err != nil || fenced {
		t.Fatalf("after the failed delete: fenced=%v err=%v, want the fence cleared", fenced, err)
	}
}

// TestDeleteProject_FailedDeleteAndFailedReReadLeaveTheFenceHeld: neither
// the delete nor the re-read tells whether the project is gone, so the fence
// is left held: neither cleared nor a tombstone.
func TestDeleteProject_FailedDeleteAndFailedReReadLeaveTheFenceHeld(t *testing.T) {
	bindings, projectID := deleteThroughFailingStore(t, &failingDeleteStore{
		ProjectStore: orchestration.NewInMemoryProjectStore(), failGet: true,
	})
	ctx := context.Background()
	if fenced, err := bindings.ProjectDeleteFenced(ctx, sdlcTestTenant, projectID); err != nil || !fenced {
		t.Fatalf("after the failed delete: fenced=%v err=%v, want the fence held", fenced, err)
	}
	var inProgress *sdlc.DeleteInProgressError
	if _, err := bindings.FenceProjectDelete(ctx, sdlcTestTenant, projectID); !errors.As(err, &inProgress) {
		t.Fatalf("FenceProjectDelete after the failed delete = %v, want the live fence, not a tombstone", err)
	}
}
