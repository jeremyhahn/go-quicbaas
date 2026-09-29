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

package orchestration

import (
	"context"
	"errors"
	"testing"
)

// TestStartJob_RefusesAProjectIDThatIsNotAnID is the regression test for #667,
// the direct-call half of #613.
//
// CreateTask learned to refuse a project NAME where an id belongs, which
// closed the auto-spawn path. StartJob is the other way in and took the value
// unexamined: the job's lease pins the project segment of its key from
// whatever the job carried (job_run_lease.go), so a name reached the keyspace
// as if it were an id and the project's concurrency quota then counted a
// subtree nobody registered.
func TestStartJob_RefusesAProjectIDThatIsNotAnID(t *testing.T) {
	refused := []struct{ name, projectID string }{
		{"the project's name, the shape #613 was filed for", "go-quicbaas"},
		{"the shape the API docs used to show", "proj_abc123"},
		{"a project that is not registered and is not an id either", "unknown-project"},
		{"a path", "/workspace/go-quicbaas"},
		{"a uuid with a character changed", "6f9619ff-8b86-d011-b42d-00c04fc964fg"},
		{"whitespace around an otherwise valid id", " 6f9619ff-8b86-d011-b42d-00c04fc964ff "},
	}
	for _, tc := range refused {
		t.Run("refused: "+tc.name, func(t *testing.T) {
			svc := newMockBackendService(t)
			defer cancelAllJobs(svc)

			_, err := svc.StartJob(context.Background(), &StartJobRequest{
				TenantID:      "tenant-1",
				Description:   "work",
				routedBackend: "mock",
				ProjectID:     tc.projectID,
				MaxIterations: 1,
			})
			var invalid *InvalidProjectIDError
			if !errors.As(err, &invalid) || !errors.Is(err, ErrInvalidProjectID) {
				t.Fatalf("got %v, want InvalidProjectIDError", err)
			}
			if invalid.ProjectID != tc.projectID || invalid.Field != "project_id" {
				t.Errorf("the refusal must name the field and the value: %+v", invalid)
			}
			if n := countRunningJobs(svc); n != 0 {
				t.Fatalf("a refused request starts nothing, %d jobs were launched", n)
			}
		})
	}

	// The other direction, so the guard cannot pass by refusing everything.
	accepted := []struct{ name, projectID string }{
		{"unset, an unscoped job", ""},
		{"a canonical lower-case uuid", "6f9619ff-8b86-d011-b42d-00c04fc964ff"},
		{"an upper-case uuid, which is the same id", "6F9619FF-8B86-D011-B42D-00C04FC964FF"},
	}
	for _, tc := range accepted {
		t.Run("accepted: "+tc.name, func(t *testing.T) {
			svc := newMockBackendService(t)
			defer cancelAllJobs(svc)

			resp, err := svc.StartJob(context.Background(), &StartJobRequest{
				TenantID:      "tenant-1",
				Description:   "work",
				routedBackend: "mock",
				ProjectID:     tc.projectID,
				MaxIterations: 1,
			})
			if err != nil {
				t.Fatalf("StartJob(%q): %v", tc.projectID, err)
			}
			if resp.JobID == "" {
				t.Fatal("an accepted request starts a job")
			}
		})
	}
}
