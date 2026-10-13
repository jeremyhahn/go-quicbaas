// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
)

// boundDeleteRig is a managed-workspace project service whose deletes are
// checked against a real binding store over in-memory tenant keyspaces.
type boundDeleteRig struct {
	svc        ProjectService
	bindings   *sdlc.Store
	projectID  string
	projectDir string
}

func newBoundDeleteRig(t *testing.T) *boundDeleteRig {
	t.Helper()
	svc, _, root := newManagedProjectService(t)
	bindings, err := sdlc.NewStore(&sdlcMemoryResolver{})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	svc.(*projectServiceImpl).SetBindingReader(bindings)

	resp, err := svc.CreateProject(context.Background(), &CreateProjectRequest{
		TenantID:         sdlcTestTenant,
		Name:             "bound-delete",
		WorkspaceManaged: true,
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	return &boundDeleteRig{
		svc:        svc,
		bindings:   bindings,
		projectID:  resp.Project.ID,
		projectDir: filepath.Join(root, sdlcTestTenant, resp.Project.ID),
	}
}

func (r *boundDeleteRig) bind(t *testing.T) {
	t.Helper()
	binding := sdlcTestBinding()
	binding.ProjectID = r.projectID
	if err := r.bindings.PutBinding(context.Background(), sdlcTestTenant, binding); err != nil {
		t.Fatalf("PutBinding: %v", err)
	}
}

func (r *boundDeleteRig) delete() error {
	_, err := r.svc.DeleteProject(context.Background(), &DeleteProjectRequest{
		TenantID:  sdlcTestTenant,
		ProjectID: r.projectID,
	})
	return err
}

// assertIntact fails unless the project record still reads back and its
// managed workspace was not torn down.
func (r *boundDeleteRig) assertIntact(t *testing.T) {
	t.Helper()
	if _, err := r.svc.GetProject(context.Background(), &GetProjectRequest{
		TenantID:  sdlcTestTenant,
		ProjectID: r.projectID,
	}); err != nil {
		t.Fatalf("a refused delete removed the project record: %v", err)
	}
	if _, err := os.Stat(r.projectDir); err != nil {
		t.Fatalf("a refused delete tore down the managed workspace: %v", err)
	}
}

func (r *boundDeleteRig) assertDeleted(t *testing.T) {
	t.Helper()
	_, err := r.svc.GetProject(context.Background(), &GetProjectRequest{
		TenantID:  sdlcTestTenant,
		ProjectID: r.projectID,
	})
	var notFound *ProjectNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("GetProject after delete = %v, want *ProjectNotFoundError", err)
	}
	if _, err := os.Stat(r.projectDir); !os.IsNotExist(err) {
		t.Fatalf("managed workspace survived the delete (stat err: %v)", err)
	}
}

// TestDeleteProject_BoundProjectIsRefused is go-quicbaas#891: a bound project
// is not deleted out from under its binding, and the refusal names unbind.
func TestDeleteProject_BoundProjectIsRefused(t *testing.T) {
	rig := newBoundDeleteRig(t)
	rig.bind(t)

	err := rig.delete()
	var bound *ProjectBoundError
	if !errors.As(err, &bound) {
		t.Fatalf("DeleteProject = %v, want *ProjectBoundError", err)
	}
	if bound.TenantID != sdlcTestTenant || bound.ProjectID != rig.projectID {
		t.Fatalf("ProjectBoundError = %+v, want tenant %q project %q", bound, sdlcTestTenant, rig.projectID)
	}
	if !strings.HasPrefix(err.Error(), "run unbind") {
		t.Fatalf("error %q does not lead with the unbind remedy", err)
	}
	rig.assertIntact(t)
}

// TestDeleteProject_UnbindThenDeleteSucceeds proves the refusal clears once
// the binding is gone.
func TestDeleteProject_UnbindThenDeleteSucceeds(t *testing.T) {
	rig := newBoundDeleteRig(t)
	rig.bind(t)
	var bound *ProjectBoundError
	if err := rig.delete(); !errors.As(err, &bound) {
		t.Fatalf("DeleteProject while bound = %v, want *ProjectBoundError", err)
	}

	if err := rig.bindings.DeleteBinding(context.Background(), sdlcTestTenant, rig.projectID); err != nil {
		t.Fatalf("DeleteBinding: %v", err)
	}
	if err := rig.delete(); err != nil {
		t.Fatalf("DeleteProject after unbind: %v", err)
	}
	rig.assertDeleted(t)
}

// TestDeleteProject_UnboundProjectIsDeleted proves a project with no binding
// deletes as before, managed teardown included.
func TestDeleteProject_UnboundProjectIsDeleted(t *testing.T) {
	rig := newBoundDeleteRig(t)
	if err := rig.delete(); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	rig.assertDeleted(t)
}

// failingBindingReader is a binding store that cannot be read.
type failingBindingReader struct{ err error }

func (f failingBindingReader) GetBinding(context.Context, string, string) (*sdlc.ProjectBinding, error) {
	return nil, f.err
}

func (f failingBindingReader) ListBindings(context.Context, string) ([]*sdlc.ProjectBinding, error) {
	return nil, f.err
}

// TestDeleteProject_UnreadableBindingStoreRefuses proves a binding store that
// cannot answer refuses the delete: it does not prove the project unbound.
func TestDeleteProject_UnreadableBindingStoreRefuses(t *testing.T) {
	rig := newBoundDeleteRig(t)
	cause := &sdlc.BackendError{Op: "GetBinding", Cause: errors.New("tenant barrier sealed")}
	rig.svc.(*projectServiceImpl).SetBindingReader(failingBindingReader{err: cause})

	err := rig.delete()
	var checkErr *ProjectBindingCheckError
	if !errors.As(err, &checkErr) {
		t.Fatalf("DeleteProject = %v, want *ProjectBindingCheckError", err)
	}
	if !errors.Is(err, sdlc.ErrBackend) {
		t.Fatalf("ProjectBindingCheckError %v does not wrap the store's error", err)
	}
	var bound *ProjectBoundError
	if errors.As(err, &bound) {
		t.Fatal("an unreadable store was reported as a binding")
	}
	rig.assertIntact(t)
}

// TestDeleteProject_UnwiredBindingReaderProceeds documents the unwired case:
// a node with no tenant store resolver has no binding store, so no binding
// can exist and the delete proceeds.
func TestDeleteProject_UnwiredBindingReaderProceeds(t *testing.T) {
	svc := NewProjectService(orchestration.NewInMemoryProjectStore())
	resp, err := svc.CreateProject(context.Background(), &CreateProjectRequest{
		TenantID: sdlcTestTenant,
		Name:     "unwired",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if _, err := svc.DeleteProject(context.Background(), &DeleteProjectRequest{
		TenantID:  sdlcTestTenant,
		ProjectID: resp.Project.ID,
	}); err != nil {
		t.Fatalf("DeleteProject with no binding reader: %v", err)
	}
}

// TestQuicBaaS_SetProjectBindingReaderReachesProjectService proves the facade
// method binds the reader onto the project service it serves.
func TestQuicBaaS_SetProjectBindingReaderReachesProjectService(t *testing.T) {
	projects := NewProjectService(orchestration.NewInMemoryProjectStore())
	facade := &quicBaaS{projectService: projects}
	reader := failingBindingReader{err: errors.New("unused")}

	facade.SetProjectBindingReader(reader)

	if got := projects.(*projectServiceImpl).bindings; got != reader {
		t.Fatalf("project service binding reader = %v, want the reader the facade was given", got)
	}
}

// TestQuicBaaS_SetProjectBindingReaderIgnoresForeignProjectService proves the
// facade leaves a project service it did not build alone.
func TestQuicBaaS_SetProjectBindingReaderIgnoresForeignProjectService(t *testing.T) {
	facade := &quicBaaS{}
	facade.SetProjectBindingReader(failingBindingReader{err: errors.New("unused")})
	if facade.projectService != nil {
		t.Fatal("the facade grew a project service it was not given")
	}
}
