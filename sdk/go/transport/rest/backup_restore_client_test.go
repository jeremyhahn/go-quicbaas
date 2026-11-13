package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/backup"
)

// newBackupRestoreServer serves the backup restore route in the server's REST
// response shape, reporting status for every restore, and records the backup
// ID the client restored.
func newBackupRestoreServer(t *testing.T, status string, restoredID *string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/backups/{id}/restore", func(w http.ResponseWriter, r *http.Request) {
		*restoredID = r.PathValue("id")
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{
			"job_id":   "job-1",
			"status":   status,
			"progress": 0.5,
		}))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func TestBackupClient_OverREST_RestoreReportsJobStatus(t *testing.T) {
	tests := []struct {
		name         string
		status       string
		wantRestored bool
	}{
		{name: "running restore is accepted, not finished", status: "running", wantRestored: false},
		{name: "completed restore is finished", status: "completed", wantRestored: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var restoredID string
			server := newBackupRestoreServer(t, tt.status, &restoredID)
			client := backup.NewClient(newTestTransport(t, server))

			resp, err := client.Restore(context.Background(), &backup.RestoreRequest{BackupID: "bkp-1"})
			require.NoError(t, err)
			assert.Equal(t, "bkp-1", restoredID)
			assert.Equal(t, "job-1", resp.JobID)
			assert.Equal(t, tt.status, resp.Status)
			assert.Equal(t, tt.wantRestored, resp.Restored)
		})
	}
}

func TestBackupClient_OverREST_RestoreServerErrorIsReturned(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "backup service unavailable", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	client := backup.NewClient(newTestTransport(t, server))

	resp, err := client.Restore(context.Background(), &backup.RestoreRequest{BackupID: "bkp-1"})
	assert.Error(t, err)
	assert.Nil(t, resp)
}
