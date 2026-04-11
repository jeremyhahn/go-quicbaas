//go:build quic

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

package quic

import (
	"context"
	stderrors "errors"
	"testing"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Client Nil Request Validation Tests
// These tests validate that all client methods properly reject nil requests
// =============================================================================

// createTestClient creates a minimal client for nil request testing.
// This client is in a closed state so sendRequest will fail, allowing us
// to test the nil check that happens before sendRequest.
func createTestClient(t *testing.T) *Client {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	log := createClientTestLogger()

	return &Client{
		addr:       "test:8443",
		connected:  false,
		closed:     true, // Closed so sendRequest will fail after nil check
		ctx:        ctx,
		cancelFunc: cancel,
		logger:     log,
	}
}

// verifyNilRequestError verifies that an error is a QBError with ErrInvalidInput code.
func verifyNilRequestError(t *testing.T, err error, methodName string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected error for nil request", methodName)
	}
	qbErr, ok := err.(*errors.QBError)
	if !ok {
		t.Fatalf("%s: expected QBError, got %T", methodName, err)
	}
	if qbErr.Code != errors.ErrInvalidInput {
		t.Errorf("%s: expected ErrInvalidInput, got %v", methodName, qbErr.Code)
	}
}

// =============================================================================
// Data Operations - Nil Request Tests
// =============================================================================

func TestClient_Put_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.Put(context.Background(), nil)
	verifyNilRequestError(t, err, "Put")
}

func TestClient_Get_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.Get(context.Background(), nil)
	verifyNilRequestError(t, err, "Get")
}

func TestClient_Delete_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.Delete(context.Background(), nil)
	verifyNilRequestError(t, err, "Delete")
}

func TestClient_List_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.List(context.Background(), nil)
	verifyNilRequestError(t, err, "List")
}

// =============================================================================
// KMS Operations - Nil Request Tests
// =============================================================================

func TestClient_KMSGenerateKey_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSGenerateKey(context.Background(), nil)
	verifyNilRequestError(t, err, "KMSGenerateKey")
}

func TestClient_KMSImportKey_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSImportKey(context.Background(), nil)
	verifyNilRequestError(t, err, "KMSImportKey")
}

func TestClient_KMSGetKey_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSGetKey(context.Background(), nil)
	verifyNilRequestError(t, err, "KMSGetKey")
}

func TestClient_KMSRotateKey_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSRotateKey(context.Background(), nil)
	verifyNilRequestError(t, err, "KMSRotateKey")
}

func TestClient_KMSDeleteKey_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSDeleteKey(context.Background(), nil)
	verifyNilRequestError(t, err, "KMSDeleteKey")
}

func TestClient_KMSEncrypt_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSEncrypt(context.Background(), nil)
	verifyNilRequestError(t, err, "KMSEncrypt")
}

func TestClient_KMSDecrypt_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSDecrypt(context.Background(), nil)
	verifyNilRequestError(t, err, "KMSDecrypt")
}

func TestClient_KMSListKeyVersions_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSListKeyVersions(context.Background(), nil)
	verifyNilRequestError(t, err, "KMSListKeyVersions")
}

func TestClient_KMSUpdateKeyStatus_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSUpdateKeyStatus(context.Background(), nil)
	verifyNilRequestError(t, err, "KMSUpdateKeyStatus")
}

func TestClient_KMSGenerateDataKey_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSGenerateDataKey(context.Background(), nil)
	verifyNilRequestError(t, err, "KMSGenerateDataKey")
}

// =============================================================================
// DEK Operations - Nil Request Tests
// =============================================================================

func TestClient_KMSCreateDEK_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSCreateDEK(context.Background(), nil)
	verifyNilRequestError(t, err, "KMSCreateDEK")
}

func TestClient_KMSGetDEK_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSGetDEK(context.Background(), nil)
	verifyNilRequestError(t, err, "KMSGetDEK")
}

func TestClient_KMSRotateDEK_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSRotateDEK(context.Background(), nil)
	verifyNilRequestError(t, err, "KMSRotateDEK")
}

func TestClient_KMSDeleteDEK_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSDeleteDEK(context.Background(), nil)
	verifyNilRequestError(t, err, "KMSDeleteDEK")
}

func TestClient_KMSGetDEKStatus_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSGetDEKStatus(context.Background(), nil)
	verifyNilRequestError(t, err, "KMSGetDEKStatus")
}

func TestClient_KMSSetDefaultDEK_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSSetDefaultDEK(context.Background(), nil)
	verifyNilRequestError(t, err, "KMSSetDefaultDEK")
}

func TestClient_KMSSetDEKSafety_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSSetDEKSafety(context.Background(), nil)
	verifyNilRequestError(t, err, "KMSSetDEKSafety")
}

func TestClient_KMSClearDEKSafety_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSClearDEKSafety(context.Background(), nil)
	verifyNilRequestError(t, err, "KMSClearDEKSafety")
}

// =============================================================================
// Key Export/Unwrap/Safety Operations - Nil Request Tests
// =============================================================================

func TestClient_KMSExportKey_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSExportKey(context.Background(), nil)
	verifyNilRequestError(t, err, "KMSExportKey")
}

func TestClient_KMSUnwrapKey_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSUnwrapKey(context.Background(), nil)
	verifyNilRequestError(t, err, "KMSUnwrapKey")
}

func TestClient_KMSSetKeySafety_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSSetKeySafety(context.Background(), nil)
	verifyNilRequestError(t, err, "KMSSetKeySafety")
}

func TestClient_KMSClearKeySafety_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSClearKeySafety(context.Background(), nil)
	verifyNilRequestError(t, err, "KMSClearKeySafety")
}

// =============================================================================
// Seal Operations - Nil Request Tests
// =============================================================================

func TestClient_SealInit_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.SealInit(context.Background(), nil)
	verifyNilRequestError(t, err, "SealInit")
}

func TestClient_Unseal_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.Unseal(context.Background(), nil)
	verifyNilRequestError(t, err, "Unseal")
}

func TestClient_Rekey_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.Rekey(context.Background(), nil)
	verifyNilRequestError(t, err, "Rekey")
}

// =============================================================================
// Secrets Operations - Nil Request Tests
// =============================================================================

func TestClient_StoreSecret_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.StoreSecret(context.Background(), nil)
	verifyNilRequestError(t, err, "StoreSecret")
}

func TestClient_GetSecret_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.GetSecret(context.Background(), nil)
	verifyNilRequestError(t, err, "GetSecret")
}

func TestClient_ListSecrets_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.ListSecrets(context.Background(), nil)
	verifyNilRequestError(t, err, "ListSecrets")
}

func TestClient_DeleteSecret_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.DeleteSecret(context.Background(), nil)
	verifyNilRequestError(t, err, "DeleteSecret")
}

func TestClient_ListSecretVersions_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.ListSecretVersions(context.Background(), nil)
	verifyNilRequestError(t, err, "ListSecretVersions")
}

func TestClient_RollbackSecret_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.RollbackSecret(context.Background(), nil)
	verifyNilRequestError(t, err, "RollbackSecret")
}

func TestClient_RotateSecret_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.RotateSecret(context.Background(), nil)
	verifyNilRequestError(t, err, "RotateSecret")
}

func TestClient_UpdateSecretStatus_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.UpdateSecretStatus(context.Background(), nil)
	verifyNilRequestError(t, err, "UpdateSecretStatus")
}

func TestClient_GetSecretsByPrefix_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.GetSecretsByPrefix(context.Background(), nil)
	verifyNilRequestError(t, err, "GetSecretsByPrefix")
}

func TestClient_WatchSecretsByPrefix_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.WatchSecretsByPrefix(context.Background(), nil)
	verifyNilRequestError(t, err, "WatchSecretsByPrefix")
}

// =============================================================================
// Cluster Operations - Nil Request Tests
// =============================================================================

func TestClient_GetLeader_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.GetLeader(context.Background(), nil)
	verifyNilRequestError(t, err, "GetLeader")
}

func TestClient_AddNode_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.AddNode(context.Background(), nil)
	verifyNilRequestError(t, err, "AddNode")
}

func TestClient_RemoveNode_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.RemoveNode(context.Background(), nil)
	verifyNilRequestError(t, err, "RemoveNode")
}

func TestClient_CreateShard_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.CreateShard(context.Background(), nil)
	verifyNilRequestError(t, err, "CreateShard")
}

// =============================================================================
// Scheduler Operations - Nil Request Tests
// =============================================================================

func TestClient_SchedulerCreateJob_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.SchedulerCreateJob(context.Background(), nil)
	verifyNilRequestError(t, err, "SchedulerCreateJob")
}

func TestClient_SchedulerGetJob_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.SchedulerGetJob(context.Background(), nil)
	verifyNilRequestError(t, err, "SchedulerGetJob")
}

func TestClient_SchedulerUpdateJob_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.SchedulerUpdateJob(context.Background(), nil)
	verifyNilRequestError(t, err, "SchedulerUpdateJob")
}

func TestClient_SchedulerDeleteJob_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.SchedulerDeleteJob(context.Background(), nil)
	verifyNilRequestError(t, err, "SchedulerDeleteJob")
}

func TestClient_SchedulerTriggerJob_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.SchedulerTriggerJob(context.Background(), nil)
	verifyNilRequestError(t, err, "SchedulerTriggerJob")
}

func TestClient_SchedulerPauseJob_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.SchedulerPauseJob(context.Background(), nil)
	verifyNilRequestError(t, err, "SchedulerPauseJob")
}

func TestClient_SchedulerResumeJob_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.SchedulerResumeJob(context.Background(), nil)
	verifyNilRequestError(t, err, "SchedulerResumeJob")
}

func TestClient_SchedulerCancelJob_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.SchedulerCancelJob(context.Background(), nil)
	verifyNilRequestError(t, err, "SchedulerCancelJob")
}

func TestClient_SchedulerGetJobHistory_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.SchedulerGetJobHistory(context.Background(), nil)
	verifyNilRequestError(t, err, "SchedulerGetJobHistory")
}

// =============================================================================
// sendRequest Tests - Client Closed Path
// =============================================================================

func TestClient_sendRequest_ClientClosed(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	// The client is closed, so sendRequest should return an error
	_, err := client.sendRequest(context.Background(), OpGet, []byte("test"))
	if err == nil {
		t.Fatal("expected error for closed client")
	}

	qbErr, ok := err.(*errors.QBError)
	if !ok {
		t.Fatalf("expected QBError, got %T", err)
	}
	if qbErr.Code != errors.ErrUnavailable {
		t.Errorf("expected ErrUnavailable, got %v", qbErr.Code)
	}
}

// =============================================================================
// Client with Valid Request but Closed Connection Tests
// These test the path where nil check passes but sendRequest fails
// =============================================================================

func TestClient_Put_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.Put(context.Background(), &service.PutRequest{EntityType: "test"})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_Get_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.Get(context.Background(), &service.GetRequest{EntityType: "test", ID: 1})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_Delete_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.Delete(context.Background(), &service.DeleteRequest{EntityType: "test", ID: 1})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_List_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.List(context.Background(), &service.ListRequest{})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_KMSGenerateKey_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSGenerateKey(context.Background(), &service.GenerateKeyRequest{Name: "test"})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_KMSImportKey_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSImportKey(context.Background(), &service.ImportKeyRequest{WrappingKeyID: "test"})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_KMSGetKey_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSGetKey(context.Background(), &service.GetKeyRequest{KeyID: "test"})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_KMSListKeys_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	// KMSListKeys allows nil request (creates empty one)
	_, err := client.KMSListKeys(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_KMSListDEKs_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	// KMSListDEKs allows nil request (creates empty one)
	_, err := client.KMSListDEKs(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_KMSSafetyCheck_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	// KMSSafetyCheck allows nil request (creates empty one)
	_, err := client.KMSSafetyCheck(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_KMSSafetyReport_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	// KMSSafetyReport allows nil request (creates empty one)
	_, err := client.KMSSafetyReport(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_KMSBackendStatus_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSBackendStatus(context.Background())
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_KMSBackendTest_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSBackendTest(context.Background())
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_SealStatus_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.SealStatus(context.Background())
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_Seal_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	err := client.Seal(context.Background())
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_ClusterStatus_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	// ClusterStatus allows nil request
	_, err := client.ClusterStatus(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_RebalanceCluster_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	// RebalanceCluster allows nil request
	_, err := client.RebalanceCluster(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_SchedulerListJobs_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	// SchedulerListJobs allows nil request
	_, err := client.SchedulerListJobs(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_SchedulerGetStatus_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.SchedulerGetStatus(context.Background())
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

// =============================================================================
// Additional Client Method Tests for Coverage
// =============================================================================

func TestClient_KMSRotateKey_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSRotateKey(context.Background(), &service.RotateKeyRequest{KeyID: "test"})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_KMSDeleteKey_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSDeleteKey(context.Background(), &service.DeleteKeyRequest{KeyID: "test"})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_KMSEncrypt_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSEncrypt(context.Background(), &service.EncryptRequest{KeyID: "test"})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_KMSDecrypt_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSDecrypt(context.Background(), &service.DecryptRequest{KeyID: "test"})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_KMSListKeyVersions_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSListKeyVersions(context.Background(), &service.ListKeyVersionsRequest{KeyID: "test"})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_KMSUpdateKeyStatus_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSUpdateKeyStatus(context.Background(), &service.UpdateKeyStatusRequest{KeyID: "test"})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_KMSGenerateDataKey_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSGenerateDataKey(context.Background(), &service.GenerateDataKeyRequest{KeyID: "test"})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_KMSCreateDEK_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSCreateDEK(context.Background(), &service.CreateDEKRequest{Path: "test"})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_KMSGetDEK_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSGetDEK(context.Background(), &service.GetDEKRequest{Path: "test"})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_KMSRotateDEK_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSRotateDEK(context.Background(), &service.RotateDEKRequest{Path: "test"})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_KMSDeleteDEK_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSDeleteDEK(context.Background(), &service.DeleteDEKRequest{Path: "test"})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_KMSGetDEKStatus_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSGetDEKStatus(context.Background(), &service.GetDEKStatusRequest{Path: "test"})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_KMSSetDefaultDEK_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSSetDefaultDEK(context.Background(), &service.SetDefaultDEKRequest{Path: "test"})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_KMSSetDEKSafety_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSSetDEKSafety(context.Background(), &service.SetDEKSafetyRequest{Path: "test"})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_KMSClearDEKSafety_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSClearDEKSafety(context.Background(), &service.ClearDEKSafetyRequest{Path: "test"})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_KMSExportKey_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSExportKey(context.Background(), &service.ExportKeyRequest{KeyID: "test"})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_KMSUnwrapKey_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSUnwrapKey(context.Background(), &service.UnwrapKeyRequest{WrappingKeyID: "test"})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_KMSSetKeySafety_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSSetKeySafety(context.Background(), &service.SetKeySafetyRequest{KeyID: "test"})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_KMSClearKeySafety_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.KMSClearKeySafety(context.Background(), &service.ClearKeySafetyRequest{KeyID: "test"})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_SealInit_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.SealInit(context.Background(), &service.SealInitRequest{})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_Unseal_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.Unseal(context.Background(), &service.UnsealRequest{Share: []byte("test")})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_Rekey_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.Rekey(context.Background(), &service.RekeyRequest{})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_StoreSecret_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.StoreSecret(context.Background(), &service.StoreSecretRequest{Name: "test"})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_GetSecret_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.GetSecret(context.Background(), &service.GetSecretRequest{SecretID: "test"})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_ListSecrets_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.ListSecrets(context.Background(), &service.ListSecretsRequest{})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_DeleteSecret_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.DeleteSecret(context.Background(), &service.DeleteSecretRequest{SecretID: "test"})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_ListSecretVersions_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.ListSecretVersions(context.Background(), &service.ListSecretVersionsRequest{SecretID: "test"})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_GetLeader_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.GetLeader(context.Background(), &service.GetLeaderRequest{ShardID: 1})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_AddNode_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.AddNode(context.Background(), &service.AddNodeRequest{})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_RemoveNode_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.RemoveNode(context.Background(), &service.RemoveNodeRequest{})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_CreateShard_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.CreateShard(context.Background(), &service.CreateShardRequest{})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

// =============================================================================
// WatchSecretsByPrefix - Unsupported on QUIC Transport
// =============================================================================

func TestClient_WatchSecretsByPrefix_FeatureUnsupported(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	// Test with valid request - should return feature unsupported error
	_, err := client.WatchSecretsByPrefix(context.Background(), &service.WatchSecretsRequest{Prefix: "test"})
	if err == nil {
		t.Fatal("expected error for unsupported streaming operation")
	}

	qbErr, ok := err.(*errors.QBError)
	if !ok {
		t.Fatalf("expected QBError, got %T", err)
	}
	if qbErr.Code != errors.ErrInternal {
		t.Errorf("expected ErrInternal (feature unsupported), got %v", qbErr.Code)
	}

	var featureErr *api.FeatureUnsupportedError
	if !stderrors.As(qbErr.Err, &featureErr) {
		t.Fatalf("expected FeatureUnsupportedError, got %T: %v", qbErr.Err, qbErr.Err)
	}
}

// =============================================================================
// ClientConfig Tests
// =============================================================================

func TestClient_WithTLSConfig(t *testing.T) {
	cfg := &clientConfig{}
	// Test with nil - should set nil
	opt := WithTLSConfig(nil)
	opt(cfg)
	if cfg.tlsConfig != nil {
		t.Error("expected nil TLS config")
	}
}

func TestClient_WithLogger(t *testing.T) {
	cfg := &clientConfig{}
	logCfg := logger.LogConfig{
		Level:     "debug",
		Format:    "json",
		Output:    "stdout",
		Component: "test",
	}
	log, _ := logger.New(logCfg)
	opt := WithLogger(log)
	opt(cfg)
	if cfg.logger != log {
		t.Error("expected logger to be set")
	}
}

// =============================================================================
// sendSchedulerRequest Tests
// =============================================================================

func TestClient_sendSchedulerRequest_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.sendSchedulerRequest(context.Background(), SubOpSchedulerCreateJob, []byte("test"))
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

// =============================================================================
// Additional Scheduler Method Tests for Coverage
// =============================================================================

func TestClient_SchedulerCreateJob_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.SchedulerCreateJob(context.Background(), &service.CreateSchedulerJobRequest{})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_SchedulerGetJob_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.SchedulerGetJob(context.Background(), &service.GetSchedulerJobRequest{JobID: 1})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_SchedulerUpdateJob_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.SchedulerUpdateJob(context.Background(), &service.UpdateSchedulerJobRequest{JobID: 1})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_SchedulerDeleteJob_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.SchedulerDeleteJob(context.Background(), &service.DeleteSchedulerJobRequest{JobID: 1})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_SchedulerTriggerJob_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.SchedulerTriggerJob(context.Background(), &service.TriggerSchedulerJobRequest{JobID: 1})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_SchedulerPauseJob_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.SchedulerPauseJob(context.Background(), &service.PauseSchedulerJobRequest{JobID: 1})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_SchedulerResumeJob_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.SchedulerResumeJob(context.Background(), &service.ResumeSchedulerJobRequest{JobID: 1})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_SchedulerCancelJob_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.SchedulerCancelJob(context.Background(), &service.CancelSchedulerJobRequest{JobID: 1})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_SchedulerGetJobHistory_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.SchedulerGetJobHistory(context.Background(), &service.GetSchedulerJobHistoryRequest{JobID: 1})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

// =============================================================================
// Additional Secrets Method Tests for Coverage
// =============================================================================

func TestClient_RollbackSecret_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.RollbackSecret(context.Background(), &service.RollbackSecretRequest{SecretID: "test"})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_RotateSecret_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.RotateSecret(context.Background(), &service.RotateSecretRequest{SecretID: "test"})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_UpdateSecretStatus_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.UpdateSecretStatus(context.Background(), &service.UpdateSecretStatusRequest{SecretID: "test"})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestClient_GetSecretsByPrefix_ClosedConnection(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.GetSecretsByPrefix(context.Background(), &service.GetSecretsByPrefixRequest{Prefix: "test"})
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}
