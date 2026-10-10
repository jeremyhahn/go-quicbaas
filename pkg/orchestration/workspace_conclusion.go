package orchestration

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// workspaceAttemptMarker separates the task ID from the attempt number in the
// workspace key the sdlc runner derives: <task id>-a<attempt>.
const workspaceAttemptMarker = "-a"

// WorkspaceConclusions answers whether the task a session workspace belongs to
// has concluded, so the startup sweep may remove the checkout.
//
// THE ANSWER FAILS CLOSED. true means the store positively says the task is
// over; anything else — a live task, an unknown key, a store error — is false
// or an error, and the sweep keeps the workspace in all three cases.
type WorkspaceConclusions interface {
	// WorkspaceConcluded reports whether the workspace named by the tenant,
	// project and key belongs to a concluded task.
	WorkspaceConcluded(ctx context.Context, tenantID, projectID, key string) (bool, error)
}

// TaskWorkspaceConclusions resolves workspace conclusion from the task store.
// A key names its task as <task id>-a<attempt>; a key that does not parse that
// way, or whose task the store does not hold, belongs to no task this store
// can vouch for and is never reported concluded.
type TaskWorkspaceConclusions struct {
	Store TaskStore
}

var _ WorkspaceConclusions = TaskWorkspaceConclusions{}

// WorkspaceConcluded implements WorkspaceConclusions. Completed, failed and
// cancelled are the concluded statuses; a blocked task is parked, not over —
// readmission returns it to pending on the same checkout.
func (c TaskWorkspaceConclusions) WorkspaceConcluded(ctx context.Context, tenantID, projectID, key string) (bool, error) {
	if c.Store == nil {
		return false, nil
	}
	taskID, ok := workspaceKeyTaskID(key)
	if !ok {
		return false, nil
	}
	task, err := c.Store.GetTask(ctx, tenantID, taskID)
	if errors.Is(err, ErrTaskNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if task.ProjectID != projectID {
		return false, nil
	}
	switch task.Status {
	case TaskStatusCompleted, TaskStatusFailed, TaskStatusCancelled:
		return true, nil
	}
	return false, nil
}

// workspaceKeyTaskID extracts the task ID from a <task id>-a<attempt> key.
func workspaceKeyTaskID(key string) (string, bool) {
	cut := strings.LastIndex(key, workspaceAttemptMarker)
	if cut <= 0 {
		return "", false
	}
	if n, err := strconv.Atoi(key[cut+len(workspaceAttemptMarker):]); err != nil || n < 1 {
		return "", false
	}
	return key[:cut], true
}

// WorkspaceEscapeError reports a removal refused because the path does not
// stay under the directory the sweep is confined to.
type WorkspaceEscapeError struct {
	// Path is the refused path.
	Path string
}

// Error implements the error interface.
func (e *WorkspaceEscapeError) Error() string {
	return "workspace: refusing to remove " + strconv.Quote(e.Path) + ": not under the workspace root"
}

// SetConclusions installs the resolver the sweep consults before removing a
// session workspace. Without one the sweep removes spec directories only.
// Call it before SweepSessionSpecs; it is not safe against a concurrent sweep.
func (m *WorkspaceManager) SetConclusions(c WorkspaceConclusions) { m.conclusions = c }

// removeConfined removes rel under root, refusing a path that is not local to
// it. os.Root enforces the same bound on the filesystem; this refuses first,
// with a typed error, so the refusal never depends on what the path resolves to.
func removeConfined(root *os.Root, rel string) error {
	if !filepath.IsLocal(rel) {
		return &WorkspaceEscapeError{Path: rel}
	}
	return root.RemoveAll(rel)
}

// sweepConcludedWorkspace removes the session workspace entry when its task
// has concluded, returning the removed path or "". Anything that is not a
// plain directory with a valid key, and any resolver doubt, keeps it.
func (m *WorkspaceManager) sweepConcludedWorkspace(root *os.Root, tenantDir, tenantID, projectID, parent string, entry fs.DirEntry) (string, error) {
	if m.conclusions == nil || !entry.IsDir() || validateWorkspaceSegment("key", entry.Name()) != nil {
		return "", nil
	}
	concluded, err := m.conclusions.WorkspaceConcluded(context.Background(), tenantID, projectID, entry.Name())
	if err != nil || !concluded {
		return "", nil
	}
	rel := filepath.Join(parent, entry.Name())
	if rmErr := removeConfined(root, rel); rmErr != nil {
		return "", &WorkspaceIOError{Op: "sweep", Path: filepath.Join(tenantDir, rel), Err: rmErr}
	}
	return filepath.Join(tenantDir, rel), nil
}
