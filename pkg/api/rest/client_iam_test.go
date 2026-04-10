//go:build rest && iam

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_KMSEncrypt_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.KMSEncrypt(context.Background(), &service.EncryptRequest{
		KeyID:     "key1",
		Plaintext: []byte("plaintext"),
	})
	assert.Error(t, err)
}

func TestClient_KMSDecrypt_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.KMSDecrypt(context.Background(), &service.DecryptRequest{
		KeyID:      "key1",
		Ciphertext: []byte("ciphertext"),
	})
	assert.Error(t, err)
}

func TestClient_KMSGenerateDataKey_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.KMSGenerateDataKey(context.Background(), &DataKeyRESTRequest{
		KeySizeBits: 256,
	})
	assert.Error(t, err)
}

func TestClient_KMSListKeys_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.KMSListKeys(context.Background(), 100, "")
	assert.Error(t, err)
}

func TestClient_KMSDeleteDEK_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.KMSDeleteDEK(context.Background(), &service.DeleteDEKRequest{
		Path: "dek/path1",
	})
	assert.Error(t, err)
}

func TestClient_SealStatus_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.SealStatus(context.Background())
	assert.Error(t, err)
}

func TestClient_Seal_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	err = client.Seal(context.Background())
	assert.Error(t, err)
}

func TestClient_ListSecrets_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.ListSecrets(context.Background(), &service.ListSecretsRequest{
		Limit: 100,
	})
	assert.Error(t, err)
}

func TestClient_GetSecretsByPrefix_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.GetSecretsByPrefix(context.Background(), &service.GetSecretsByPrefixRequest{
		Prefix: "prefix/",
	})
	assert.Error(t, err)
}

func TestClient_SchedulerTriggerJob_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.SchedulerTriggerJob(context.Background(), &service.TriggerSchedulerJobRequest{JobID: 1})
	assert.Error(t, err)
}

func TestClient_SchedulerPauseJob_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.SchedulerPauseJob(context.Background(), &service.PauseSchedulerJobRequest{JobID: 1})
	assert.Error(t, err)
}

func TestClient_SchedulerResumeJob_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.SchedulerResumeJob(context.Background(), &service.ResumeSchedulerJobRequest{JobID: 1})
	assert.Error(t, err)
}

func TestClient_TSWritePoint_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.TSWritePoint(context.Background(), &cli.WritePointRequest{
		Metric:    "cpu",
		Tags:      map[string]string{"host": "server1"},
		Value:     50.0,
		Timestamp: time.Now(),
	})
	assert.Error(t, err)
}

func TestClient_TSQuery_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.TSQuery(context.Background(), &cli.TSQueryRequest{
		Metric:    "cpu",
		StartTime: time.Now().Add(-time.Hour),
		EndTime:   time.Now(),
	})
	assert.Error(t, err)
}

func TestClient_ClusterStatus_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.ClusterStatus(context.Background())
	assert.Error(t, err)
}

func TestClient_Ping_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	err = client.Ping(context.Background())
	assert.Error(t, err)
}

func TestWithTLS_InvalidCert(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	_, err := NewRESTClient("https://localhost:8443", log, WithTLS("/nonexistent/cert.pem", "/nonexistent/key.pem", "/nonexistent/ca.pem"))
	assert.Error(t, err)
}

func TestWithTLS_ValidPaths(t *testing.T) {
	// Just test that the option function returns without error when paths don't exist
	// The actual file reading happens during client creation
	opt := WithTLS("", "", "")
	assert.NotNil(t, opt)
}

func TestWithTimeout_ValidDuration(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient("http://localhost:8080", log, WithTimeout(30*time.Second))
	require.NoError(t, err)
	defer client.Close()
}

func TestClient_doRequest_InvalidURL(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient("http://localhost:99999", log)
	require.NoError(t, err)
	defer client.Close()

	// This should fail due to connection refused
	_, err = client.KMSListKeys(context.Background(), 100, "")
	assert.Error(t, err)
}

func TestClient_doRequest_ContextCanceled(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log, WithTimeout(10*time.Millisecond))
	require.NoError(t, err)
	defer client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err = client.KMSListKeys(ctx, 100, "")
	assert.Error(t, err)
}

func TestClient_WatchSecretsByPrefix_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err = client.WatchSecretsByPrefix(ctx, &service.WatchSecretsRequest{
		Prefix: "prefix/",
	})
	assert.Error(t, err)
}

func TestClient_KMSGenerateKey_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"key": map[string]interface{}{
				"key_id":    "key123",
				"version":   1,
				"algorithm": "AES-256-GCM",
				"status":    "ENABLED",
			},
		})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	resp, err := client.KMSGenerateKey(context.Background(), &GenerateKeyRESTRequest{
		Name: "testkey",
		KeyAttributes: &KeyAttributesRequest{
			Algorithm: "AES-256-GCM",
			KeyUsage:  []string{"encrypt", "decrypt"},
		},
	})
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestClient_KMSGenerateKey_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.KMSGenerateKey(context.Background(), &GenerateKeyRESTRequest{
		Name: "testkey",
		KeyAttributes: &KeyAttributesRequest{
			Algorithm: "AES-256-GCM",
			KeyUsage:  []string{"encrypt", "decrypt"},
		},
	})
	assert.Error(t, err)
}

func TestClient_KMSImportKey_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.KMSImportKey(context.Background(), &ImportKeyRESTRequest{
		Name:          "testkey",
		KeyMaterial:   []byte("keymaterial"),
		WrappingKeyID: "wrappingkey",
	})
	assert.Error(t, err)
}

func TestClient_KMSRotateKey_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.KMSRotateKey(context.Background(), "key1")
	assert.Error(t, err)
}

func TestClient_KMSDeleteKey_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.KMSDeleteKey(context.Background(), "key1")
	assert.Error(t, err)
}

func TestClient_KMSListKeyVersions_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.KMSListKeyVersions(context.Background(), "key1")
	assert.Error(t, err)
}

func TestClient_KMSUpdateKeyStatus_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.KMSUpdateKeyStatus(context.Background(), "key1", &UpdateKeyStatusRESTRequest{
		Status: "disabled",
	})
	assert.Error(t, err)
}

func TestClient_KMSCreateDEK_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.KMSCreateDEK(context.Background(), &service.CreateDEKRequest{
		Path: "dek/path1",
	})
	assert.Error(t, err)
}

func TestClient_KMSGetDEK_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.KMSGetDEK(context.Background(), &service.GetDEKRequest{
		Path: "dek/path1",
	})
	assert.Error(t, err)
}

func TestClient_KMSListDEKs_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.KMSListDEKs(context.Background(), &service.ListDEKsRequest{
		Prefix: "dek/",
	})
	assert.Error(t, err)
}

func TestClient_KMSRotateDEK_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.KMSRotateDEK(context.Background(), &service.RotateDEKRequest{
		Path: "dek/path1",
	})
	assert.Error(t, err)
}

func TestClient_KMSGetDEKStatus_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.KMSGetDEKStatus(context.Background(), &service.GetDEKStatusRequest{
		Path: "dek/path1",
	})
	assert.Error(t, err)
}

func TestClient_KMSSetDefaultDEK_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.KMSSetDefaultDEK(context.Background(), &service.SetDefaultDEKRequest{
		Path: "dek/path1",
	})
	assert.Error(t, err)
}

func TestClient_KMSSetDEKSafety_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.KMSSetDEKSafety(context.Background(), &service.SetDEKSafetyRequest{
		Path:          "dek/path1",
		MaxOperations: 1000000,
		MaxBytes:      1073741824,
	})
	assert.Error(t, err)
}

func TestClient_KMSClearDEKSafety_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.KMSClearDEKSafety(context.Background(), &service.ClearDEKSafetyRequest{
		Path: "dek/path1",
	})
	assert.Error(t, err)
}

func TestClient_SealInit_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.SealInit(context.Background(), &service.SealInitRequest{
		TotalShares: 5,
		Threshold:   3,
	})
	assert.Error(t, err)
}

func TestClient_Unseal_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.Unseal(context.Background(), &service.UnsealRequest{
		Share: []byte("key-share-1"),
	})
	assert.Error(t, err)
}

func TestClient_Rekey_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.Rekey(context.Background(), &service.RekeyRequest{
		NewTotalShares: 5,
		NewThreshold:   3,
	})
	assert.Error(t, err)
}

func TestClient_StoreSecret_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.StoreSecret(context.Background(), &service.StoreSecretRequest{
		Name: "secret/path",
		Data: []byte("secret-data"),
	})
	assert.Error(t, err)
}

func TestClient_GetSecret_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.GetSecret(context.Background(), &service.GetSecretRequest{
		SecretID: "secret/path",
	})
	assert.Error(t, err)
}

func TestClient_DeleteSecret_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.DeleteSecret(context.Background(), &service.DeleteSecretRequest{
		SecretID: "secret/path",
	})
	assert.Error(t, err)
}

func TestClient_ListSecretVersions_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.ListSecretVersions(context.Background(), &service.ListSecretVersionsRequest{
		SecretID: "secret/path",
	})
	assert.Error(t, err)
}

func TestClient_RollbackSecret_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.RollbackSecret(context.Background(), &service.RollbackSecretRequest{
		SecretID:      "secret/path",
		TargetVersion: 1,
	})
	assert.Error(t, err)
}

func TestClient_RotateSecret_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.RotateSecret(context.Background(), &service.RotateSecretRequest{
		SecretID: "secret/path",
		NewData:  []byte("new-secret"),
	})
	assert.Error(t, err)
}

func TestClient_UpdateSecretStatus_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.UpdateSecretStatus(context.Background(), &service.UpdateSecretStatusRequest{
		SecretID: "secret/path",
		Status:   service.SecretStatusDisabled,
	})
	assert.Error(t, err)
}

func TestClient_SchedulerGetJob_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.SchedulerGetJob(context.Background(), &service.GetSchedulerJobRequest{JobID: 1})
	assert.Error(t, err)
}

func TestClient_SchedulerUpdateJob_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.SchedulerUpdateJob(context.Background(), &service.UpdateSchedulerJobRequest{JobID: 1})
	assert.Error(t, err)
}

func TestClient_SchedulerListJobs_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.SchedulerListJobs(context.Background(), &service.ListSchedulerJobsRequest{})
	assert.Error(t, err)
}

func TestClient_SchedulerCancelJob_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.SchedulerCancelJob(context.Background(), &service.CancelSchedulerJobRequest{JobID: 1})
	assert.Error(t, err)
}

func TestClient_SchedulerGetJobHistory_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.SchedulerGetJobHistory(context.Background(), &service.GetSchedulerJobHistoryRequest{JobID: 1})
	assert.Error(t, err)
}

func TestClient_SchedulerGetStatus_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.SchedulerGetStatus(context.Background())
	assert.Error(t, err)
}

func TestClient_KMSGenerateDataKeyWithKEK_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer ts.Close()

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	client, err := NewRESTClient(ts.URL, log)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.KMSGenerateDataKeyWithKEK(context.Background(), "kek1", &DataKeyRESTRequest{
		KeySizeBits: 256,
	})
	assert.Error(t, err)
}
