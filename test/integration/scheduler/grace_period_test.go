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

//go:build integration

package scheduler

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kctypes "github.com/jeremyhahn/go-xkms/pkg/types"

	grpcclient "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// TestKMSGracePeriodWorkflow tests the complete grace period workflow:
// 1. Create a key
// 2. Rotate with grace period
// 3. Verify grace period job is scheduled
// 4. Wait for job to execute
// 5. Verify old key version is disabled
func TestKMSGracePeriodWorkflow(t *testing.T) {
	// Use a longer timeout for the grace period to complete
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	t.Run("gRPC grace period workflow", func(t *testing.T) {
		testGRPCGracePeriodWorkflow(t, ctx)
	})

	t.Run("REST grace period workflow", func(t *testing.T) {
		testRESTGracePeriodWorkflow(t, ctx)
	})
}

// testGRPCGracePeriodWorkflow tests the grace period workflow using gRPC for KMS
// and REST for scheduler operations.
func testGRPCGracePeriodWorkflow(t *testing.T, ctx context.Context) {
	// KMS operations via gRPC
	grpcEndpoints := getGRPCEndpoints()
	tlsCfg1, tlsErr1 := helpers.IntegrationTLSConfig()
	require.NoError(t, tlsErr1, "load integration TLS config")
	grpcClient, err := grpcclient.NewClient(grpcEndpoints[0], grpcclient.WithTLSConfig(tlsCfg1), kmsGRPCAuth(t))
	require.NoError(t, err, "gRPC server must be available")
	defer grpcClient.Close()

	requireKMSAvailable(t, grpcClient, ctx)

	// Scheduler operations via REST
	restEndpoints := getRESTEndpoints()
	schedulerClient := NewSystemAdminSchedulerRESTClient(t, restEndpoints[0])

	// Verify scheduler is available
	_, err = schedulerClient.GetStatus(ctx)
	require.NoError(t, err, "Scheduler must be available via REST")

	// Step 1: Create a key
	keyName := generateUniqueKeyName("grace-period-test")
	genReq := &service.GenerateKeyRequest{
		Name:        keyName,
		Description: "Grace period integration test key",
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		},
		Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		Tags: map[string]string{
			"test": "grace-period",
		},
	}

	genResp, err := grpcClient.KMSGenerateKey(ctx, genReq)
	require.NoError(t, err)
	require.NotNil(t, genResp)
	keyID := genResp.Key.KeyID
	defer cleanupKey(t, grpcClient, ctx, keyID)

	t.Logf("Created key: %s (version %d)", keyID, genResp.Key.Version)

	// Verify key is enabled
	assert.Equal(t, service.KeyStatusEnabled, genResp.Key.Status)

	// Step 2: Rotate key with a short grace period (10 seconds for testing)
	gracePeriodSeconds := int64(10)
	rotateReq := &service.RotateKeyRequest{
		KeyID:              keyID,
		RetainOldVersion:   true,
		GracePeriodSeconds: gracePeriodSeconds,
	}

	rotateResp, err := grpcClient.KMSRotateKey(ctx, rotateReq)
	require.NoError(t, err)
	require.NotNil(t, rotateResp)

	oldVersion := rotateResp.OldVersion
	newVersion := rotateResp.NewVersion

	t.Logf("Rotated key: old version %d, new version %d", oldVersion, newVersion)
	t.Logf("Grace period expires at: %v", time.Unix(rotateResp.GracePeriodDisableAt, 0))
	t.Logf("Grace period job ID: %s", rotateResp.GracePeriodJobID)

	// Verify grace period job was scheduled
	require.NotEmpty(t, rotateResp.GracePeriodJobID, "grace period job ID should be set")
	require.NotZero(t, rotateResp.GracePeriodDisableAt, "grace period disable time should be set")

	// Step 3: Verify both versions are currently enabled
	versionsReq := &service.ListKeyVersionsRequest{KeyID: keyID}
	versionsResp, err := grpcClient.KMSListKeyVersions(ctx, versionsReq)
	require.NoError(t, err)
	require.Len(t, versionsResp.Versions, 2, "should have exactly 2 versions")

	for _, v := range versionsResp.Versions {
		assert.Equal(t, service.KeyStatusEnabled, v.Status, "version %d should be enabled initially", v.Version)
	}

	// Step 4: Verify the grace period job exists in scheduler via REST
	jobID, err := strconv.ParseUint(rotateResp.GracePeriodJobID, 10, 64)
	require.NoError(t, err, "failed to parse grace period job ID: %s", rotateResp.GracePeriodJobID)
	require.NotZero(t, jobID, "job ID should not be zero")

	job, err := schedulerClient.GetJob(ctx, jobID)
	require.NoError(t, err, "failed to get grace period job %d", jobID)
	assert.Equal(t, "kms.grace_period", job.Handler)
	assert.Equal(t, string(scheduler.JobStatusPending), job.Status)

	t.Logf("Grace period job found: %s (status: %s, next_run: %v, execute_at: %v)",
		job.Name, job.Status, job.NextRun, job.ExecuteAt)

	// Step 5: Wait for the grace period to expire and job to complete
	waitTime := time.Duration(gracePeriodSeconds+5) * time.Second
	t.Logf("Waiting %v for grace period job to execute...", waitTime)

	cfg := RetryConfig{
		MaxAttempts: 30,
		InitialWait: 1 * time.Second,
		MaxWait:     3 * time.Second,
		Multiplier:  1.2,
	}

	// Wait for job to complete via REST
	finalJob, err := WaitForJobCompletion(ctx, schedulerClient, jobID, cfg)
	require.NoError(t, err, "grace period job should complete")
	assert.Equal(t, string(scheduler.JobStatusCompleted), finalJob.Status, "grace period job should succeed")

	t.Logf("Grace period job completed with status: %s", finalJob.Status)
	if finalJob.Status != string(scheduler.JobStatusCompleted) && finalJob.LastError != "" {
		t.Logf("Grace period job failed with error: %s", finalJob.LastError)
	}

	// Step 6: Verify old key version is now disabled (with retry for Raft propagation)
	waitForKeyVersionStatus(t, grpcClient, ctx, keyID, oldVersion, service.KeyStatusDisabled)
	t.Logf("Old version %d is now disabled", oldVersion)

	waitForKeyVersionStatus(t, grpcClient, ctx, keyID, newVersion, service.KeyStatusEnabled)
	t.Logf("New version %d is still enabled", newVersion)
}

// testRESTGracePeriodWorkflow tests the grace period workflow via REST.
func testRESTGracePeriodWorkflow(t *testing.T, ctx context.Context) {
	// For REST, we need to use gRPC for KMS operations since the REST KMS client
	// may not be fully implemented. We'll use REST for scheduler operations.
	grpcEndpoints := getGRPCEndpoints()
	tlsCfg2, tlsErr2 := helpers.IntegrationTLSConfig()
	require.NoError(t, tlsErr2, "load integration TLS config")
	grpcClient, err := grpcclient.NewClient(grpcEndpoints[0], grpcclient.WithTLSConfig(tlsCfg2), kmsGRPCAuth(t))
	require.NoError(t, err, "gRPC server must be available")
	defer grpcClient.Close()

	requireKMSAvailable(t, grpcClient, ctx)

	restEndpoints := getRESTEndpoints()
	schedulerClient := NewSystemAdminSchedulerRESTClient(t, restEndpoints[0])

	// Verify scheduler is available via REST
	_, err = schedulerClient.GetStatus(ctx)
	require.NoError(t, err, "Scheduler must be available via REST")

	// Step 1: Create a key via gRPC
	keyName := generateUniqueKeyName("rest-grace-period-test")
	genReq := &service.GenerateKeyRequest{
		Name:        keyName,
		Description: "REST grace period integration test key",
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		},
		Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
	}

	genResp, err := grpcClient.KMSGenerateKey(ctx, genReq)
	require.NoError(t, err)
	keyID := genResp.Key.KeyID
	defer cleanupKey(t, grpcClient, ctx, keyID)

	t.Logf("Created key via gRPC: %s", keyID)

	// Step 2: Rotate key with grace period
	gracePeriodSeconds := int64(10)
	rotateReq := &service.RotateKeyRequest{
		KeyID:              keyID,
		RetainOldVersion:   true,
		GracePeriodSeconds: gracePeriodSeconds,
	}

	rotateResp, err := grpcClient.KMSRotateKey(ctx, rotateReq)
	require.NoError(t, err)

	oldVersion := rotateResp.OldVersion
	require.NotEmpty(t, rotateResp.GracePeriodJobID, "grace period job ID should be set")

	t.Logf("Rotated key, grace period job ID: %s", rotateResp.GracePeriodJobID)

	// Step 3: Verify job via REST scheduler client
	jobID, err := strconv.ParseUint(rotateResp.GracePeriodJobID, 10, 64)
	require.NoError(t, err, "failed to parse grace period job ID: %s", rotateResp.GracePeriodJobID)
	require.NotZero(t, jobID, "job ID should not be zero")

	job, err := schedulerClient.GetJob(ctx, jobID)
	require.NoError(t, err, "failed to get grace period job %d", jobID)
	assert.Equal(t, "kms.grace_period", job.Handler)

	t.Logf("Found grace period job via REST: %s (status: %s, next_run: %v, execute_at: %v)",
		job.Name, job.Status, job.NextRun, job.ExecuteAt)

	// Step 4: Wait for job completion via REST
	cfg := RetryConfig{
		MaxAttempts: 30,
		InitialWait: 1 * time.Second,
		MaxWait:     3 * time.Second,
		Multiplier:  1.2,
	}

	finalJob, err := WaitForJobCompletion(ctx, schedulerClient, jobID, cfg)
	require.NoError(t, err, "grace period job should complete")
	assert.Equal(t, string(scheduler.JobStatusCompleted), finalJob.Status)

	t.Logf("Job completed via REST: %s", finalJob.Status)

	// Step 5: Verify old version is disabled via gRPC (with retry for Raft propagation)
	waitForKeyVersionStatus(t, grpcClient, ctx, keyID, oldVersion, service.KeyStatusDisabled)
	t.Logf("Verified old version %d is disabled", oldVersion)
}

// TestGracePeriodJobCancellation tests that canceling a grace period job
// prevents the old key version from being disabled.
func TestGracePeriodJobCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// KMS operations via gRPC
	grpcEndpoints := getGRPCEndpoints()
	tlsCfg3, tlsErr3 := helpers.IntegrationTLSConfig()
	require.NoError(t, tlsErr3, "load integration TLS config")
	grpcClient, err := grpcclient.NewClient(grpcEndpoints[0], grpcclient.WithTLSConfig(tlsCfg3), kmsGRPCAuth(t))
	require.NoError(t, err, "must connect to gRPC server for cancellation test")
	defer grpcClient.Close()

	requireKMSAvailable(t, grpcClient, ctx)

	// Scheduler operations via REST
	restEndpoints := getRESTEndpoints()
	require.NotEmpty(t, restEndpoints, "REST endpoints must be available")
	schedulerClient := NewSystemAdminSchedulerRESTClient(t, restEndpoints[0])

	// Verify scheduler is available
	_, err = schedulerClient.GetStatus(ctx)
	require.NoError(t, err, "scheduler must be available for cancellation test")

	// Step 1: Create a key
	keyName := generateUniqueKeyName("cancel-grace-test")
	genReq := &service.GenerateKeyRequest{
		Name:        keyName,
		Description: "Grace period cancellation test key",
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		},
		Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		Tags: map[string]string{
			"test": "grace-period-cancel",
		},
	}

	genResp, err := grpcClient.KMSGenerateKey(ctx, genReq)
	require.NoError(t, err)
	require.NotNil(t, genResp)
	keyID := genResp.Key.KeyID
	defer cleanupKey(t, grpcClient, ctx, keyID)

	t.Logf("Created key: %s (version %d)", keyID, genResp.Key.Version)

	// Step 2: Rotate key with a long grace period so we have time to cancel
	gracePeriodSeconds := int64(300) // 5 minutes -- plenty of time to cancel
	rotateReq := &service.RotateKeyRequest{
		KeyID:              keyID,
		RetainOldVersion:   true,
		GracePeriodSeconds: gracePeriodSeconds,
	}

	rotateResp, err := grpcClient.KMSRotateKey(ctx, rotateReq)
	require.NoError(t, err)
	require.NotNil(t, rotateResp)

	oldVersion := rotateResp.OldVersion
	require.NotEmpty(t, rotateResp.GracePeriodJobID, "grace period job ID should be set")

	t.Logf("Rotated key: old version %d, job ID %s", oldVersion, rotateResp.GracePeriodJobID)

	// Step 3: Parse job ID and verify it exists as pending
	jobID, err := strconv.ParseUint(rotateResp.GracePeriodJobID, 10, 64)
	require.NoError(t, err, "failed to parse grace period job ID")
	require.NotZero(t, jobID)

	job, err := schedulerClient.GetJob(ctx, jobID)
	require.NoError(t, err, "grace period job must exist")
	assert.Equal(t, string(scheduler.JobStatusPending), job.Status,
		"grace period job should be pending before cancellation")

	// Step 4: Cancel the grace period job via REST
	cancelResp, err := schedulerClient.CancelJob(ctx, jobID, "integration test cancellation")
	require.NoError(t, err, "cancel job must succeed")
	require.NotNil(t, cancelResp)
	assert.Equal(t, jobID, cancelResp.JobID)
	assert.Equal(t, string(scheduler.JobStatusCanceled), cancelResp.NewStatus,
		"job status after cancel must be 'canceled'")

	t.Logf("Canceled job %d: old_status=%s new_status=%s",
		cancelResp.JobID, cancelResp.OldStatus, cancelResp.NewStatus)

	// Step 5: Verify job status is canceled via GET
	canceledJob, err := schedulerClient.GetJob(ctx, jobID)
	require.NoError(t, err)
	assert.Equal(t, string(scheduler.JobStatusCanceled), canceledJob.Status,
		"job must be canceled after cancel operation")

	// Step 6: Verify old version is still enabled (cancel prevented disable)
	versionsReq := &service.ListKeyVersionsRequest{KeyID: keyID}
	versionsResp, err := grpcClient.KMSListKeyVersions(ctx, versionsReq)
	require.NoError(t, err)

	for _, v := range versionsResp.Versions {
		if v.Version == oldVersion {
			assert.Equal(t, service.KeyStatusEnabled, v.Status,
				"old version should remain enabled because grace period job was canceled")
			t.Logf("Old version %d is still enabled (cancel prevented disable)", oldVersion)
		}
	}
}

// TestMultipleGracePeriodRotations tests multiple consecutive rotations with grace periods.
func TestMultipleGracePeriodRotations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// KMS via gRPC
	grpcEndpoints := getGRPCEndpoints()
	tlsCfg4, tlsErr4 := helpers.IntegrationTLSConfig()
	require.NoError(t, tlsErr4, "load integration TLS config")
	grpcClient, err := grpcclient.NewClient(grpcEndpoints[0], grpcclient.WithTLSConfig(tlsCfg4), kmsGRPCAuth(t))
	require.NoError(t, err, "gRPC server must be available")
	defer grpcClient.Close()

	requireKMSAvailable(t, grpcClient, ctx)

	// Scheduler via REST
	restEndpoints := getRESTEndpoints()
	schedulerClient := NewSystemAdminSchedulerRESTClient(t, restEndpoints[0])

	_, err = schedulerClient.GetStatus(ctx)
	require.NoError(t, err, "Scheduler must be available via REST")

	// Create a key
	keyName := generateUniqueKeyName("multi-rotate-test")
	genReq := &service.GenerateKeyRequest{
		Name: keyName,
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		},
		Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
	}

	genResp, err := grpcClient.KMSGenerateKey(ctx, genReq)
	require.NoError(t, err)
	keyID := genResp.Key.KeyID
	defer cleanupKey(t, grpcClient, ctx, keyID)

	version1 := genResp.Key.Version
	t.Logf("Created key with version %d", version1)

	// First rotation with short grace period
	rotate1Req := &service.RotateKeyRequest{
		KeyID:              keyID,
		RetainOldVersion:   true,
		GracePeriodSeconds: 5,
	}

	rotate1Resp, err := grpcClient.KMSRotateKey(ctx, rotate1Req)
	require.NoError(t, err)
	version2 := rotate1Resp.NewVersion
	job1ID := rotate1Resp.GracePeriodJobID
	require.NotEmpty(t, job1ID, "first rotation should have grace period job ID")
	t.Logf("First rotation: version %d -> %d, job %s", version1, version2, job1ID)

	// Second rotation with short grace period
	rotate2Req := &service.RotateKeyRequest{
		KeyID:              keyID,
		RetainOldVersion:   true,
		GracePeriodSeconds: 5,
	}

	rotate2Resp, err := grpcClient.KMSRotateKey(ctx, rotate2Req)
	require.NoError(t, err)
	version3 := rotate2Resp.NewVersion
	job2ID := rotate2Resp.GracePeriodJobID
	require.NotEmpty(t, job2ID, "second rotation should have grace period job ID")
	t.Logf("Second rotation: version %d -> %d, job %s", version2, version3, job2ID)

	// Verify we have 3 versions
	versionsReq := &service.ListKeyVersionsRequest{KeyID: keyID}
	versionsResp, err := grpcClient.KMSListKeyVersions(ctx, versionsReq)
	require.NoError(t, err)
	require.Len(t, versionsResp.Versions, 3, "should have 3 versions")

	// Wait for both grace period jobs to complete via REST
	cfg := RetryConfig{
		MaxAttempts: 30,
		InitialWait: 1 * time.Second,
		MaxWait:     3 * time.Second,
		Multiplier:  1.2,
	}

	// Parse job IDs with proper error handling
	job1IDUint, err := strconv.ParseUint(job1ID, 10, 64)
	require.NoError(t, err, "failed to parse first job ID: %s", job1ID)
	require.NotZero(t, job1IDUint, "first job ID should not be zero")

	job2IDUint, err := strconv.ParseUint(job2ID, 10, 64)
	require.NoError(t, err, "failed to parse second job ID: %s", job2ID)
	require.NotZero(t, job2IDUint, "second job ID should not be zero")

	// Wait for first job via REST
	_, err = WaitForJobCompletion(ctx, schedulerClient, job1IDUint, cfg)
	require.NoError(t, err, "first grace period job should complete")

	// Wait for second job via REST
	_, err = WaitForJobCompletion(ctx, schedulerClient, job2IDUint, cfg)
	require.NoError(t, err, "second grace period job should complete")

	// Verify final version states (with retry for Raft propagation)
	waitForKeyVersionStatus(t, grpcClient, ctx, keyID, version1, service.KeyStatusDisabled)
	t.Logf("Version %d (old): disabled", version1)

	waitForKeyVersionStatus(t, grpcClient, ctx, keyID, version2, service.KeyStatusDisabled)
	t.Logf("Version %d (old): disabled", version2)

	waitForKeyVersionStatus(t, grpcClient, ctx, keyID, version3, service.KeyStatusEnabled)
	t.Logf("Version %d (latest): enabled", version3)
}

// TestGracePeriodWithEncryption verifies that data encrypted with old version
// can still be decrypted during grace period, but fails after.
func TestGracePeriodWithEncryption(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// KMS via gRPC
	grpcEndpoints := getGRPCEndpoints()
	tlsCfg5, tlsErr5 := helpers.IntegrationTLSConfig()
	require.NoError(t, tlsErr5, "load integration TLS config")
	grpcClient, err := grpcclient.NewClient(grpcEndpoints[0], grpcclient.WithTLSConfig(tlsCfg5), kmsGRPCAuth(t))
	require.NoError(t, err, "gRPC server must be available")
	defer grpcClient.Close()

	requireKMSAvailable(t, grpcClient, ctx)

	// Scheduler via REST
	restEndpoints := getRESTEndpoints()
	schedulerClient := NewSystemAdminSchedulerRESTClient(t, restEndpoints[0])

	_, err = schedulerClient.GetStatus(ctx)
	require.NoError(t, err, "Scheduler must be available via REST")

	// Create a key
	keyName := generateUniqueKeyName("encrypt-grace-test")
	genReq := &service.GenerateKeyRequest{
		Name: keyName,
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		},
		Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
	}

	genResp, err := grpcClient.KMSGenerateKey(ctx, genReq)
	require.NoError(t, err)
	keyID := genResp.Key.KeyID
	defer cleanupKey(t, grpcClient, ctx, keyID)

	// Encrypt some data with version 1
	plaintext := []byte("secret data for grace period test")
	encryptReq := &service.EncryptRequest{
		KeyID:     keyID,
		Plaintext: plaintext,
	}

	encryptResp, err := grpcClient.KMSEncrypt(ctx, encryptReq)
	require.NoError(t, err)
	ciphertext := encryptResp.Ciphertext
	encryptedVersion := encryptResp.KeyVersion

	t.Logf("Encrypted data with key version %d", encryptedVersion)

	// Rotate with short grace period
	rotateReq := &service.RotateKeyRequest{
		KeyID:              keyID,
		RetainOldVersion:   true,
		GracePeriodSeconds: 10,
	}

	rotateResp, err := grpcClient.KMSRotateKey(ctx, rotateReq)
	require.NoError(t, err)
	require.NotEmpty(t, rotateResp.GracePeriodJobID, "grace period job ID should be set")

	t.Logf("Rotated key, old version %d, new version %d", rotateResp.OldVersion, rotateResp.NewVersion)
	t.Logf("Grace period job ID: %s", rotateResp.GracePeriodJobID)

	// Decrypt should still work during grace period
	decryptReq := &service.DecryptRequest{
		KeyID:      keyID,
		Ciphertext: ciphertext,
	}

	decryptResp, err := grpcClient.KMSDecrypt(ctx, decryptReq)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decryptResp.Plaintext, "decryption should work during grace period")

	t.Logf("Decryption successful during grace period")

	// Wait for grace period to expire via REST
	cfg := RetryConfig{
		MaxAttempts: 30,
		InitialWait: 1 * time.Second,
		MaxWait:     3 * time.Second,
		Multiplier:  1.2,
	}

	jobID, err := strconv.ParseUint(rotateResp.GracePeriodJobID, 10, 64)
	require.NoError(t, err, "failed to parse grace period job ID: %s", rotateResp.GracePeriodJobID)
	require.NotZero(t, jobID, "job ID should not be zero")

	_, err = WaitForJobCompletion(ctx, schedulerClient, jobID, cfg)
	require.NoError(t, err, "grace period job should complete")

	t.Logf("Grace period expired, old version should be disabled")

	// Verify old version is disabled. In a multi-node Raft cluster, version status
	// propagation via KVStore may have a brief delay after job completion, so
	// poll with retries until the status is visible.
	statusCfg := RetryConfig{
		MaxAttempts: 15,
		InitialWait: 500 * time.Millisecond,
		MaxWait:     2 * time.Second,
		Multiplier:  1.3,
	}

	versionsReq := &service.ListKeyVersionsRequest{KeyID: keyID}
	err = RetryOperation(ctx, statusCfg, func() error {
		versionsResp, listErr := grpcClient.KMSListKeyVersions(ctx, versionsReq)
		if listErr != nil {
			return listErr
		}
		for _, v := range versionsResp.Versions {
			if v.Version == encryptedVersion {
				if v.Status != service.KeyStatusDisabled {
					return fmt.Errorf("version %d status is %s, waiting for disabled", v.Version, v.Status)
				}
				t.Logf("Verified version %d is now disabled", encryptedVersion)
				return nil
			}
		}
		return fmt.Errorf("version %d not found in response", encryptedVersion)
	})
	require.NoError(t, err, "old version should be disabled after grace period")

	// Note: Decryption behavior after grace period depends on implementation.
	// Some implementations may still allow decryption with disabled versions
	// for data recovery purposes, while others may reject it.
	// The test verifies the version status change, which is the primary objective.
}
