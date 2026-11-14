package service

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
)

// bindAfterCheckReader runs a bind the instant DeleteProject's binding check
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

// TestDeleteProject_BindAfterTheCheckSeesTheFence is #1303. A fresh bind that
// commits between the delete's binding check and the delete reads the fence
// after its write (SDLCService.withdrawIfDeleting), so it must find one there.
func TestDeleteProject_BindAfterTheCheckSeesTheFence(t *testing.T) {
	rig := newBoundDeleteRig(t)
	ctx := context.Background()

	var sawFence bool
	var fenceErr error
	reader := &bindAfterCheckReader{Store: rig.bindings}
	reader.afterCheck = func() {
		rig.bind(t)
		sawFence, fenceErr = rig.bindings.ProjectDeleteFenced(ctx, sdlcTestTenant, rig.projectID)
	}
	rig.svc.(*projectServiceImpl).SetBindingReader(reader)

	if err := rig.delete(); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	if fenceErr != nil {
		t.Fatalf("ProjectDeleteFenced: %v", fenceErr)
	}
	if !sawFence {
		t.Fatal("a bind that committed after the delete's binding check found no fence, " +
			"so it would stand on a deleted project")
	}
	// The delete lifts its fence on the way out.
	if fenced, err := rig.bindings.ProjectDeleteFenced(ctx, sdlcTestTenant, rig.projectID); err != nil || fenced {
		t.Fatalf("after the delete: fenced=%v err=%v, want the fence cleared", fenced, err)
	}
}

// A refused delete lifts its fence too, or the project could not be bound for
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

// TestSDLCService_BindBehindADeleteIsWithdrawn is the bind's half: a fresh
// bind that finds a delete's fence after writing its binding withdraws the
// binding and the route, and says why.
func TestSDLCService_BindBehindADeleteIsWithdrawn(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	if _, err := rig.store.FenceProjectDelete(ctx, sdlcTestTenant, sdlcTestProject); err != nil {
		t.Fatal(err)
	}

	_, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding(), BindOptions{})
	var deleting *sdlc.ProjectDeletingError
	if !errors.As(err, &deleting) || !errors.Is(err, sdlc.ErrVersionConflict) {
		t.Fatalf("Bind = %v, want *sdlc.ProjectDeletingError (a version conflict)", err)
	}
	if _, err := rig.store.GetBinding(ctx, sdlcTestTenant, sdlcTestProject); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("binding after the withdrawn bind: %v, want not found", err)
	}
	if _, err := rig.svc.Routes().Tenant(ctx, sdlcTestProject); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("route after the withdrawn bind: %v, want not found", err)
	}
}

// A rebind keeps its binding under a fence: one was held throughout, so the
// delete's check sees it and refuses, and withdrawing would unbind a project
// the delete is about to leave alone.
func TestSDLCService_RebindUnderAFenceKeepsTheBinding(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding(), BindOptions{}); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if _, err := rig.store.FenceProjectDelete(ctx, sdlcTestTenant, sdlcTestProject); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding(), BindOptions{}); err != nil {
		t.Fatalf("rebind under a fence: %v", err)
	}
	if _, err := rig.store.GetBinding(ctx, sdlcTestTenant, sdlcTestProject); err != nil {
		t.Fatalf("binding after the rebind: %v", err)
	}
}
