// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package mcp

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	backupclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/backup"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// The restore tool answers with the server's RestoreBackupResponse marshalled
// as is: the service struct carries no JSON tags, so its keys are the Go field
// names (JobID, Status).
func TestBackupClientOverMCP_RestoreReportsJobStatus(t *testing.T) {
	tests := []struct {
		status       string
		wantRestored bool
	}{
		{status: "completed", wantRestored: true},
		{status: "running", wantRestored: false},
		{status: "failed", wantRestored: false},
	}
	for _, tc := range tests {
		t.Run(tc.status, func(t *testing.T) {
			tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
				wantTool: "quicbaas_backup_restore",
				assertArgs: func(t *testing.T, args map[string]interface{}) {
					if args["backup_id"] != "bk-1" {
						t.Errorf("backup_id = %v, want bk-1", args["backup_id"])
					}
				},
				result: dekToolResult(t, &service.RestoreBackupResponse{
					JobID: "restore-bk-1", Status: tc.status, Progress: 50,
				}),
			})
			defer cleanup()

			resp, err := backupclient.NewClient(tr).Restore(context.Background(),
				&backupclient.RestoreRequest{BackupID: "bk-1"})
			if err != nil {
				t.Fatalf("Restore: %v", err)
			}
			if resp.JobID != "restore-bk-1" {
				t.Errorf("JobID = %q, want restore-bk-1", resp.JobID)
			}
			if resp.Status != tc.status {
				t.Errorf("Status = %q, want %q", resp.Status, tc.status)
			}
			if resp.Restored != tc.wantRestored {
				t.Errorf("Restored = %v, want %v", resp.Restored, tc.wantRestored)
			}
		})
	}
}

func TestBackupClientOverMCP_MalformedRestoreIsInternal(t *testing.T) {
	tests := map[string]map[string]interface{}{
		"job id not a string": {"JobID": 7, "Status": "completed"},
		"status not a string": {"JobID": "restore-bk-1", "Status": true},
	}
	for name, result := range tests {
		t.Run(name, func(t *testing.T) {
			tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
				wantTool: "quicbaas_backup_restore",
				result:   result,
			})
			defer cleanup()

			_, err := backupclient.NewClient(tr).Restore(context.Background(),
				&backupclient.RestoreRequest{BackupID: "bk-1"})
			if sdkerrors.Code(err) != sdkerrors.CodeInternal {
				t.Fatalf("err = %v, want a typed internal error", err)
			}
		})
	}
}

func TestTransformBackupResponse_OtherVerbPassesThrough(t *testing.T) {
	result := map[string]interface{}{"backup_id": "bk-1"}
	got, err := transformBackupResponse("Verify", result)
	if err != nil {
		t.Fatalf("transformBackupResponse: %v", err)
	}
	if m, ok := got.(map[string]interface{}); !ok || m["backup_id"] != "bk-1" {
		t.Errorf("result = %#v, want the tool result unchanged", got)
	}
}

func TestBackupClientOverMCP_CreateReturnsBackupID(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_backup_create",
		result: dekToolResult(t, &service.CreateBackupResponse{
			BackupID: "bk-1", Size: 42, Timestamp: 1700000000, Status: "completed",
		}),
	})
	defer cleanup()

	resp, err := backupclient.NewClient(tr).Create(context.Background(), &backupclient.BackupRequest{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if resp.BackupID != "bk-1" {
		t.Errorf("BackupID = %q, want bk-1", resp.BackupID)
	}
	if resp.Size != 42 {
		t.Errorf("Size = %d, want 42", resp.Size)
	}
	if resp.Status != "completed" {
		t.Errorf("Status = %q, want completed", resp.Status)
	}
}

func TestBackupClientOverMCP_MalformedCreateIsInternal(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_backup_create",
		result:   map[string]interface{}{"BackupID": 7, "Status": "completed"},
	})
	defer cleanup()

	_, err := backupclient.NewClient(tr).Create(context.Background(), &backupclient.BackupRequest{})
	if sdkerrors.Code(err) != sdkerrors.CodeInternal {
		t.Fatalf("err = %v, want a typed internal error", err)
	}
}

func TestBackupClientOverMCP_ListReturnsBackups(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_backup_list",
		result: dekToolResult(t, &service.ListBackupsResponse{
			Backups:    []*service.BackupMetadata{{BackupID: "bk-1", Size: 42, Status: "completed"}},
			TotalCount: 1,
		}),
	})
	defer cleanup()

	resp, err := backupclient.NewClient(tr).List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(resp.Backups) != 1 || resp.Backups[0].ID != "bk-1" {
		t.Errorf("Backups = %#v, want one backup bk-1", resp.Backups)
	}
}

func TestIsMCPBackupService(t *testing.T) {
	for service, want := range map[string]bool{
		"Backup": true, "quicbaas.Backup": true, "DEK": false, "BackupAdmin": false,
	} {
		if got := isMCPBackupService(service); got != want {
			t.Errorf("isMCPBackupService(%q) = %v, want %v", service, got, want)
		}
	}
}
