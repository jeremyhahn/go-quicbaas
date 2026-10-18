//go:build rest

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

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestKMSSafetyMethods tests the KMS safety methods with an unreachable server.
func TestKMSSafetyMethods(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	client, err := NewRESTClient("http://localhost:8080", log)
	require.NoError(t, err)

	t.Run("KMSSafetyCheck connection error", func(t *testing.T) {
		_, err := client.KMSSafetyCheck(ctx, &service.SafetyCheckRequest{})
		require.Error(t, err)
	})

	t.Run("KMSSafetyReport connection error", func(t *testing.T) {
		_, err := client.KMSSafetyReport(ctx, &service.SafetyReportRequest{})
		require.Error(t, err)
	})
}

// TestWatchSecretsByPrefixValidation tests input validation for WatchSecretsByPrefix.
func TestWatchSecretsByPrefixValidation(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	client, err := NewRESTClient("http://localhost:8080", log)
	require.NoError(t, err)

	t.Run("nil request returns error", func(t *testing.T) {
		_, err := client.WatchSecretsByPrefix(ctx, nil)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})

	t.Run("empty prefix returns error", func(t *testing.T) {
		_, err := client.WatchSecretsByPrefix(ctx, &service.WatchSecretsRequest{Prefix: ""})
		require.Error(t, err)
		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})
}

// TestWatchSecretsByPrefixServerError tests server error handling.
func TestWatchSecretsByPrefixServerError(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)

	_, err = client.WatchSecretsByPrefix(ctx, &service.WatchSecretsRequest{Prefix: "test"})
	require.Error(t, err)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInternal, qbErr.Code)
}

// TestWatchSecretsByPrefixSuccess tests successful SSE connection.
func TestWatchSecretsByPrefixSuccess(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "text/event-stream", r.Header.Get("Accept"))
		assert.Contains(t, r.URL.Path, "/api/v1/secrets/watch/")

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			w.Write([]byte("data: {}\n\n"))
			f.Flush()
		}
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	changes, err := client.WatchSecretsByPrefix(ctx, &service.WatchSecretsRequest{
		Prefix:       "test",
		StartVersion: 5,
	})
	require.NoError(t, err)
	require.NotNil(t, changes)

	select {
	case <-changes:
	case <-ctx.Done():
	}
}

// TestSchedulerJobConversions tests the scheduler conversion functions.
func TestSchedulerJobConversions(t *testing.T) {
	t.Run("schedulerJobFromREST nil returns nil", func(t *testing.T) {
		assert.Nil(t, schedulerJobFromREST(nil))
	})

	t.Run("schedulerExecutionFromREST nil returns nil", func(t *testing.T) {
		assert.Nil(t, schedulerExecutionFromREST(nil))
	})

	t.Run("schedulerJobFromREST with data", func(t *testing.T) {
		now := time.Now()
		input := &SchedulerJobRESTResponse{
			ID:         123,
			Name:       "test",
			OwnerType:  "user",
			Type:       "cron",
			Handler:    "backup",
			Status:     "scheduled",
			Priority:   5,
			MaxRetries: 3,
			RunCount:   10,
			FailCount:  2,
			Labels:     map[string]string{"env": "test"},
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		result := schedulerJobFromREST(input)
		require.NotNil(t, result)
		assert.Equal(t, uint64(123), result.ID)
		assert.Equal(t, "test", result.Name)
		assert.Equal(t, service.SchedulerOwnerType("user"), result.OwnerType)
	})

	t.Run("schedulerExecutionFromREST with data", func(t *testing.T) {
		now := time.Now()
		input := &SchedulerJobExecutionRESTResponse{
			ID:        456,
			JobID:     123,
			Status:    "completed",
			StartedAt: now.Add(-time.Hour),
			EndedAt:   now,
			Duration:  time.Hour,
			Output:    "success",
		}
		result := schedulerExecutionFromREST(input)
		require.NotNil(t, result)
		assert.Equal(t, uint64(456), result.ID)
		assert.Equal(t, "success", result.Output)
	})
}

// TestKMSDeleteDEKClient tests KMSDeleteDEK client method.
func TestKMSDeleteDEKClient(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	t.Run("nil request returns error", func(t *testing.T) {
		client, _ := NewRESTClient("http://localhost:8080", log)
		_, err := client.KMSDeleteDEK(ctx, nil)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})

	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "/api/v1/kms/deks/")
			json.NewEncoder(w).Encode(service.DeleteDEKResponse{Timestamp: time.Now()})
		}))
		defer server.Close()

		client, _ := NewRESTClient(server.URL, log)
		resp, err := client.KMSDeleteDEK(ctx, &service.DeleteDEKRequest{Path: "test"})
		require.NoError(t, err)
		require.NotNil(t, resp)
	})
}

// TestKMSListKeysClient tests KMSListKeys client method.
func TestKMSListKeysClient(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/kms/keys", r.URL.Path)
		json.NewEncoder(w).Encode(ListKeysRESTResponse{
			Keys:       []*KeyHandleResponse{{KeyID: "k1", Status: "enabled"}},
			TotalCount: 1,
		})
	}))
	defer server.Close()

	client, _ := NewRESTClient(server.URL, log)
	resp, err := client.KMSListKeys(ctx, 10, "")
	require.NoError(t, err)
	assert.Len(t, resp.Keys, 1)
}

// TestListSecretsClient tests ListSecrets client method.
func TestListSecretsClientMethod(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	t.Run("nil request uses defaults", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(service.ListSecretsResponse{Secrets: []*service.Secret{}})
		}))
		defer server.Close()

		client, _ := NewRESTClient(server.URL, log)
		resp, err := client.ListSecrets(ctx, nil)
		require.NoError(t, err)
		require.NotNil(t, resp)
	})
}

// TestGetSecretsByPrefixClient tests GetSecretsByPrefix client method.
func TestGetSecretsByPrefixClient(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	t.Run("nil request returns error", func(t *testing.T) {
		client, _ := NewRESTClient("http://localhost:8080", log)
		_, err := client.GetSecretsByPrefix(ctx, nil)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})
}

// TestPingClient tests the Ping client method which calls /api/v1/health.
func TestPingClientMethod(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	t.Run("healthy server", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v1/health", r.URL.Path)
			json.NewEncoder(w).Encode(HealthResponse{
				Status:    "healthy",
				Timestamp: time.Now(),
				Uptime:    "1h0m0s",
			})
		}))
		defer server.Close()

		client, _ := NewRESTClient(server.URL, log)
		err := client.Ping(ctx)
		require.NoError(t, err)
	})

	t.Run("unhealthy server", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(HealthResponse{
				Status:    "unhealthy",
				Timestamp: time.Now(),
			})
		}))
		defer server.Close()

		client, _ := NewRESTClient(server.URL, log)
		err := client.Ping(ctx)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrUnavailable, qbErr.Code)
	})
}

// TestKMSDecryptClient tests KMSDecrypt nil request.
func TestKMSDecryptClientNil(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	client, _ := NewRESTClient("http://localhost:8080", log)
	_, err := client.KMSDecrypt(ctx, nil)
	require.Error(t, err)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

// TestKMSEncryptClientNil tests KMSEncrypt nil request.
func TestKMSEncryptClientNil(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	client, _ := NewRESTClient("http://localhost:8080", log)
	_, err := client.KMSEncrypt(ctx, nil)
	require.Error(t, err)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

// TestKMSDeleteKeyClient tests KMSDeleteKey client method.
func TestKMSDeleteKeyClient(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	t.Run("empty key id returns error", func(t *testing.T) {
		client, _ := NewRESTClient("http://localhost:8080", log)
		_, err := client.KMSDeleteKey(ctx, "")
		require.Error(t, err)
		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})
}

// TestKMSRotateKeyClient tests KMSRotateKey client method.
func TestKMSRotateKeyClient(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	t.Run("empty key id returns error", func(t *testing.T) {
		client, _ := NewRESTClient("http://localhost:8080", log)
		_, err := client.KMSRotateKey(ctx, "")
		require.Error(t, err)
		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})
}

// TestKMSGetKeyClient tests KMSGetKey client method.
func TestKMSGetKeyClient(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	t.Run("empty key id returns error", func(t *testing.T) {
		client, _ := NewRESTClient("http://localhost:8080", log)
		_, err := client.KMSGetKey(ctx, "")
		require.Error(t, err)
		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})
}

// TestKMSGetDEKClient tests KMSGetDEK client method.
func TestKMSGetDEKClient(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	t.Run("nil request returns error", func(t *testing.T) {
		client, _ := NewRESTClient("http://localhost:8080", log)
		_, err := client.KMSGetDEK(ctx, nil)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})
}

// TestKMSCreateDEKClient tests KMSCreateDEK client method.
func TestKMSCreateDEKClient(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	t.Run("nil request returns error", func(t *testing.T) {
		client, _ := NewRESTClient("http://localhost:8080", log)
		_, err := client.KMSCreateDEK(ctx, nil)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})
}

// TestSecretClientMethods tests various secret client methods.
func TestSecretClientMethods(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	t.Run("StoreSecret nil request returns error", func(t *testing.T) {
		client, _ := NewRESTClient("http://localhost:8080", log)
		_, err := client.StoreSecret(ctx, nil)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})

	t.Run("GetSecret nil request returns error", func(t *testing.T) {
		client, _ := NewRESTClient("http://localhost:8080", log)
		_, err := client.GetSecret(ctx, nil)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})

	t.Run("DeleteSecret nil request returns error", func(t *testing.T) {
		client, _ := NewRESTClient("http://localhost:8080", log)
		_, err := client.DeleteSecret(ctx, nil)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})

	t.Run("RotateSecret nil request returns error", func(t *testing.T) {
		client, _ := NewRESTClient("http://localhost:8080", log)
		_, err := client.RotateSecret(ctx, nil)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})

	t.Run("RollbackSecret nil request returns error", func(t *testing.T) {
		client, _ := NewRESTClient("http://localhost:8080", log)
		_, err := client.RollbackSecret(ctx, nil)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})

	t.Run("UpdateSecretStatus nil request returns error", func(t *testing.T) {
		client, _ := NewRESTClient("http://localhost:8080", log)
		_, err := client.UpdateSecretStatus(ctx, nil)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})

	t.Run("ListSecretVersions nil request returns error", func(t *testing.T) {
		client, _ := NewRESTClient("http://localhost:8080", log)
		_, err := client.ListSecretVersions(ctx, nil)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})
}

// TestClusterClientMethods tests the cluster client methods.
func TestClusterClientMethods(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	t.Run("ClusterStatus success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v1/cluster/status", r.URL.Path)
			// Return REST ClusterStatusResponse which has Healthy field
			json.NewEncoder(w).Encode(ClusterStatusResponse{
				Healthy:    true,
				TotalNodes: 3,
			})
		}))
		defer server.Close()

		client, _ := NewRESTClient(server.URL, log)
		resp, err := client.ClusterStatus(ctx)
		require.NoError(t, err)
		assert.True(t, resp.Healthy)
		assert.Equal(t, 3, resp.NodeCount)
	})
}

// TestKMSImportKeyClient tests KMSImportKey client method.
func TestKMSImportKeyClient(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	t.Run("nil request returns error", func(t *testing.T) {
		client, _ := NewRESTClient("http://localhost:8080", log)
		_, err := client.KMSImportKey(ctx, nil)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})
}

// TestKMSGenerateKeyClient tests KMSGenerateKey client method.
func TestKMSGenerateKeyClient(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	t.Run("nil request returns error", func(t *testing.T) {
		client, _ := NewRESTClient("http://localhost:8080", log)
		_, err := client.KMSGenerateKey(ctx, nil)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})
}

// TestKMSGenerateDataKeyClient tests KMSGenerateDataKey client method.
func TestKMSGenerateDataKeyClient(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	t.Run("nil request returns error", func(t *testing.T) {
		client, _ := NewRESTClient("http://localhost:8080", log)
		_, err := client.KMSGenerateDataKey(ctx, nil)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})
}

// TestKMSGenerateDataKeyWithKEKClient tests KMSGenerateDataKeyWithKEK client method.
func TestKMSGenerateDataKeyWithKEKClient(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	t.Run("empty kek name returns error", func(t *testing.T) {
		client, _ := NewRESTClient("http://localhost:8080", log)
		_, err := client.KMSGenerateDataKeyWithKEK(ctx, "", &DataKeyRESTRequest{})
		require.Error(t, err)
		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})

	t.Run("nil request returns error", func(t *testing.T) {
		client, _ := NewRESTClient("http://localhost:8080", log)
		_, err := client.KMSGenerateDataKeyWithKEK(ctx, "test-kek", nil)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})
}

// TestKMSListKeyVersionsClient tests KMSListKeyVersions client method.
func TestKMSListKeyVersionsClient(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	t.Run("empty key id returns error", func(t *testing.T) {
		client, _ := NewRESTClient("http://localhost:8080", log)
		_, err := client.KMSListKeyVersions(ctx, "")
		require.Error(t, err)
		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})
}

// TestKMSUpdateKeyStatusClient tests KMSUpdateKeyStatus client method.
func TestKMSUpdateKeyStatusClient(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	t.Run("empty key id returns error", func(t *testing.T) {
		client, _ := NewRESTClient("http://localhost:8080", log)
		_, err := client.KMSUpdateKeyStatus(ctx, "", &UpdateKeyStatusRESTRequest{Status: "enabled"})
		require.Error(t, err)
		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})

	t.Run("nil request returns error", func(t *testing.T) {
		client, _ := NewRESTClient("http://localhost:8080", log)
		_, err := client.KMSUpdateKeyStatus(ctx, "test-key", nil)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})
}

// TestKMSListDEKsClient tests KMSListDEKs client method.
func TestKMSListDEKsClient(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	t.Run("nil request uses defaults", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(service.ListDEKsResponse{DEKs: []*service.DEKMetadata{}})
		}))
		defer server.Close()

		client, _ := NewRESTClient(server.URL, log)
		resp, err := client.KMSListDEKs(ctx, nil)
		require.NoError(t, err)
		require.NotNil(t, resp)
	})
}

// TestKMSRotateDEKClient tests KMSRotateDEK client method.
func TestKMSRotateDEKClient(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	t.Run("nil request returns error", func(t *testing.T) {
		client, _ := NewRESTClient("http://localhost:8080", log)
		_, err := client.KMSRotateDEK(ctx, nil)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})
}

// TestKMSGetDEKStatusClient tests KMSGetDEKStatus client method.
func TestKMSGetDEKStatusClient(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	t.Run("nil request returns error", func(t *testing.T) {
		client, _ := NewRESTClient("http://localhost:8080", log)
		_, err := client.KMSGetDEKStatus(ctx, nil)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})
}

// TestKMSSetDefaultDEKClient tests KMSSetDefaultDEK client method.
func TestKMSSetDefaultDEKClient(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	t.Run("nil request returns error", func(t *testing.T) {
		client, _ := NewRESTClient("http://localhost:8080", log)
		_, err := client.KMSSetDefaultDEK(ctx, nil)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})
}

// TestKMSSetDEKSafetyClient tests KMSSetDEKSafety client method.
func TestKMSSetDEKSafetyClient(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	t.Run("nil request returns error", func(t *testing.T) {
		client, _ := NewRESTClient("http://localhost:8080", log)
		_, err := client.KMSSetDEKSafety(ctx, nil)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})
}

// TestKMSClearDEKSafetyClient tests KMSClearDEKSafety client method.
func TestKMSClearDEKSafetyClient(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	t.Run("nil request returns error", func(t *testing.T) {
		client, _ := NewRESTClient("http://localhost:8080", log)
		_, err := client.KMSClearDEKSafety(ctx, nil)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})
}

// Verify cli package is used to satisfy import
var _ = cli.ClusterStatusResponse{}
