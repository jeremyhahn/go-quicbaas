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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// createClosedClient creates a Client in the closed state so that sendRequest
// returns a ClientClosedError. This lets us test the encode + sendRequest
// error paths for all client methods.
func createClosedClient(t *testing.T) *Client {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{
		addr:       "test:8443",
		connected:  false,
		closed:     true,
		ctx:        ctx,
		cancelFunc: cancel,
		logger:     createClientTestLogger(),
	}
}

// TestClient_ClosedClient_DataOperations verifies that all data operation
// methods return an error when called on a closed client with valid requests.
func TestClient_ClosedClient_DataOperations(t *testing.T) {
	client := createClosedClient(t)
	defer client.Close()
	ctx := context.Background()

	t.Run("Put", func(t *testing.T) {
		_, err := client.Put(ctx, &service.PutRequest{EntityType: "test", ID: 1, Data: []byte("data")})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("Get", func(t *testing.T) {
		_, err := client.Get(ctx, &service.GetRequest{EntityType: "test", ID: 1})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("Delete", func(t *testing.T) {
		_, err := client.Delete(ctx, &service.DeleteRequest{EntityType: "test", ID: 1})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("List", func(t *testing.T) {
		_, err := client.List(ctx, &service.ListRequest{EntityType: "test"})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})
}

// TestClient_ClosedClient_KMSOperations verifies KMS methods on a closed client.
func TestClient_ClosedClient_KMSOperations(t *testing.T) {
	client := createClosedClient(t)
	defer client.Close()
	ctx := context.Background()

	t.Run("KMSGenerateKey", func(t *testing.T) {
		_, err := client.KMSGenerateKey(ctx, &service.GenerateKeyRequest{Name: "test-key"})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("KMSImportKey", func(t *testing.T) {
		_, err := client.KMSImportKey(ctx, &service.ImportKeyRequest{Name: "key1"})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("KMSGetKey", func(t *testing.T) {
		_, err := client.KMSGetKey(ctx, &service.GetKeyRequest{KeyID: "key1"})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("KMSListKeys", func(t *testing.T) {
		_, err := client.KMSListKeys(ctx, &service.ListKeysRequest{})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("KMSRotateKey", func(t *testing.T) {
		_, err := client.KMSRotateKey(ctx, &service.RotateKeyRequest{KeyID: "key1"})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("KMSDeleteKey", func(t *testing.T) {
		_, err := client.KMSDeleteKey(ctx, &service.DeleteKeyRequest{KeyID: "key1"})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("KMSEncrypt", func(t *testing.T) {
		_, err := client.KMSEncrypt(ctx, &service.EncryptRequest{KeyID: "key1", Plaintext: []byte("data")})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("KMSDecrypt", func(t *testing.T) {
		_, err := client.KMSDecrypt(ctx, &service.DecryptRequest{KeyID: "key1", Ciphertext: []byte("data")})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("KMSListKeyVersions", func(t *testing.T) {
		_, err := client.KMSListKeyVersions(ctx, &service.ListKeyVersionsRequest{KeyID: "key1"})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("KMSUpdateKeyStatus", func(t *testing.T) {
		_, err := client.KMSUpdateKeyStatus(ctx, &service.UpdateKeyStatusRequest{KeyID: "key1"})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("KMSGenerateDataKey", func(t *testing.T) {
		_, err := client.KMSGenerateDataKey(ctx, &service.GenerateDataKeyRequest{KeyID: "key1"})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("KMSExportKey", func(t *testing.T) {
		_, err := client.KMSExportKey(ctx, &service.ExportKeyRequest{KeyID: "key1"})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("KMSUnwrapKey", func(t *testing.T) {
		_, err := client.KMSUnwrapKey(ctx, &service.UnwrapKeyRequest{WrappingKeyID: "key1"})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("KMSSetKeySafety", func(t *testing.T) {
		_, err := client.KMSSetKeySafety(ctx, &service.SetKeySafetyRequest{KeyID: "key1"})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("KMSClearKeySafety", func(t *testing.T) {
		_, err := client.KMSClearKeySafety(ctx, &service.ClearKeySafetyRequest{KeyID: "key1"})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("KMSSafetyCheck", func(t *testing.T) {
		_, err := client.KMSSafetyCheck(ctx, &service.SafetyCheckRequest{})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("KMSSafetyReport", func(t *testing.T) {
		_, err := client.KMSSafetyReport(ctx, &service.SafetyReportRequest{})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})
}

// TestClient_ClosedClient_DEKOperations verifies DEK methods on a closed client.
func TestClient_ClosedClient_DEKOperations(t *testing.T) {
	client := createClosedClient(t)
	defer client.Close()
	ctx := context.Background()

	t.Run("KMSCreateDEK", func(t *testing.T) {
		_, err := client.KMSCreateDEK(ctx, &service.CreateDEKRequest{Path: "dek/test"})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("KMSGetDEK", func(t *testing.T) {
		_, err := client.KMSGetDEK(ctx, &service.GetDEKRequest{Path: "dek/test"})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("KMSListDEKs", func(t *testing.T) {
		_, err := client.KMSListDEKs(ctx, &service.ListDEKsRequest{})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("KMSRotateDEK", func(t *testing.T) {
		_, err := client.KMSRotateDEK(ctx, &service.RotateDEKRequest{Path: "dek/test"})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("KMSDeleteDEK", func(t *testing.T) {
		_, err := client.KMSDeleteDEK(ctx, &service.DeleteDEKRequest{Path: "dek/test"})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("KMSGetDEKStatus", func(t *testing.T) {
		_, err := client.KMSGetDEKStatus(ctx, &service.GetDEKStatusRequest{Path: "dek/test"})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("KMSSetDefaultDEK", func(t *testing.T) {
		_, err := client.KMSSetDefaultDEK(ctx, &service.SetDefaultDEKRequest{Path: "dek/test"})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("KMSSetDEKSafety", func(t *testing.T) {
		_, err := client.KMSSetDEKSafety(ctx, &service.SetDEKSafetyRequest{Path: "dek/test"})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("KMSClearDEKSafety", func(t *testing.T) {
		_, err := client.KMSClearDEKSafety(ctx, &service.ClearDEKSafetyRequest{Path: "dek/test"})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})
}

// TestClient_ClosedClient_BackendOperations verifies backend status methods on a closed client.
func TestClient_ClosedClient_BackendOperations(t *testing.T) {
	client := createClosedClient(t)
	defer client.Close()
	ctx := context.Background()

	t.Run("KMSBackendStatus", func(t *testing.T) {
		_, err := client.KMSBackendStatus(ctx)
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("KMSBackendTest", func(t *testing.T) {
		_, err := client.KMSBackendTest(ctx)
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})
}

// TestClient_ClosedClient_SealOperations verifies seal methods on a closed client.
func TestClient_ClosedClient_SealOperations(t *testing.T) {
	client := createClosedClient(t)
	defer client.Close()
	ctx := context.Background()

	t.Run("SealStatus", func(t *testing.T) {
		_, err := client.SealStatus(ctx)
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("SealInit", func(t *testing.T) {
		_, err := client.SealInit(ctx, &service.SealInitRequest{Threshold: 3, TotalShares: 5})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("Unseal", func(t *testing.T) {
		_, err := client.Unseal(ctx, &service.UnsealRequest{Share: []byte("share1")})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("Seal", func(t *testing.T) {
		err := client.Seal(ctx)
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("Rekey", func(t *testing.T) {
		_, err := client.Rekey(ctx, &service.RekeyRequest{NewThreshold: 3, NewTotalShares: 5})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})
}

// TestClient_ClosedClient_SecretsOperations verifies secrets methods on a closed client.
func TestClient_ClosedClient_SecretsOperations(t *testing.T) {
	client := createClosedClient(t)
	defer client.Close()
	ctx := context.Background()

	t.Run("StoreSecret", func(t *testing.T) {
		_, err := client.StoreSecret(ctx, &service.StoreSecretRequest{Name: "test-secret", Data: []byte("val")})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("GetSecret", func(t *testing.T) {
		_, err := client.GetSecret(ctx, &service.GetSecretRequest{SecretID: "secret1"})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("ListSecrets", func(t *testing.T) {
		_, err := client.ListSecrets(ctx, &service.ListSecretsRequest{Prefix: "secret/"})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("DeleteSecret", func(t *testing.T) {
		_, err := client.DeleteSecret(ctx, &service.DeleteSecretRequest{SecretID: "secret1"})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("ListSecretVersions", func(t *testing.T) {
		_, err := client.ListSecretVersions(ctx, &service.ListSecretVersionsRequest{SecretID: "secret1"})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("RollbackSecret", func(t *testing.T) {
		_, err := client.RollbackSecret(ctx, &service.RollbackSecretRequest{SecretID: "secret1", TargetVersion: 1})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("RotateSecret", func(t *testing.T) {
		_, err := client.RotateSecret(ctx, &service.RotateSecretRequest{SecretID: "secret1", NewData: []byte("new")})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("UpdateSecretStatus", func(t *testing.T) {
		_, err := client.UpdateSecretStatus(ctx, &service.UpdateSecretStatusRequest{SecretID: "secret1", Status: "active"})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("GetSecretsByPrefix", func(t *testing.T) {
		_, err := client.GetSecretsByPrefix(ctx, &service.GetSecretsByPrefixRequest{Prefix: "secret/"})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})
}

// TestClient_ClosedClient_ClusterOperations verifies cluster methods on a closed client.
func TestClient_ClosedClient_ClusterOperations(t *testing.T) {
	client := createClosedClient(t)
	defer client.Close()
	ctx := context.Background()

	t.Run("ClusterStatus", func(t *testing.T) {
		_, err := client.ClusterStatus(ctx, &service.ClusterStatusRequest{})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("GetLeader", func(t *testing.T) {
		_, err := client.GetLeader(ctx, &service.GetLeaderRequest{ShardID: 1})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("AddNode", func(t *testing.T) {
		_, err := client.AddNode(ctx, &service.AddNodeRequest{NodeID: 1, ShardID: 1})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("RemoveNode", func(t *testing.T) {
		_, err := client.RemoveNode(ctx, &service.RemoveNodeRequest{NodeID: 1, ShardID: 1})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("CreateShard", func(t *testing.T) {
		_, err := client.CreateShard(ctx, &service.CreateShardRequest{ShardID: 1})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("RebalanceCluster", func(t *testing.T) {
		_, err := client.RebalanceCluster(ctx, &service.RebalanceClusterRequest{})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})
}

// TestClient_ClosedClient_SchedulerOperations verifies scheduler methods on a closed client.
func TestClient_ClosedClient_SchedulerOperations(t *testing.T) {
	client := createClosedClient(t)
	defer client.Close()
	ctx := context.Background()

	t.Run("SchedulerCreateJob", func(t *testing.T) {
		_, err := client.SchedulerCreateJob(ctx, &service.CreateSchedulerJobRequest{Name: "job1"})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("SchedulerGetJob", func(t *testing.T) {
		_, err := client.SchedulerGetJob(ctx, &service.GetSchedulerJobRequest{JobID: 1})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("SchedulerUpdateJob", func(t *testing.T) {
		_, err := client.SchedulerUpdateJob(ctx, &service.UpdateSchedulerJobRequest{JobID: 1})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("SchedulerDeleteJob", func(t *testing.T) {
		_, err := client.SchedulerDeleteJob(ctx, &service.DeleteSchedulerJobRequest{JobID: 1})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("SchedulerListJobs", func(t *testing.T) {
		_, err := client.SchedulerListJobs(ctx, &service.ListSchedulerJobsRequest{})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("SchedulerTriggerJob", func(t *testing.T) {
		_, err := client.SchedulerTriggerJob(ctx, &service.TriggerSchedulerJobRequest{JobID: 1})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("SchedulerPauseJob", func(t *testing.T) {
		_, err := client.SchedulerPauseJob(ctx, &service.PauseSchedulerJobRequest{JobID: 1})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("SchedulerResumeJob", func(t *testing.T) {
		_, err := client.SchedulerResumeJob(ctx, &service.ResumeSchedulerJobRequest{JobID: 1})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("SchedulerCancelJob", func(t *testing.T) {
		_, err := client.SchedulerCancelJob(ctx, &service.CancelSchedulerJobRequest{JobID: 1})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("SchedulerGetJobHistory", func(t *testing.T) {
		_, err := client.SchedulerGetJobHistory(ctx, &service.GetSchedulerJobHistoryRequest{JobID: 1})
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})

	t.Run("SchedulerGetStatus", func(t *testing.T) {
		_, err := client.SchedulerGetStatus(ctx)
		if err == nil {
			t.Fatal("expected error from closed client")
		}
	})
}

// TestClient_SendRequest_ClosedClient verifies sendRequest returns a
// ClientClosedError when the client is closed.
func TestClient_SendRequest_ClosedClient(t *testing.T) {
	client := createClosedClient(t)
	defer client.Close()

	_, err := client.sendRequest(context.Background(), OpGet, []byte("{}"))
	if err == nil {
		t.Fatal("expected error from closed client sendRequest")
	}
}
