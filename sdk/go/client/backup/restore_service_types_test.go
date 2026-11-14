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

package backup

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// TestRestore_SendsServiceRequestType proves Restore sends the backup
// service's own restore request, the type the embedded transport requires
// and the REST transport reads the backup ID from (#1726).
func TestRestore_SendsServiceRequestType(t *testing.T) {
	var got interface{}
	client := NewClient(&mockTransport{
		requestFn: func(_ context.Context, _, _ string, req interface{}) (interface{}, error) {
			got = req
			return &service.RestoreBackupResponse{JobID: "restore-b1", Status: "completed", Progress: 100}, nil
		},
	})

	if _, err := client.Restore(context.Background(), &RestoreRequest{BackupID: "b1"}); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	req, ok := got.(*service.RestoreBackupRequest)
	if !ok {
		t.Fatalf("request type = %T, want *service.RestoreBackupRequest", got)
	}
	if req.BackupID != "b1" {
		t.Errorf("BackupID = %q, want %q", req.BackupID, "b1")
	}
}

// TestRestore_ReportsServiceJobAndStatus proves Restore returns the job id,
// status and progress the service reports, and sets Restored only for a
// finished restore: an accepted, running restore is not an error and not
// restored (#1726).
func TestRestore_ReportsServiceJobAndStatus(t *testing.T) {
	cases := []struct {
		name         string
		resp         *service.RestoreBackupResponse
		wantRestored bool
	}{
		{"running", &service.RestoreBackupResponse{JobID: "restore-b1", Status: "running", Progress: 40}, false},
		{"completed", &service.RestoreBackupResponse{JobID: "restore-b1", Status: "completed", Progress: 100}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := NewClient(&mockTransport{
				requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
					return tc.resp, nil
				},
			})
			resp, err := client.Restore(context.Background(), &RestoreRequest{BackupID: "b1"})
			if err != nil {
				t.Fatalf("Restore() error = %v, want nil", err)
			}
			if resp.Restored != tc.wantRestored {
				t.Errorf("Restored = %v, want %v", resp.Restored, tc.wantRestored)
			}
			if resp.JobID != tc.resp.JobID || resp.Status != tc.resp.Status || resp.Progress != tc.resp.Progress {
				t.Errorf("resp = %+v, want job %q status %q progress %v", resp, tc.resp.JobID, tc.resp.Status, tc.resp.Progress)
			}
		})
	}
}

// TestRestore_SourceIsRefusedBeforeAnyCall proves a restore that names a
// source is refused with a typed invalid-input error and sends nothing: the
// service has no restore source, so the client must not drop it (#1726).
func TestRestore_SourceIsRefusedBeforeAnyCall(t *testing.T) {
	for _, req := range []*RestoreRequest{
		{Source: "s3://bucket/backup"},
		{BackupID: "b1", Source: "s3://bucket/backup"},
	} {
		called := false
		client := NewClient(&mockTransport{
			requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
				called = true
				return nil, nil
			},
		})
		_, err := client.Restore(context.Background(), req)
		if !errors.IsInvalidInput(err) {
			t.Errorf("Restore(%+v) error = %v, want invalid input", req, err)
		}
		if called {
			t.Errorf("Restore(%+v) sent a request, want none", req)
		}
	}
}

// TestRestore_NilServiceResponseIsInternal proves a typed nil service
// response is an internal error, not a panic.
func TestRestore_NilServiceResponseIsInternal(t *testing.T) {
	client := NewClient(&mockTransport{
		requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			var r *service.RestoreBackupResponse
			return r, nil
		},
	})
	if _, err := client.Restore(context.Background(), &RestoreRequest{BackupID: "b1"}); err == nil {
		t.Fatal("Restore() error = nil, want an internal error")
	}
}
