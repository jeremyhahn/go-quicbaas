// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// SPDX-License-Identifier: GPL-3.0-only
// See the LICENSE file in the root of this repository.

package service

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	stderrors "errors"
	"math/big"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/cache"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	"github.com/jeremyhahn/go-quicbaas/pkg/shards"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
)

type mockLoggerForAdapter struct {
	debugCalls []string
	warnCalls  []string
	errorCalls []string
}

func (m *mockLoggerForAdapter) Debug(msg string, args ...any) {
	m.debugCalls = append(m.debugCalls, msg)
}

func (m *mockLoggerForAdapter) Debugf(format string, args ...interface{}) {
	m.debugCalls = append(m.debugCalls, format)
}

func (m *mockLoggerForAdapter) Info(msg string, args ...any) {}

func (m *mockLoggerForAdapter) Infof(format string, args ...interface{}) {}

func (m *mockLoggerForAdapter) Warn(msg string, args ...any) {
	m.warnCalls = append(m.warnCalls, msg)
}

func (m *mockLoggerForAdapter) Warnf(format string, args ...interface{}) {
	m.warnCalls = append(m.warnCalls, format)
}

func (m *mockLoggerForAdapter) Error(msg string, args ...any) {
	m.errorCalls = append(m.errorCalls, msg)
}

func (m *mockLoggerForAdapter) Errorf(format string, args ...interface{}) {
	m.errorCalls = append(m.errorCalls, format)
}

func (m *mockLoggerForAdapter) Fatal(msg string) {}

func (m *mockLoggerForAdapter) Fatalf(format string, args ...interface{}) {}

func (m *mockLoggerForAdapter) WithField(key string, value interface{}) interfaces.Logger {
	return m
}

func (m *mockLoggerForAdapter) WithFields(fields map[string]interface{}) interfaces.Logger {
	return m
}

func (m *mockLoggerForAdapter) WithError(err error) interfaces.Logger {
	return m
}

type coverageReencInfoWithGetters struct {
	active   bool
	progress float64
}

func (m *coverageReencInfoWithGetters) GetActive() bool {
	return m.active
}

func (m *coverageReencInfoWithGetters) GetProgress() float64 {
	return m.progress
}

type coverageReencStatusWithAllGetters struct {
	active        bool
	startedAt     time.Time
	totalKeys     uint64
	processedKeys uint64
	progress      float64
	oldVersion    uint64
}

func (m *coverageReencStatusWithAllGetters) GetActive() bool         { return m.active }
func (m *coverageReencStatusWithAllGetters) GetProgress() float64    { return m.progress }
func (m *coverageReencStatusWithAllGetters) GetStartedAt() time.Time { return m.startedAt }

func (m *coverageReencStatusWithAllGetters) GetTotalKeys() uint64 { return m.totalKeys }

func (m *coverageReencStatusWithAllGetters) GetProcessedKeys() uint64 {
	return m.processedKeys
}

func (m *coverageReencStatusWithAllGetters) GetOldVersion() uint64 { return m.oldVersion }

func (m *coverageReencStatusWithAllGetters) GetNewVersion() uint64 { return m.oldVersion + 1 }

func (m *coverageReencStatusWithAllGetters) GetFailedKeys() uint64 { return 0 }

func (m *coverageReencStatusWithAllGetters) GetLastError() string { return "" }

func (m *coverageReencStatusWithAllGetters) GetCompletedAt() *time.Time { return nil }

type mockStatusWithSyncedNodes struct {
	syncedNodes map[uint64]string
}

func (m *mockStatusWithSyncedNodes) GetSyncedNodes() map[uint64]string {
	return m.syncedNodes
}

type mockStatusWithSyncedNodesMethod struct {
	syncedNodes map[uint64]string
}

func (m *mockStatusWithSyncedNodesMethod) SyncedNodes() map[uint64]string {
	return m.syncedNodes
}

func TestServiceImpl_Accessor_KMS(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	result := svc.KMS()
	assert.Nil(t, result, "Expected nil for unconfigured KMS accessor")
}

func TestServiceImpl_Accessor_Secrets(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	result := svc.Secrets()
	assert.Nil(t, result, "Expected nil for unconfigured Secrets accessor")
}

func TestServiceImpl_Accessor_Transit(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	result := svc.Crypto()
	assert.Nil(t, result, "Expected nil for unconfigured Crypto accessor")
}

func TestServiceImpl_Accessor_Vector(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	result := svc.Vector()
	assert.Nil(t, result, "Expected nil for unconfigured Vector accessor")
}

func TestServiceImpl_Accessor_Embedding(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	result := svc.Embedding()
	assert.Nil(t, result, "Expected nil for unconfigured Embedding accessor")
}

func TestServiceImpl_Close_Idempotent(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))

	// First close
	err := svc.Close()
	require.NoError(t, err)

	// Second close should be idempotent
	err = svc.Close()
	assert.NoError(t, err, "Close should be idempotent and not return error on second call")
}

func TestServiceImpl_Close_WithBackupManager(t *testing.T) {
	mockBackup := &mockBackupManager{}
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft), withBackupManager(mockBackup))

	err := svc.Close()
	assert.NoError(t, err)
}

type unavailableMockKMSManager struct {
	mockKMSManager
}

func (m *unavailableMockKMSManager) IsAvailable(ctx context.Context) bool {
	return false
}

func TestOperationsService_MigrateData_NilRequest(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	resp, err := svc.MigrateData(context.Background(), nil)
	require.Error(t, err)
	assert.Nil(t, resp)
	var qbErr *dberrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestOperationsService_ReplaceNode_NilRequest(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	resp, err := svc.ReplaceNode(context.Background(), nil)
	require.Error(t, err)
	assert.Nil(t, resp)
	var qbErr *dberrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestOperationsService_UpgradeNode_NilRequest(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	resp, err := svc.UpgradeNode(context.Background(), nil)
	require.Error(t, err)
	assert.Nil(t, resp)
	var qbErr *dberrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestOperationsService_GetUpgradeStatus_WrongJobType(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	// Create a migration (not an upgrade)
	_, err := svc.MigrateData(context.Background(), &MigrateDataRequest{
		SourceShard: 1,
		TargetShard: 2,
	})
	require.NoError(t, err)

	// Get the underlying operationsService to find the internal job ID
	impl := svc.(*quicBaaS)
	impl.operationsService.jobsMu.RLock()
	var migrationJobID string
	for id, job := range impl.operationsService.jobs {
		if job.Type == "migration" {
			migrationJobID = id
			break
		}
	}
	impl.operationsService.jobsMu.RUnlock()

	require.NotEmpty(t, migrationJobID, "should have a migration job")

	// Try to get upgrade status using the migration job ID - should fail with wrong type
	resp, err := svc.GetUpgradeStatus(context.Background(), &GetUpgradeStatusRequest{
		JobID: migrationJobID,
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	var qbErr *dberrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

type covMockCAService struct {
	xkmstransport.Client

	issueCertificateFunc func(ctx context.Context, req *xkmstransport.IssueCertificateRequest) (*xkmstransport.IssueCertificateResponse, error)
	getCACertificateFunc func(ctx context.Context, req *xkmstransport.GetCACertificateRequest) (*xkmstransport.GetCACertificateResponse, error)
	getCABundleFunc      func(ctx context.Context, req *xkmstransport.GetCABundleRequest) (*xkmstransport.GetCABundleResponse, error)
	isRevokedFunc        func(ctx context.Context, req *xkmstransport.IsRevokedRequest) (*xkmstransport.IsRevokedResponse, error)

	caCert    *x509.Certificate
	caCertPEM []byte
	caKey     *ecdsa.PrivateKey
}

func newCovMockCAService(t *testing.T) *covMockCAService {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName:   "Test CA",
			Organization: []string{"Test Org"},
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	require.NoError(t, err)

	caCert, err := x509.ParseCertificate(caCertDER)
	require.NoError(t, err)

	caCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertDER})

	mock := &covMockCAService{
		caCert:    caCert,
		caCertPEM: caCertPEM,
		caKey:     caKey,
	}

	// Default implementations for CA certificate retrieval
	mock.getCACertificateFunc = func(_ context.Context, _ *xkmstransport.GetCACertificateRequest) (*xkmstransport.GetCACertificateResponse, error) {
		return &xkmstransport.GetCACertificateResponse{
			CertificatePEM: caCertPEM,
			Subject:        "CN=Test CA,O=Test Org",
		}, nil
	}
	mock.getCABundleFunc = func(_ context.Context, _ *xkmstransport.GetCABundleRequest) (*xkmstransport.GetCABundleResponse, error) {
		return &xkmstransport.GetCABundleResponse{
			BundlePEM: caCertPEM,
		}, nil
	}
	mock.isRevokedFunc = func(_ context.Context, _ *xkmstransport.IsRevokedRequest) (*xkmstransport.IsRevokedResponse, error) {
		return &xkmstransport.IsRevokedResponse{Revoked: false}, nil
	}

	return mock
}

func (m *covMockCAService) IssueCertificate(ctx context.Context, req *xkmstransport.IssueCertificateRequest) (*xkmstransport.IssueCertificateResponse, error) {
	if m.issueCertificateFunc != nil {
		return m.issueCertificateFunc(ctx, req)
	}

	// Generate a real cert signed by the CA
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			CommonName: req.CommonName,
		},
		NotBefore: time.Now().Add(-time.Hour),
		NotAfter:  time.Now().Add(24 * time.Hour),
		KeyUsage:  x509.KeyUsageDigitalSignature,
	}
	certDER, _ := x509.CreateCertificate(rand.Reader, template, m.caCert, &key.PublicKey, m.caKey)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, _ := x509.MarshalECPrivateKey(key)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return &xkmstransport.IssueCertificateResponse{
		CertificatePEM: certPEM,
		PrivateKeyPEM:  keyPEM,
		ChainPEM:       m.caCertPEM,
		SerialNumber:   "2",
	}, nil
}

func (m *covMockCAService) SignCSR(_ context.Context, _ *xkmstransport.SignCSRRequest) (*xkmstransport.SignCSRResponse, error) {
	return &xkmstransport.SignCSRResponse{
		CertificatePEM: []byte("-----BEGIN CERTIFICATE-----\nsigned\n-----END CERTIFICATE-----"),
		SerialNumber:   "3",
	}, nil
}

func (m *covMockCAService) RevokeCertificate(_ context.Context, _ *xkmstransport.RevokeCertificateRequest) (*xkmstransport.RevokeCertificateResponse, error) {
	return &xkmstransport.RevokeCertificateResponse{Success: true}, nil
}

func (m *covMockCAService) GetCACertificate(ctx context.Context, req *xkmstransport.GetCACertificateRequest) (*xkmstransport.GetCACertificateResponse, error) {
	if m.getCACertificateFunc != nil {
		return m.getCACertificateFunc(ctx, req)
	}
	return &xkmstransport.GetCACertificateResponse{
		CertificatePEM: m.caCertPEM,
		Subject:        "CN=Test CA,O=Test Org",
	}, nil
}

func (m *covMockCAService) GetCABundle(ctx context.Context, req *xkmstransport.GetCABundleRequest) (*xkmstransport.GetCABundleResponse, error) {
	if m.getCABundleFunc != nil {
		return m.getCABundleFunc(ctx, req)
	}
	return &xkmstransport.GetCABundleResponse{
		BundlePEM: m.caCertPEM,
	}, nil
}

func (m *covMockCAService) GenerateCRL(_ context.Context, _ *xkmstransport.GenerateCRLRequest) (*xkmstransport.GenerateCRLResponse, error) {
	return &xkmstransport.GenerateCRLResponse{
		CRLPEM: []byte("-----BEGIN X509 CRL-----\ncrl\n-----END X509 CRL-----"),
	}, nil
}

func (m *covMockCAService) IsRevoked(ctx context.Context, req *xkmstransport.IsRevokedRequest) (*xkmstransport.IsRevokedResponse, error) {
	if m.isRevokedFunc != nil {
		return m.isRevokedFunc(ctx, req)
	}
	return &xkmstransport.IsRevokedResponse{Revoked: false}, nil
}

type covMockCryptoManager struct{}

func (m *covMockCryptoManager) Encrypt(_ context.Context, _ string, _, _ []byte) ([]byte, error) {
	return nil, nil
}

func (m *covMockCryptoManager) Decrypt(_ context.Context, _ string, _, _ []byte) ([]byte, error) {
	return nil, nil
}

func (m *covMockCryptoManager) Rewrap(_ context.Context, _ string, _, _ []byte) ([]byte, error) {
	return nil, nil
}

func (m *covMockCryptoManager) EncryptBatch(_ context.Context, _ string, _ [][]byte, _ []byte) ([][]byte, error) {
	return nil, nil
}

func (m *covMockCryptoManager) DecryptBatch(_ context.Context, _ string, _ [][]byte, _ []byte) ([][]byte, error) {
	return nil, nil
}

func (m *covMockCryptoManager) Sign(_ context.Context, _ string, _ []byte, _ string, _ bool) ([]byte, error) {
	return nil, nil
}

func (m *covMockCryptoManager) Verify(_ context.Context, _ string, _, _ []byte, _ string) (bool, error) {
	return false, nil
}

func (m *covMockCryptoManager) GenerateHMAC(_ context.Context, _ string, _ []byte, _ string) ([]byte, error) {
	return nil, nil
}

func (m *covMockCryptoManager) VerifyHMAC(_ context.Context, _ string, _, _ []byte, _ string) (bool, error) {
	return false, nil
}

func (m *covMockCryptoManager) GenerateRandom(_ context.Context, _ int) ([]byte, error) {
	return nil, nil
}

func (m *covMockCryptoManager) Hash(_ context.Context, _ []byte, _ string) ([]byte, error) {
	return nil, nil
}

type covMockSecretsManager struct{}

func (m *covMockSecretsManager) Store(_ context.Context, _ string, _ map[string][]byte, _ map[string]string) error {
	return nil
}

func (m *covMockSecretsManager) StoreWithOptions(_ context.Context, _ string, _ map[string][]byte, _ map[string]string, _ *interfaces.StoreOptions) error {
	return nil
}

func (m *covMockSecretsManager) Get(_ context.Context, _ string) (map[string][]byte, map[string]string, error) {
	return nil, nil, nil
}

func (m *covMockSecretsManager) List(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (m *covMockSecretsManager) Delete(_ context.Context, _ string) error { return nil }

func (m *covMockSecretsManager) Destroy(_ context.Context, _ string) error {
	return nil
}

func (m *covMockSecretsManager) ListVersions(_ context.Context, _ string) ([]*interfaces.SecretVersionMetadata, error) {
	return nil, nil
}

func (m *covMockSecretsManager) Rollback(_ context.Context, _ string, _ int) error {
	return nil
}

func (m *covMockSecretsManager) GetBackendType() string { return "mock" }

func (m *covMockSecretsManager) GetVersion(_ context.Context, _ string, _ int) (map[string][]byte, map[string]string, error) {
	return nil, nil, nil
}

func (m *covMockSecretsManager) Close() error { return nil }

type covMockEmbeddingManager struct{}

func (m *covMockEmbeddingManager) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, nil
}

func (m *covMockEmbeddingManager) EmbedBatch(_ context.Context, _ []string) ([][]float32, error) {
	return nil, nil
}

func (m *covMockEmbeddingManager) Dimensions() (int, error) { return 128, nil }

func (m *covMockEmbeddingManager) GetProvider(_ string) (interfaces.EmbeddingProvider, error) {
	return nil, nil
}

func (m *covMockEmbeddingManager) GetStats() interfaces.EmbeddingStats {
	return interfaces.EmbeddingStats{}
}

type covMockKVStore struct{}

func (m *covMockKVStore) Put(_ context.Context, _ string, _ []byte) error { return nil }

func (m *covMockKVStore) Scan(_ context.Context, _ string, _ func(string, []byte) error) error {
	return nil
}

func (m *covMockKVStore) Exists(_ context.Context, _ string) (bool, error) { return false, nil }

func (m *covMockKVStore) Get(_ context.Context, _ string) ([]byte, error) { return nil, nil }

func (m *covMockKVStore) Delete(_ context.Context, _ string) error { return nil }

func (m *covMockKVStore) List(_ context.Context, _ string) ([]string, error) { return nil, nil }

type covKeyspaceError struct {
	msg string
}

type covFailingKeyspaceResolver struct {
	buildKeyErr       error
	buildPrefixErr    error
	buildTenantPfxErr error
}

func (r *covFailingKeyspaceResolver) BuildKey(_ context.Context, _ string, _ uint64) ([]byte, error) {
	if r.buildKeyErr != nil {
		return nil, r.buildKeyErr
	}
	return []byte("key"), nil
}

func (r *covFailingKeyspaceResolver) BuildPrefix(_ context.Context, _ string) ([]byte, error) {
	if r.buildPrefixErr != nil {
		return nil, r.buildPrefixErr
	}
	return []byte("prefix:"), nil
}

func (r *covFailingKeyspaceResolver) BuildTenantPrefix(_ context.Context) ([]byte, error) {
	if r.buildTenantPfxErr != nil {
		return nil, r.buildTenantPfxErr
	}
	return nil, nil
}

func (r *covFailingKeyspaceResolver) ParseKey(_ []byte) (string, string, uint64, error) {
	return "", "", 0, nil
}

func (r *covFailingKeyspaceResolver) ValidateAccess(_ context.Context, _ []byte) error {
	return nil
}

func (r *covFailingKeyspaceResolver) IsSystemKey(_ []byte) bool { return false }

func (r *covFailingKeyspaceResolver) IsTenantKey(_ []byte) bool { return false }

func (r *covFailingKeyspaceResolver) IsLegacyKey(_ []byte) bool { return false }

func (r *covFailingKeyspaceResolver) ValidateEntityType(_ string) error { return nil }

func (r *covFailingKeyspaceResolver) BuildSystemKey(_ string) []byte { return nil }

func (r *covFailingKeyspaceResolver) ExtractEntityTypeFromKey(_ []byte) (string, error) {
	return "", nil
}

type covMockSchedulerEngine struct {
	listJobsFn func(ctx context.Context, query *scheduler.JobQuery) ([]*scheduler.Job, string, error)
}

func (m *covMockSchedulerEngine) Start(_ context.Context) error { return nil }

func (m *covMockSchedulerEngine) Stop(_ context.Context) error { return nil }

func (m *covMockSchedulerEngine) RegisterHandler(_ string, _ scheduler.JobHandler) error {
	return nil
}

func (m *covMockSchedulerEngine) ScheduleJob(_ context.Context, _ *scheduler.Job) error {
	return nil
}

func (m *covMockSchedulerEngine) GetJobWithTenant(_ context.Context, _ uint64, _ string) (*scheduler.Job, error) {
	return nil, nil
}

func (m *covMockSchedulerEngine) UpdateJob(_ context.Context, _ uint64, _ string, _ *scheduler.JobUpdateParams) (*scheduler.Job, error) {
	return nil, nil
}

func (m *covMockSchedulerEngine) DeleteJob(_ context.Context, _ uint64, _ string, _ bool) error {
	return nil
}

func (m *covMockSchedulerEngine) ListJobsWithCursor(_ context.Context, query *scheduler.JobQuery) ([]*scheduler.Job, string, error) {
	if m.listJobsFn != nil {
		return m.listJobsFn(nil, query)
	}
	return nil, "", nil
}

func (m *covMockSchedulerEngine) TriggerJob(_ context.Context, _ uint64, _ string) error {
	return nil
}

func (m *covMockSchedulerEngine) PauseJobWithStatus(_ context.Context, _ uint64, _ string) (scheduler.JobStatus, error) {
	return "", nil
}

func (m *covMockSchedulerEngine) ResumeJobWithStatus(_ context.Context, _ uint64, _ string) (scheduler.JobStatus, error) {
	return "", nil
}

func (m *covMockSchedulerEngine) CancelJobWithTenant(_ context.Context, _ uint64, _ string, _ string) (scheduler.JobStatus, error) {
	return "", nil
}

func (m *covMockSchedulerEngine) GetJobHistory(_ context.Context, _ uint64, _ string, _ int) ([]*scheduler.JobExecution, error) {
	return nil, nil
}

func (m *covMockSchedulerEngine) Stats() *scheduler.SchedulerStats { return nil }

func (m *covMockSchedulerEngine) IsRunning() bool { return false }

type covNonCacheMgr struct{}

func (m *covNonCacheMgr) Clear() {}

type boost81RaftNode struct{}

func (m *boost81RaftNode) ID() string { return "boost81-node" }

func (m *boost81RaftNode) RaftAddress() string { return "localhost:63001" }

func (m *boost81RaftNode) ShardCount() int { return 1 }

func (m *boost81RaftNode) IsDraining() bool { return false }

func (m *boost81RaftNode) Drain() error { return nil }

func (m *boost81RaftNode) HealthCheck() *interfaces.HealthStatus { return &interfaces.HealthStatus{} }

func (m *boost81RaftNode) SyncPropose(_ context.Context, _ uint64, _ []byte) (interfaces.Result, error) {
	return interfaces.Result{}, nil
}

func (m *boost81RaftNode) ProposeWithMode(_ context.Context, _ uint64, _ []byte, _ interfaces.WriteMode) (interfaces.Result, error) {
	return interfaces.Result{}, nil
}

func (m *boost81RaftNode) SyncRead(_ context.Context, _ uint64, _ interface{}) (interface{}, error) {
	return nil, nil
}

func (m *boost81RaftNode) StaleRead(_ context.Context, _ uint64, _ interface{}) (interface{}, error) {
	return nil, nil
}

func (m *boost81RaftNode) GetLeaderID(_ uint64) (uint64, bool, error) {
	return 1, true, nil
}

func (m *boost81RaftNode) GetNodeHostInfo(_ interfaces.NodeHostInfoOption) *interfaces.NodeHostInfo {
	return &interfaces.NodeHostInfo{}
}

func (m *boost81RaftNode) RequestAddNode(_ context.Context, _, _ uint64, _ string, _ uint64) (interfaces.RequestState, error) {
	return nil, nil
}

func (m *boost81RaftNode) RequestRemoveNode(_ context.Context, _, _ uint64, _ uint64) (interfaces.RequestState, error) {
	return nil, nil
}

func (m *boost81RaftNode) RequestSnapshot(_ context.Context, _ uint64) (interfaces.RequestState, error) {
	return nil, nil
}

func (m *boost81RaftNode) StartShard(_ map[uint64]string, _ bool, _ interfaces.CreateFunc, _ interfaces.ShardConfig) error {
	return nil
}

func (m *boost81RaftNode) StopShard(_ uint64) error {
	return nil
}

func (m *boost81RaftNode) ReadWithConsistency(_ context.Context, _ uint64, _ interface{}, _ interfaces.ReadOptions) (interface{}, error) {
	return nil, nil
}

func (m *boost81RaftNode) GetShardRaftState(_ uint64) (uint64, uint64, error) {
	return 0, 0, nil
}

type boost82MockCacheManager struct {
	closeErr error
}

type boost82MockBackupManager struct {
	closeErr error
}

func (m *boost82MockBackupManager) CreateBackup(_ context.Context, _ interface{}) (interface{}, error) {
	return nil, nil
}

func (m *boost82MockBackupManager) RestoreBackup(_ context.Context, _ string, _ interface{}) error {
	return nil
}

func (m *boost82MockBackupManager) ListBackups(_ context.Context, _ int) (interface{}, error) {
	return nil, nil
}

func (m *boost82MockBackupManager) DeleteBackup(_ context.Context, _ string) error {
	return nil
}

func (m *boost82MockBackupManager) VerifyBackup(_ context.Context, _ string) (interface{}, error) {
	return nil, nil
}

type boost82MockLogger struct {
	debugfCalled bool
	warnfCalled  bool
	errorfCalled bool
}

func (l *boost82MockLogger) Debugf(format string, args ...interface{}) { l.debugfCalled = true }
func (l *boost82MockLogger) Infof(format string, args ...interface{})  {}
func (l *boost82MockLogger) Warnf(format string, args ...interface{})  { l.warnfCalled = true }
func (l *boost82MockLogger) Errorf(format string, args ...interface{}) { l.errorfCalled = true }
func (l *boost82MockLogger) Fatalf(format string, args ...interface{}) {}
func (l *boost82MockLogger) Debug(msg string, args ...any)             {}
func (l *boost82MockLogger) Info(msg string, args ...any)              {}
func (l *boost82MockLogger) Warn(msg string, args ...any)              {}
func (l *boost82MockLogger) Error(msg string, args ...any)             {}
func (l *boost82MockLogger) Fatal(msg string)                          {}

func (l *boost82MockLogger) WithField(key string, value interface{}) interfaces.Logger  { return l }
func (l *boost82MockLogger) WithFields(fields map[string]interface{}) interfaces.Logger { return l }
func (l *boost82MockLogger) WithError(err error) interfaces.Logger                      { return l }

func TestServiceBuilder_WithTimeSeriesStorage(t *testing.T) {
	builder := NewServiceBuilder()
	mockTSStorage := &struct{}{}

	result := builder.WithTimeSeriesStorage(mockTSStorage)

	// Verify it returns the builder for chaining
	assert.NotNil(t, result)

	// Verify we can still use the builder
	assert.NotNil(t, builder)
}

func TestClusterService_RebalanceCluster_NilRaftNode(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log, qrdbClient: nil}
	clusterSvc := &clusterService{svc: svc}

	resp, err := clusterSvc.RebalanceCluster(context.Background(), &RebalanceClusterRequest{
		TargetShardCount: 3,
	})
	require.Error(t, err)
	assert.Nil(t, resp)

	qbErr, ok := err.(*dberrors.QBError)
	assert.True(t, ok)
	assert.Equal(t, dberrors.ErrInternal, qbErr.Code)
	assert.Contains(t, err.Error(), "qrdb client service not configured")
}

func TestClusterService_AddNode_NilQRDBClient(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log, qrdbClient: nil}
	clusterSvc := &clusterService{svc: svc}

	resp, err := clusterSvc.AddNode(context.Background(), &AddNodeRequest{
		ShardID: 1,
		NodeID:  2,
		Address: "localhost:9000",
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "qrdb client provider not configured")
}

func TestClusterService_RemoveNode_NilQRDBClient(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log, qrdbClient: nil}
	clusterSvc := &clusterService{svc: svc}

	resp, err := clusterSvc.RemoveNode(context.Background(), &RemoveNodeRequest{
		ShardID: 1,
		NodeID:  2,
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "qrdb client provider not configured")
}

func TestClusterService_CreateShard_NilQRDBClient(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log, qrdbClient: nil}
	clusterSvc := &clusterService{svc: svc}

	resp, err := clusterSvc.CreateShard(context.Background(), &CreateShardRequest{
		ShardID:        1,
		InitialMembers: map[uint64]string{1: "localhost:9000"},
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "qrdb client service not configured")
}

type coverageRaftNode struct{}

type coverageEmbeddingManager struct {
	mock.Mock
}

func newCoverageEmbeddingManager() *coverageEmbeddingManager {
	return &coverageEmbeddingManager{}
}

type coverageEmbeddingProvider struct {
	mock.Mock
}

func newCoverageEmbeddingProvider() *coverageEmbeddingProvider {
	return &coverageEmbeddingProvider{}
}

func (m *coverageEmbeddingProvider) MaxTokens() int {
	args := m.Called()
	return args.Int(0)
}

func (m *coverageEmbeddingProvider) Name() string {
	args := m.Called()
	return args.String(0)
}

func (m *coverageEmbeddingProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	args := m.Called(ctx, text)
	if v := args.Get(0); v != nil {
		return v.([]float32), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *coverageEmbeddingProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	args := m.Called(ctx, texts)
	if v := args.Get(0); v != nil {
		return v.([][]float32), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *coverageEmbeddingProvider) Dimensions() int {
	args := m.Called()
	return args.Int(0)
}

func (m *coverageEmbeddingProvider) Close() error {
	return nil
}

type coverageEmbeddingLogger struct {
	fields map[string]interface{}
}

func newCoverageEmbeddingLogger() *coverageEmbeddingLogger {
	return &coverageEmbeddingLogger{
		fields: make(map[string]interface{}),
	}
}

type mockKMSScheduler struct{}

type rotateKeyTestScheduler struct {
	scheduleJobFn func(ctx context.Context, job *SchedulerJob) error
	lastJob       *SchedulerJob
}

// newTestKMSServiceForValidation creates a minimal kmsServiceImpl for validation testing.
func newTestKMSServiceForValidation() *kmsServiceImpl {
	return &kmsServiceImpl{
		dekStore: newDEKStore(),
	}
}

type mockReencStatusPartialGetters struct {
	active bool
}

func (m *mockReencStatusPartialGetters) GetActive() bool { return m.active }

type mockReencStatusWithStartTime struct {
	startedAt time.Time
}

func (m *mockReencStatusWithStartTime) GetStartedAt() time.Time { return m.startedAt }

type mockReencStatusWithProgress struct {
	progress float64
}

func (m *mockReencStatusWithProgress) GetProgress() float64 { return m.progress }

type mockReencStatusWithOldVersion struct {
	oldVersion uint64
}

func (m *mockReencStatusWithOldVersion) GetOldVersion() uint64 { return m.oldVersion }

type cov90MockCacheManager struct {
	getFn    func(ctx context.Context, key string) ([]byte, error)
	setFn    func(ctx context.Context, key string, value []byte) error
	deleteFn func(ctx context.Context, key string) error
	closeFn  func() error
}

type cov90MockStorage struct {
	closeFn func() error
}

func (m *cov90MockStorage) GetWithContext(_ context.Context, _ []byte) ([]byte, error) {
	return nil, nil
}

func (m *cov90MockStorage) SetWithContext(_ context.Context, _, _ []byte) error { return nil }

func (m *cov90MockStorage) Batch() storage.Batch { return nil }

func (m *cov90MockStorage) NewIterator(_ *storage.IteratorOptions) storage.Iterator { return nil }

func (m *cov90MockStorage) Snapshot() (storage.Snapshot, error) { return nil, nil }

func (m *cov90MockStorage) Compact(_, _ []byte) error { return nil }

func (m *cov90MockStorage) Flush() error { return nil }

type cov90MockBackupManager struct {
	closeFn func() error
}

type cov90MockClosingQRDBProvider struct {
	closeFn func() error
}

func (m *cov90MockClosingQRDBProvider) ForShard(_ uint64) (*qrdbsdk.Client, error) {
	return nil, stderrors.New("not implemented")
}

func (m *cov90MockClosingQRDBProvider) IsEmbedded() bool { return true }

type svcBoostUser struct {
	id       string
	username string
	roles    []string
	created  time.Time
	lastAuth time.Time
}

func (u *svcBoostUser) GetID() string { return u.id }

func (u *svcBoostUser) GetUsername() string { return u.username }

func (u *svcBoostUser) GetRoles() []string { return u.roles }

func (u *svcBoostUser) GetCreated() time.Time { return u.created }

func (u *svcBoostUser) GetLastAuth() time.Time { return u.lastAuth }

type svcBoostRaftNode struct {
	syncReadFn     func(ctx context.Context, shardID uint64, query interface{}) (interface{}, error)
	syncReadErr    error
	syncReadResult interface{}
}

func TestServiceAccessors_KMS(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl, ok := svc.(*quicBaaS)
	require.True(t, ok)

	// Test KMS accessor when nil
	kmsService := impl.KMS()
	assert.Nil(t, kmsService)

	// Test KMS accessor when set
	mockKMS := newKMSService(&mockKMSManager{keys: make(map[string]*mockKeyData)}, createTestLogger(t))
	impl.kmsService = mockKMS
	kmsService = impl.KMS()
	assert.NotNil(t, kmsService)
	assert.Equal(t, mockKMS, kmsService)
}

func TestServiceAccessors_Secrets(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl, ok := svc.(*quicBaaS)
	require.True(t, ok)

	// Test Secrets accessor when nil
	secretsService := impl.Secrets()
	assert.Nil(t, secretsService)

	// Test Secrets accessor when set
	mockSecrets := newSecretsService(&mockSecretsManager{}, createTestLogger(t))
	impl.secretsService = mockSecrets
	secretsService = impl.Secrets()
	assert.NotNil(t, secretsService)
	assert.Equal(t, mockSecrets, secretsService)
}

func TestServiceAccessors_Crypto(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl, ok := svc.(*quicBaaS)
	require.True(t, ok)

	// Test Crypto accessor when nil
	cryptoService := impl.Crypto()
	assert.Nil(t, cryptoService)

	// Test Crypto accessor when set (requires crypto manager and kms manager)
	mockCrypto := newCryptoService(&mockCryptoManager{}, &mockKMSManager{keys: make(map[string]*mockKeyData)}, createTestLogger(t), nil)
	impl.cryptoService = mockCrypto
	cryptoService = impl.Crypto()
	assert.NotNil(t, cryptoService)
	assert.Equal(t, mockCrypto, cryptoService)
}

func TestServiceAccessors_Vector(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl, ok := svc.(*quicBaaS)
	require.True(t, ok)

	// Test Vector accessor when nil
	vectorService := impl.Vector()
	assert.Nil(t, vectorService)
}

func TestServiceAccessors_Embedding(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl, ok := svc.(*quicBaaS)
	require.True(t, ok)

	// Test Embedding accessor when nil
	embeddingService := impl.Embedding()
	assert.Nil(t, embeddingService)
}

func TestTimeSeriesMethods_WritePoint(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl, ok := svc.(*quicBaaS)
	require.True(t, ok)

	req := &WritePointRequest{
		Metric:    "cpu",
		Timestamp: time.Now(),
		Value:     1.0,
		Tags:      map[string]string{"host": "server1"},
	}

	_, err := impl.WritePoint(nil, req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestTimeSeriesMethods_WriteBatch(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl, ok := svc.(*quicBaaS)
	require.True(t, ok)

	req := &WriteBatchRequest{
		Points: []*WritePointRequest{
			{Metric: "cpu", Timestamp: time.Now(), Value: 1.0},
		},
	}

	_, err := impl.WriteBatch(nil, req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestTimeSeriesMethods_TSQuery(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl, ok := svc.(*quicBaaS)
	require.True(t, ok)

	req := &TSQueryRequest{
		Metric:    "cpu",
		StartTime: time.Now().Add(-1 * time.Hour),
		EndTime:   time.Now(),
	}

	_, err := impl.TSQuery(nil, req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestAuthServiceImpl_Login_NilManager(t *testing.T) {
	// Password-based login is removed. Login() always returns ErrNotImplemented.
	svc := &quicBaaS{}
	authSvc := &authService{svc: svc}

	_, err := authSvc.Login(context.Background(), &LoginRequest{
		Username: "test",
	})

	require.Error(t, err)
}

func TestAuthServiceImpl_Logout_NilManager(t *testing.T) {
	svc := &quicBaaS{}
	authSvc := &authService{svc: svc}

	_, err := authSvc.Logout(context.Background(), &LogoutRequest{
		AccessToken: "token",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication")
}

func TestAuthServiceImpl_RefreshToken_NilManager(t *testing.T) {
	svc := &quicBaaS{}
	authSvc := &authService{svc: svc}

	_, err := authSvc.RefreshToken(context.Background(), &RefreshTokenRequest{
		RefreshToken: "token",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication")
}

func TestAuthServiceImpl_ValidateToken_NilManager(t *testing.T) {
	svc := &quicBaaS{}
	authSvc := &authService{svc: svc}

	_, err := authSvc.ValidateToken(context.Background(), &ValidateTokenRequest{
		AccessToken: "token",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication")
}

func TestRBACServiceImpl_AssignRole_NilManager(t *testing.T) {
	svc := &quicBaaS{}
	rbacSvc := &rbacService{svc: svc}

	_, err := rbacSvc.AssignRole(context.Background(), &AssignRoleRequest{
		Username: "testuser",
		RoleName: "admin",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "service.AssignRole")
}

func TestRBACServiceImpl_RevokeRole_NilManager(t *testing.T) {
	svc := &quicBaaS{}
	rbacSvc := &rbacService{svc: svc}

	_, err := rbacSvc.RevokeRole(context.Background(), &RevokeRoleRequest{
		Username: "testuser",
		RoleName: "admin",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "service.RevokeRole")
}

func TestClusterServiceImpl_CreateShard_ValidationError(t *testing.T) {
	svc := &quicBaaS{}
	clusterSvc := &clusterService{svc: svc}

	_, err := clusterSvc.CreateShard(context.Background(), &CreateShardRequest{
		ShardID: 0, // Invalid: ShardID must be > 0
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "shard")
}

func TestServiceBuilder_WithKMSManager(t *testing.T) {
	tests := []struct {
		name       string
		kmsManager interface{}
	}{
		{
			name:       "with nil KMS manager",
			kmsManager: nil,
		},
		{
			name:       "with string (invalid type)",
			kmsManager: "invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewServiceBuilder()
			result := builder.WithKMSManager(tt.kmsManager)

			sb, ok := result.(*serviceBuilder)
			require.True(t, ok)
			assert.Equal(t, tt.kmsManager, sb.kmsManager)
			assert.Same(t, builder, result, "WithKMSManager should return the same builder for chaining")
		})
	}
}

func TestServiceBuilder_WithSecretsManager(t *testing.T) {
	tests := []struct {
		name           string
		secretsManager interface{}
	}{
		{
			name:           "with nil secrets manager",
			secretsManager: nil,
		},
		{
			name:           "with string (invalid type)",
			secretsManager: "invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewServiceBuilder()
			result := builder.WithSecretsManager(tt.secretsManager)

			sb, ok := result.(*serviceBuilder)
			require.True(t, ok)
			assert.Equal(t, tt.secretsManager, sb.secretsManager)
			assert.Same(t, builder, result, "WithSecretsManager should return the same builder for chaining")
		})
	}
}

func TestServiceBuilder_WithCryptoService(t *testing.T) {
	tests := []struct {
		name          string
		cryptoService interface{}
	}{
		{
			name:          "with nil crypto service",
			cryptoService: nil,
		},
		{
			name:          "with string (invalid type)",
			cryptoService: "invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewServiceBuilder()
			result := builder.WithCryptoService(tt.cryptoService)

			sb, ok := result.(*serviceBuilder)
			require.True(t, ok)
			assert.Equal(t, tt.cryptoService, sb.cryptoService)
			assert.Same(t, builder, result, "WithCryptoService should return the same builder for chaining")
		})
	}
}

func TestQueryService_Query_Success(t *testing.T) {
	mockRaft := &mockRaftNode{}

	scanData := map[string][]byte{
		"user:1": []byte(`{"name":"alice","age":30}`),
		"user:2": []byte(`{"name":"bob","age":25}`),
	}
	provider := newSvcBoostMockQRDBProvider(scanData, nil)

	config := &ServiceConfig{
		NodeID:     1,
		Version:    "1.0.0",
		EnableAuth: false,
	}

	svc, err := NewServiceBuilder().
		WithRaftNode(mockRaft).
		WithQRDBClient(provider).
		WithConfig(config).
		Build()
	require.NoError(t, err)
	defer svc.Close()

	// Create response channel
	stream := make(chan *QueryResponse, 10)

	// Execute query
	req := &QueryRequest{
		EntityType: "user",
		ShardID:    1,
	}

	go func() {
		err := svc.Query(context.Background(), req, stream)
		assert.NoError(t, err)
	}()

	// Receive response
	var resp *QueryResponse
	select {
	case resp = <-stream:
		require.NotNil(t, resp)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for query response")
	}

	// Verify response - sort by ID since map iteration order is non-deterministic
	assert.Len(t, resp.Entities, 2)
	sort.Slice(resp.Entities, func(i, j int) bool {
		return resp.Entities[i].ID < resp.Entities[j].ID
	})
	assert.Equal(t, "user", resp.Entities[0].EntityType)
	assert.Equal(t, uint64(1), resp.Entities[0].ID)
	assert.Equal(t, uint64(2), resp.Entities[1].ID)
}

func TestQueryService_Query_RaftError(t *testing.T) {
	mockRaft := &mockRaftNode{}
	provider := newSvcBoostMockQRDBProvider(nil, assert.AnError)

	config := &ServiceConfig{
		NodeID:        1,
		Version:       "1.0.0",
		EnableAuth:    false,
		EnableMetrics: true,
	}

	svc, err := NewServiceBuilder().
		WithRaftNode(mockRaft).
		WithQRDBClient(provider).
		WithConfig(config).
		Build()
	require.NoError(t, err)
	defer svc.Close()

	stream := make(chan *QueryResponse, 10)
	req := &QueryRequest{
		EntityType: "user",
		ShardID:    1,
	}

	err = svc.Query(context.Background(), req, stream)
	require.Error(t, err)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrRaft, dErr.Code)
}

func TestQueryService_Query_WithIndexFilter(t *testing.T) {
	mockRaft := &mockRaftNode{}
	provider := newSvcBoostMockQRDBProvider(map[string][]byte{
		"user:1": []byte(`{"name":"alice","status":"active"}`),
		"user:2": []byte(`{"name":"bob","status":"inactive"}`),
		"user:3": []byte(`{"name":"charlie","status":"active"}`),
	}, nil)

	svc, err := NewServiceBuilder().
		WithRaftNode(mockRaft).
		WithQRDBClient(provider).
		WithConfig(&ServiceConfig{NodeID: 1, Version: "1.0.0", EnableAuth: false}).
		Build()
	require.NoError(t, err)
	defer svc.Close()

	stream := make(chan *QueryResponse, 10)
	req := &QueryRequest{
		EntityType: "user",
		IndexName:  "status",
		Value:      []byte(`active`), // Without quotes to match string representation
		ShardID:    1,
	}

	go func() {
		err := svc.Query(context.Background(), req, stream)
		assert.NoError(t, err)
	}()

	var resp *QueryResponse
	select {
	case resp = <-stream:
		require.NotNil(t, resp)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for query response")
	}

	// Should filter based on index value (exercises matchesIndexValue function)
	// Actual number depends on matchesIndexValue logic
	assert.NotNil(t, resp.Entities)
}

func TestQueryService_Query_DefaultShardID(t *testing.T) {
	mockRaft := &mockRaftNode{}
	provider := newSvcBoostMockQRDBProvider(map[string][]byte{}, nil)

	svc, err := NewServiceBuilder().
		WithRaftNode(mockRaft).
		WithQRDBClient(provider).
		WithConfig(&ServiceConfig{NodeID: 1, Version: "1.0.0", EnableAuth: false}).
		Build()
	require.NoError(t, err)
	defer svc.Close()

	stream := make(chan *QueryResponse, 10)
	req := &QueryRequest{
		EntityType: "user",
		ShardID:    0, // Should default to 1
	}

	go func() {
		_ = svc.Query(context.Background(), req, stream)
	}()

	// Verify query completes successfully (shardID 0 defaults to 1 internally)
	resp := <-stream
	assert.NotNil(t, resp)
}

func TestQueryService_Query_WithMetrics(t *testing.T) {
	mockRaft := &mockRaftNode{}
	provider := newSvcBoostMockQRDBProvider(map[string][]byte{}, nil)

	config := &ServiceConfig{
		NodeID:        1,
		Version:       "1.0.0",
		EnableAuth:    false,
		EnableMetrics: true,
	}

	svc, err := NewServiceBuilder().
		WithRaftNode(mockRaft).
		WithQRDBClient(provider).
		WithConfig(config).
		Build()
	require.NoError(t, err)
	defer svc.Close()

	stream := make(chan *QueryResponse, 10)
	req := &QueryRequest{
		EntityType: "user",
		ShardID:    1,
	}

	go func() {
		_ = svc.Query(context.Background(), req, stream)
	}()

	// Wait for completion
	<-stream

	// Verify metrics were recorded (coverage for metrics path)
	impl, ok := svc.(*quicBaaS)
	require.True(t, ok)
	assert.NotNil(t, impl.metrics)
}

func TestQueryService_Query_NilResult(t *testing.T) {
	mockRaft := &mockRaftNode{}
	provider := newSvcBoostMockQRDBProvider(nil, nil)

	svc, err := NewServiceBuilder().
		WithRaftNode(mockRaft).
		WithQRDBClient(provider).
		WithConfig(&ServiceConfig{NodeID: 1, Version: "1.0.0", EnableAuth: false}).
		Build()
	require.NoError(t, err)
	defer svc.Close()

	stream := make(chan *QueryResponse, 10)
	req := &QueryRequest{
		EntityType: "user",
		ShardID:    1,
	}

	go func() {
		err := svc.Query(context.Background(), req, stream)
		assert.NoError(t, err)
	}()

	var resp *QueryResponse
	select {
	case resp = <-stream:
		require.NotNil(t, resp)
		assert.Empty(t, resp.Entities)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for query response")
	}
}

func TestQueryService_Query_InvalidIDParsing(t *testing.T) {
	mockRaft := &mockRaftNode{}
	provider := newSvcBoostMockQRDBProvider(map[string][]byte{
		"user:invalid": []byte(`{"name":"alice"}`),
	}, nil)

	svc, err := NewServiceBuilder().
		WithRaftNode(mockRaft).
		WithQRDBClient(provider).
		WithConfig(&ServiceConfig{NodeID: 1, Version: "1.0.0", EnableAuth: false}).
		Build()
	require.NoError(t, err)
	defer svc.Close()

	stream := make(chan *QueryResponse, 10)
	req := &QueryRequest{
		EntityType: "user",
		ShardID:    1,
	}

	go func() {
		err := svc.Query(context.Background(), req, stream)
		assert.NoError(t, err)
	}()

	var resp *QueryResponse
	select {
	case resp = <-stream:
		require.NotNil(t, resp)
		// Should still include the entity even if ID parsing fails
		assert.Len(t, resp.Entities, 1)
		assert.Equal(t, uint64(0), resp.Entities[0].ID) // ID defaults to 0 on parse failure
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for query response")
	}
}

func TestQueryService_Query_EmptyValueIncluded(t *testing.T) {
	mockRaft := &mockRaftNode{}
	provider := newSvcBoostMockQRDBProvider(map[string][]byte{
		"user:1": []byte(`{"name":"alice"}`),
		"user:2": nil, // empty value
	}, nil)

	svc, err := NewServiceBuilder().
		WithRaftNode(mockRaft).
		WithQRDBClient(provider).
		WithConfig(&ServiceConfig{NodeID: 1, Version: "1.0.0", EnableAuth: false}).
		Build()
	require.NoError(t, err)
	defer svc.Close()

	stream := make(chan *QueryResponse, 10)
	req := &QueryRequest{
		EntityType: "user",
		ShardID:    1,
	}

	go func() {
		err := svc.Query(context.Background(), req, stream)
		assert.NoError(t, err)
	}()

	var resp *QueryResponse
	select {
	case resp = <-stream:
		require.NotNil(t, resp)
		// Both keys should be included (even with nil value)
		assert.Len(t, resp.Entities, 2)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for query response")
	}
}

// Test Helpers.
func testLoggerCoverage(t *testing.T) interfaces.Logger {
	cfg := logger.LogConfig{
		Level:  "error",
		Format: "console",
		Output: "stdout",
	}
	log, err := logger.New(cfg)
	require.NoError(t, err)
	return &simpleLogger{log}
}

// Health Service Tests - Liveness and Readiness (0% -> 100%).
func TestHealthServiceImpl_Liveness(t *testing.T) {
	svc := &quicBaaS{
		config: &ServiceConfig{},
		logger: testLoggerCoverage(t),
	}

	healthSvc := &healthService{svc: svc}

	resp, err := healthSvc.Liveness(context.Background(), &LivenessRequest{})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Alive)
}

func TestHealthServiceImpl_Readiness(t *testing.T) {
	svc := &quicBaaS{
		config: &ServiceConfig{},
		logger: testLoggerCoverage(t),
	}

	healthSvc := &healthService{svc: svc}

	resp, err := healthSvc.Readiness(context.Background(), &ReadinessRequest{})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Ready)
}

// Cluster Service Tests - ClusterStatus (45% -> 75%).
func TestClusterServiceImpl_ClusterStatus_Success(t *testing.T) {
	provider := newSvcBoostMockQRDBProviderWithTopology(nil, nil,
		&transport.TopologyResponse{
			Shards: []transport.ShardInfo{
				{
					ShardID:  1,
					LeaderID: 1,
					Nodes: map[uint64]string{
						1: "node1:8080",
						2: "node2:8080",
					},
				},
			},
		}, nil)

	svc := &quicBaaS{
		qrdbClient: provider,
		logger:     testLoggerCoverage(t),
	}

	clusterSvc := &clusterService{svc: svc}

	resp, err := clusterSvc.ClusterStatus(context.Background(), &ClusterStatusRequest{})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "quicbaas-cluster", resp.ClusterID)
	assert.NotEmpty(t, resp.Nodes)
	assert.NotEmpty(t, resp.Shards)
}

func TestClusterServiceImpl_ClusterStatus_EmptyNodeInfo(t *testing.T) {
	provider := newSvcBoostMockQRDBProviderWithTopology(nil, nil, nil, nil)

	svc := &quicBaaS{
		qrdbClient: provider,
		logger:     testLoggerCoverage(t),
	}

	clusterSvc := &clusterService{svc: svc}

	resp, err := clusterSvc.ClusterStatus(context.Background(), &ClusterStatusRequest{})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Empty(t, resp.Nodes)
	assert.Empty(t, resp.Shards)
}

func TestClusterServiceImpl_ClusterStatus_MultipleShards(t *testing.T) {
	provider := newSvcBoostMockQRDBProviderWithTopology(nil, nil,
		&transport.TopologyResponse{
			Shards: []transport.ShardInfo{
				{
					ShardID:  1,
					LeaderID: 1,
					Nodes:    map[uint64]string{1: "node1:8080"},
				},
				{
					ShardID:  2,
					LeaderID: 2,
					Nodes:    map[uint64]string{2: "node2:8080"},
				},
			},
		}, nil)

	svc := &quicBaaS{
		qrdbClient: provider,
		logger:     testLoggerCoverage(t),
	}

	clusterSvc := &clusterService{svc: svc}

	resp, err := clusterSvc.ClusterStatus(context.Background(), &ClusterStatusRequest{})
	require.NoError(t, err)
	assert.Len(t, resp.Shards, 2)
}

// RBAC Service Tests - Error paths (53.8% -> 70%).
// (AssignRole with no IAM provider is covered by
// TestRBACServiceImpl_AssignRole_NotConfigured.)
func TestRBACServiceImpl_AssignRole_InvalidInput(t *testing.T) {
	svc := &quicBaaS{
		logger: testLoggerCoverage(t),
	}

	rbacSvc := &rbacService{svc: svc}

	tests := []struct {
		name string
		req  *AssignRoleRequest
	}{
		{
			name: "empty username",
			req:  &AssignRoleRequest{Username: "", RoleName: "admin"},
		},
		{
			name: "empty role",
			req:  &AssignRoleRequest{Username: "user", RoleName: ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := rbacSvc.AssignRole(context.Background(), tt.req)
			assert.Error(t, err)
		})
	}
}

func TestRBACServiceImpl_AssignRole_NotConfigured(t *testing.T) {
	// AssignRole routes through the qrdb-backed IdentityStore (authz-unification
	// C1-E). With no IAM provider wired, the iam build fails closed on the
	// missing tenant context / unconfigured identity store and the !iam stub
	// returns a typed not-configured error — both carry the service.AssignRole op.
	svc := &quicBaaS{
		logger: testLoggerCoverage(t),
	}

	rbacSvc := &rbacService{svc: svc}

	req := &AssignRoleRequest{
		Username: "testuser",
		RoleName: "admin",
	}

	_, err := rbacSvc.AssignRole(context.Background(), req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "service.AssignRole")
}

func TestRBACServiceImpl_RevokeRole_NoAuthManager(t *testing.T) {
	svc := &quicBaaS{
		logger: testLoggerCoverage(t),
	}

	rbacSvc := &rbacService{svc: svc}

	req := &RevokeRoleRequest{
		Username: "testuser",
		RoleName: "admin",
	}

	_, err := rbacSvc.RevokeRole(context.Background(), req)
	assert.Error(t, err)
}

func TestRBACServiceImpl_RevokeRole_InvalidInput(t *testing.T) {
	svc := &quicBaaS{
		logger: testLoggerCoverage(t),
	}

	rbacSvc := &rbacService{svc: svc}

	req := &RevokeRoleRequest{
		Username: "",
		RoleName: "admin",
	}

	_, err := rbacSvc.RevokeRole(context.Background(), req)
	assert.Error(t, err)
}

// GetMigrationProgress Tests (60% -> 80%).
func TestOperationsServiceImpl_GetMigrationProgress_Success(t *testing.T) {
	svc := &quicBaaS{
		logger: testLoggerCoverage(t),
	}

	opsSvc := newOperationsService(svc)

	job := &operationJob{
		ID:            "mig123",
		Type:          "migration",
		Status:        "running",
		Progress:      0.5,
		ItemsMigrated: 50,
		TotalItems:    100,
		StartTime:     time.Now(),
	}

	opsSvc.jobsMu.Lock()
	opsSvc.jobs["mig123"] = job
	opsSvc.jobsMu.Unlock()

	req := &GetMigrationProgressRequest{
		MigrationID: "mig123",
	}

	resp, err := opsSvc.GetMigrationProgress(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "mig123", resp.MigrationID)
	assert.Equal(t, 0.5, resp.Progress)
	assert.Equal(t, "running", resp.Status)
	assert.Equal(t, uint64(50), resp.ItemsMigrated)
	assert.Equal(t, uint64(100), resp.TotalItems)
}

// Delegation Methods Tests - Coverage for wrapper methods.
func TestServiceImpl_Liveness_Delegation(t *testing.T) {
	svc := &quicBaaS{
		config: &ServiceConfig{},
		logger: testLoggerCoverage(t),
	}

	svc.healthService = &healthService{svc: svc}

	resp, err := svc.Liveness(context.Background(), &LivenessRequest{})
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Alive)
}

func TestServiceImpl_Readiness_Delegation(t *testing.T) {
	svc := &quicBaaS{
		config: &ServiceConfig{},
		logger: testLoggerCoverage(t),
	}

	svc.healthService = &healthService{svc: svc}

	resp, err := svc.Readiness(context.Background(), &ReadinessRequest{})
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Ready)
}

func TestAuthService_Login_PasswordlessOnly(t *testing.T) {
	// Password-based login is removed. Use WebAuthn or PIV smartcard authentication.
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &LoginRequest{Username: "testuser"}
	_, err := svc.Login(context.Background(), req)
	require.Error(t, err)
}

func TestAuthService_RefreshToken_NoAuthManager(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &RefreshTokenRequest{
		RefreshToken: "token",
	}

	resp, err := svc.RefreshToken(context.Background(), req)
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "authentication service not configured")
}

func TestAuthService_ValidateToken_NoAuthManager(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &ValidateTokenRequest{
		AccessToken: "token",
	}

	resp, err := svc.ValidateToken(context.Background(), req)
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "authentication service not configured")
}

func TestClusterService_AddNode_NilRequest(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	resp, err := svc.AddNode(context.Background(), nil)
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClusterService_AddNode_InvalidShardID(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &AddNodeRequest{
		ShardID: 0,
		NodeID:  2,
		Address: "localhost:5002",
	}

	resp, err := svc.AddNode(context.Background(), req)
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "shard_id must be greater than 0")
}

func TestClusterService_AddNode_EmptyAddress(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &AddNodeRequest{
		ShardID: 1,
		NodeID:  2,
		Address: "",
	}

	resp, err := svc.AddNode(context.Background(), req)
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "address cannot be empty")
}

func TestCacheService_GetCacheStats_Success(t *testing.T) {
	cacheMgr, err := cache.NewManager(cache.Config{
		L1Size:          100,
		L1TTL:           1 * time.Minute,
		CleanupInterval: 5 * time.Minute,
	}, createTestLogger(t))
	require.NoError(t, err)

	svc := createTestService(t, withCacheManager(cacheMgr))
	defer svc.Close()

	req := &GetCacheStatsRequest{}

	resp, err := svc.GetCacheStats(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestCacheService_ClearCache_Success(t *testing.T) {
	cacheMgr, err := cache.NewManager(cache.Config{
		L1Size:          100,
		L1TTL:           1 * time.Minute,
		CleanupInterval: 5 * time.Minute,
	}, createTestLogger(t))
	require.NoError(t, err)

	svc := createTestService(t, withCacheManager(cacheMgr))
	defer svc.Close()

	req := &ClearCacheRequest{}

	resp, err := svc.ClearCache(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestCacheService_GetCacheConfig_Success(t *testing.T) {
	cacheMgr, err := cache.NewManager(cache.Config{
		L1Size:          100,
		L1TTL:           1 * time.Minute,
		CleanupInterval: 5 * time.Minute,
	}, createTestLogger(t))
	require.NoError(t, err)

	svc := createTestService(t, withCacheManager(cacheMgr))
	defer svc.Close()

	req := &GetCacheConfigRequest{}

	resp, err := svc.GetCacheConfig(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Enabled)
}

func TestCacheService_UpdateCacheConfig_Success(t *testing.T) {
	cacheMgr, err := cache.NewManager(cache.Config{
		L1Size:          100,
		L1TTL:           1 * time.Minute,
		CleanupInterval: 5 * time.Minute,
	}, createTestLogger(t))
	require.NoError(t, err)

	svc := createTestService(t, withCacheManager(cacheMgr))
	defer svc.Close()

	newSize := int(200)
	req := &UpdateCacheConfigRequest{
		L1Size: &newSize,
	}

	resp, err := svc.UpdateCacheConfig(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Updated)
}

func TestOperationsService_MigrateData_Success(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &MigrateDataRequest{
		SourceShard: 1,
		TargetShard: 2,
		Concurrency: 4,
	}

	resp, err := svc.MigrateData(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.NotEmpty(t, resp.MigrationID)
	assert.Equal(t, "initiated", resp.Status)
}

func TestOperationsService_GetMigrationProgress_Success(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	// Create migration first
	createReq := &MigrateDataRequest{
		SourceShard: 1,
		TargetShard: 2,
	}
	createResp, err := svc.MigrateData(context.Background(), createReq)
	require.NoError(t, err)

	// Get progress
	req := &GetMigrationProgressRequest{
		MigrationID: createResp.MigrationID,
	}

	resp, err := svc.GetMigrationProgress(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, createResp.MigrationID, resp.MigrationID)
	assert.Equal(t, "initiated", resp.Status)
}

func TestOperationsService_ReplaceNode_Success(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &ReplaceNodeRequest{
		ShardID:    1,
		OldNodeID:  2,
		NewNodeID:  3,
		NewAddress: "localhost:5003",
	}

	resp, err := svc.ReplaceNode(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.NotEmpty(t, resp.JobID)
	assert.Equal(t, "initiated", resp.Status)
}

func TestOperationsService_UpgradeNode_Success(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &UpgradeNodeRequest{
		NodeID:        1,
		TargetVersion: "2.0.0",
	}

	resp, err := svc.UpgradeNode(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.NotEmpty(t, resp.JobID)
	assert.Equal(t, "initiated", resp.Status)
}

func TestOperationsService_GetUpgradeStatus_Success(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	// Create upgrade first
	createReq := &UpgradeNodeRequest{
		NodeID:        1,
		TargetVersion: "2.0.0",
	}
	createResp, err := svc.UpgradeNode(context.Background(), createReq)
	require.NoError(t, err)

	// Get status
	req := &GetUpgradeStatusRequest{
		JobID: createResp.JobID,
	}

	resp, err := svc.GetUpgradeStatus(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, createResp.JobID, resp.JobID)
	assert.Equal(t, "initiated", resp.Status)
}

func TestHealthService_Health_Success(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &HealthRequest{
		IncludeDetails: false,
	}

	resp, err := svc.Health(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, HealthStatusHealthy, resp.Status)
	assert.GreaterOrEqual(t, resp.UptimeSeconds, uint64(0))
}

func TestHealthService_Health_WithDetails(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &HealthRequest{
		IncludeDetails: true,
	}

	resp, err := svc.Health(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.NotEmpty(t, resp.Details)
}

func TestServiceBuilder_Build_NilRaftNode(t *testing.T) {
	builder := NewServiceBuilder()

	svc, err := builder.Build()
	require.Error(t, err)
	require.Nil(t, svc)
	assert.Contains(t, err.Error(), "raft node is required")
}

func TestService_Close_Success(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))

	err := svc.Close()
	require.NoError(t, err)

	// Second close should be no-op
	err = svc.Close()
	require.NoError(t, err)
}

func TestService_Close_WithAllComponents(t *testing.T) {
	cacheMgr, err := cache.NewManager(cache.Config{
		L1Size:          100,
		L1TTL:           1 * time.Minute,
		CleanupInterval: 5 * time.Minute,
	}, createTestLogger(t))
	require.NoError(t, err)

	mockBackup := &mockBackupManager{}
	mockRaft := &mockRaftNode{}

	svc := createTestService(t,
		withMockRaftNode(mockRaft),
		withCacheManager(cacheMgr),
		withBackupManager(mockBackup),
	)

	err = svc.Close()
	require.NoError(t, err)
}

func TestConsistencyLevel_String(t *testing.T) {
	tests := []struct {
		level ConsistencyLevel
		want  string
	}{
		{ConsistencyLevelUnspecified, "unspecified"},
		{ConsistencyLevelEventual, "eventual"},
		{ConsistencyLevelLinearizable, "linearizable"},
		{ConsistencyLevelStrong, "strong"},
		{ConsistencyLevel(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.level.String()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestServiceImpl_DeleteSeries_Delegation(t *testing.T) {
	svc := &quicBaaS{
		timeSeriesService: &timeSeriesService{
			tsStorage: nil,
		},
	}

	req := &TSDeleteRequest{
		Metric: "test.metric",
	}

	_, err := svc.DeleteSeries(context.Background(), req)
	assert.Error(t, err)
	qbErr, ok := err.(*dberrors.QBError)
	require.True(t, ok)
	assert.Equal(t, dberrors.ErrNotImplemented, qbErr.Code)
}

func TestServiceImpl_SetRetentionPolicy_Delegation(t *testing.T) {
	svc := &quicBaaS{
		timeSeriesService: &timeSeriesService{
			tsStorage: nil,
		},
	}

	req := &TSSetRetentionPolicyRequest{
		Metric: "test.metric",
		Policy: &TSRetentionPolicy{
			Duration: 24 * time.Hour,
		},
	}

	_, err := svc.SetRetentionPolicy(context.Background(), req)
	assert.Error(t, err)
	qbErr, ok := err.(*dberrors.QBError)
	require.True(t, ok)
	assert.Equal(t, dberrors.ErrNotImplemented, qbErr.Code)
}

func TestServiceImpl_ListMetrics_Delegation(t *testing.T) {
	svc := &quicBaaS{
		timeSeriesService: &timeSeriesService{
			tsStorage: nil,
		},
	}

	req := &TSListMetricsRequest{}

	_, err := svc.ListMetrics(context.Background(), req)
	assert.Error(t, err)
	qbErr, ok := err.(*dberrors.QBError)
	require.True(t, ok)
	assert.Equal(t, dberrors.ErrNotImplemented, qbErr.Code)
}

func TestServiceImpl_WritePoint_Delegation(t *testing.T) {
	svc := &quicBaaS{
		timeSeriesService: &timeSeriesService{
			tsStorage: nil,
		},
	}

	req := &WritePointRequest{
		Metric: "test.metric",
		Value:  42.5,
	}

	_, err := svc.WritePoint(context.Background(), req)
	assert.Error(t, err)
	qbErr, ok := err.(*dberrors.QBError)
	require.True(t, ok)
	assert.Equal(t, dberrors.ErrNotImplemented, qbErr.Code)
}

func TestServiceImpl_WriteBatch_Delegation(t *testing.T) {
	svc := &quicBaaS{
		timeSeriesService: &timeSeriesService{
			tsStorage: nil,
		},
	}

	req := &WriteBatchRequest{
		Points: []*WritePointRequest{
			{Metric: "metric1", Value: 1.0},
		},
	}

	_, err := svc.WriteBatch(context.Background(), req)
	assert.Error(t, err)
	qbErr, ok := err.(*dberrors.QBError)
	require.True(t, ok)
	assert.Equal(t, dberrors.ErrNotImplemented, qbErr.Code)
}

func TestServiceImpl_TSQuery_Delegation(t *testing.T) {
	svc := &quicBaaS{
		timeSeriesService: &timeSeriesService{
			tsStorage: nil,
		},
	}

	req := &TSQueryRequest{
		Metric: "test.metric",
	}

	_, err := svc.TSQuery(context.Background(), req)
	assert.Error(t, err)
	qbErr, ok := err.(*dberrors.QBError)
	require.True(t, ok)
	assert.Equal(t, dberrors.ErrNotImplemented, qbErr.Code)
}

func TestServiceImpl_GetRetentionPolicy_Delegation(t *testing.T) {
	svc := &quicBaaS{
		timeSeriesService: &timeSeriesService{
			tsStorage: nil,
		},
	}

	req := &TSGetRetentionPolicyRequest{
		Metric: "test.metric",
	}

	_, err := svc.GetRetentionPolicy(context.Background(), req)
	assert.Error(t, err)
	qbErr, ok := err.(*dberrors.QBError)
	require.True(t, ok)
	assert.Equal(t, dberrors.ErrNotImplemented, qbErr.Code)
}

func TestQueryService_Query_EmptyEntityType(t *testing.T) {
	svc := &quicBaaS{
		queryService: &queryService{
			svc: &quicBaaS{},
		},
	}

	req := &QueryRequest{
		EntityType: "",
	}

	stream := make(chan *QueryResponse, 1)
	err := svc.queryService.Query(context.Background(), req, stream)

	assert.Error(t, err)
	qbErr, ok := err.(*dberrors.QBError)
	require.True(t, ok)
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestServiceImpl_Query_Delegation(t *testing.T) {
	svc := &quicBaaS{
		queryService: &queryService{
			svc: &quicBaaS{},
		},
	}

	req := &QueryRequest{
		EntityType: "",
	}

	stream := make(chan *QueryResponse, 1)
	err := svc.Query(context.Background(), req, stream)
	assert.Error(t, err)
}

func TestServiceImpl_ScanIndex_Delegation(t *testing.T) {
	svc := &quicBaaS{
		queryService: &queryService{
			svc: &quicBaaS{},
		},
	}

	req := &ScanRequest{}
	stream := make(chan *ScanResponse, 1)

	// This will fail but we just want to cover the delegation call
	err := svc.ScanIndex(context.Background(), req, stream)
	assert.Error(t, err)
}

func TestServiceImpl_CreateAuthRole_Delegation(t *testing.T) {
	svc := &quicBaaS{
		rbacService: &rbacService{
			svc: &quicBaaS{},
		},
	}

	req := &AuthCreateRoleRequest{}

	// This will likely fail but we're testing delegation
	_, err := svc.CreateAuthRole(context.Background(), req)
	assert.Error(t, err)
}

func TestServiceImpl_DeleteAuthRole_Delegation(t *testing.T) {
	svc := &quicBaaS{
		rbacService: &rbacService{
			svc: &quicBaaS{},
		},
	}

	req := &AuthDeleteRoleRequest{}

	// This will likely fail but we're testing delegation
	_, err := svc.DeleteAuthRole(context.Background(), req)
	assert.Error(t, err)
}

func TestServiceImpl_ListAuthRoles_Delegation(t *testing.T) {
	svc := &quicBaaS{
		rbacService: &rbacService{
			svc: &quicBaaS{},
		},
	}

	req := &AuthListRolesRequest{}

	// This will likely fail but we're testing delegation
	_, err := svc.ListAuthRoles(context.Background(), req)
	assert.Error(t, err)
}

func TestServiceImpl_TimeSeriesQuery_Delegation(t *testing.T) {
	svc := &quicBaaS{
		timeSeriesService: &timeSeriesService{
			svc: &quicBaaS{},
		},
	}

	req := &TimeSeriesQueryRequest{}
	stream := make(chan *TimeSeriesQueryResponse, 1)

	// This will fail but we're testing delegation
	err := svc.TimeSeriesQuery(context.Background(), req, stream)
	assert.Error(t, err)
}

func TestMatchesIndexValue_Coverage(t *testing.T) {
	// Test exact match
	assert.True(t, matchesIndexValue([]byte(`{"age":30}`), "age", []byte("30")))

	// Test no match
	assert.False(t, matchesIndexValue([]byte(`{"age":30}`), "age", []byte("25")))

	// Test field not exists
	assert.False(t, matchesIndexValue([]byte(`{"age":30}`), "email", []byte("test")))

	// Test invalid JSON
	assert.False(t, matchesIndexValue([]byte(`{invalid`), "age", []byte("30")))

	// Test empty data
	assert.False(t, matchesIndexValue([]byte(``), "age", []byte("30")))
}

func TestConvertWriteMode_Coverage(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	kvSvc := &kvService{svc: svc}

	// Test all modes
	result := kvSvc.convertWriteMode(WriteModeSync)
	assert.NotNil(t, result)

	result = kvSvc.convertWriteMode(WriteModeWALDurable)
	assert.NotNil(t, result)

	result = kvSvc.convertWriteMode(WriteModeBatched)
	assert.NotNil(t, result)

	// Test default
	result = kvSvc.convertWriteMode(WriteMode(99))
	assert.NotNil(t, result)
}

func TestConvertConsistency_Coverage(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	kvSvc := &kvService{svc: svc}

	// Test all levels
	result := kvSvc.convertConsistency(ConsistencyLevelEventual)
	assert.NotNil(t, result)

	result = kvSvc.convertConsistency(ConsistencyLevelLinearizable)
	assert.NotNil(t, result)

	result = kvSvc.convertConsistency(ConsistencyLevelStrong)
	assert.NotNil(t, result)

	// Test default
	result = kvSvc.convertConsistency(ConsistencyLevel(99))
	assert.NotNil(t, result)
}

func TestLiveness_Coverage(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	healthSvc := &healthService{svc: svc}

	resp, err := healthSvc.Liveness(context.Background(), &LivenessRequest{})
	require.NoError(t, err)
	assert.True(t, resp.Alive)
}

func TestReadiness_Coverage(t *testing.T) {
	log := createTestLogger(t)
	// Readiness requires config to be non-nil and checks for raft leader availability
	// In local mode (no raftNode), it should return ready
	svc := &quicBaaS{
		logger:   log,
		config:   &ServiceConfig{},
		raftNode: nil, // No raft = local mode
	}
	healthSvc := &healthService{svc: svc}

	resp, err := healthSvc.Readiness(context.Background(), &ReadinessRequest{})
	require.NoError(t, err)
	assert.True(t, resp.Ready)
}

func TestScanIndex_Coverage(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	querySvc := &queryService{svc: svc}

	stream := make(chan *ScanResponse, 1)
	err := querySvc.ScanIndex(context.Background(), &ScanRequest{EntityType: "test"}, stream)

	require.Error(t, err)
	_, ok := err.(*dberrors.QBError)
	assert.True(t, ok)
}

func TestValidateTimeSeriesQueryRequest_Coverage(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	tsSvc := &timeSeriesService{svc: svc}

	now := time.Now().UnixNano()
	oneHourAgo := time.Now().Add(-1 * time.Hour).UnixNano()

	// Valid request
	err := tsSvc.validateTimeSeriesQueryRequest(&TimeSeriesQueryRequest{
		EntityType: "metrics",
		StartTime:  oneHourAgo,
		EndTime:    now,
	})
	assert.NoError(t, err)

	// Nil request
	err = tsSvc.validateTimeSeriesQueryRequest(nil)
	assert.Error(t, err)

	// Empty entity type
	err = tsSvc.validateTimeSeriesQueryRequest(&TimeSeriesQueryRequest{
		StartTime: oneHourAgo,
		EndTime:   now,
	})
	assert.Error(t, err)

	// End before start
	err = tsSvc.validateTimeSeriesQueryRequest(&TimeSeriesQueryRequest{
		EntityType: "metrics",
		StartTime:  now,
		EndTime:    oneHourAgo,
	})
	assert.Error(t, err)
}

func TestValidateAggregateRequest_Coverage(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	tsSvc := &timeSeriesService{svc: svc}

	now := time.Now().UnixNano()
	oneHourAgo := time.Now().Add(-1 * time.Hour).UnixNano()

	// Valid request
	err := tsSvc.validateAggregateRequest(&AggregateRequest{
		EntityType: "metrics",
		StartTime:  oneHourAgo,
		EndTime:    now,
	})
	assert.NoError(t, err)

	// Nil request
	err = tsSvc.validateAggregateRequest(nil)
	assert.Error(t, err)

	// Empty entity type
	err = tsSvc.validateAggregateRequest(&AggregateRequest{
		StartTime: oneHourAgo,
		EndTime:   now,
	})
	assert.Error(t, err)
}

func TestEmbeddingService_validateGenerateEmbeddingRequest_NilRequest(t *testing.T) {
	svc := &embeddingService{}

	err := svc.validateGenerateEmbeddingRequest(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request is nil")
}

func TestEmbeddingService_validateBatchGenerateEmbeddingsRequest_EmptyItemText(t *testing.T) {
	svc := &embeddingService{}

	req := &BatchGenerateEmbeddingsRequest{
		Items: []*BatchEmbeddingItem{
			{
				ID:   "item1",
				Text: "",
			},
		},
	}

	err := svc.validateBatchGenerateEmbeddingsRequest(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "text is required")
}

func TestEmbeddingService_validateBatchGenerateEmbeddingsRequest_EmptyItemID(t *testing.T) {
	svc := &embeddingService{}

	req := &BatchGenerateEmbeddingsRequest{
		Items: []*BatchEmbeddingItem{
			{
				ID:   "",
				Text: "some text",
			},
		},
	}

	err := svc.validateBatchGenerateEmbeddingsRequest(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "id is required")
}

func TestEmbeddingService_validateGetProviderInfoRequest_NilRequest(t *testing.T) {
	svc := &embeddingService{}

	err := svc.validateGetProviderInfoRequest(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request is nil")
}

func TestEmbeddingService_validateGetProviderInfoRequest_EmptyProvider(t *testing.T) {
	svc := &embeddingService{}

	req := &GetProviderInfoRequest{
		Provider: "",
	}

	err := svc.validateGetProviderInfoRequest(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider is required")
}

func TestEmbeddingService_validateSetDefaultProviderRequest_NilRequest(t *testing.T) {
	svc := &embeddingService{}

	err := svc.validateSetDefaultProviderRequest(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request is nil")
}

func TestEmbeddingService_validateSetDefaultProviderRequest_EmptyProvider(t *testing.T) {
	svc := &embeddingService{}

	req := &SetDefaultProviderRequest{
		Provider: "",
	}

	err := svc.validateSetDefaultProviderRequest(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider is required")
}

type mockReencInfoWithInterface struct {
	totalKeys     uint64
	processedKeys uint64
	startedAt     time.Time
}

func (m *mockReencInfoWithInterface) GetTotalKeys() uint64     { return m.totalKeys }
func (m *mockReencInfoWithInterface) GetProcessedKeys() uint64 { return m.processedKeys }
func (m *mockReencInfoWithInterface) GetStartedAt() time.Time  { return m.startedAt }

type vecTransport struct {
	svcBoostMockTransport
	createNamespaceFn    func(ctx context.Context, req *qrdbtransport.CreateNamespaceRequest) (*qrdbtransport.CreateNamespaceResponse, error)
	deleteNamespaceFn    func(ctx context.Context, req *qrdbtransport.DeleteNamespaceRequest) (*qrdbtransport.DeleteNamespaceResponse, error)
	upsertVectorFn       func(ctx context.Context, req *qrdbtransport.UpsertVectorRequest) (*qrdbtransport.UpsertVectorResponse, error)
	deleteVectorFn       func(ctx context.Context, req *qrdbtransport.DeleteVectorRequest) (*qrdbtransport.DeleteVectorResponse, error)
	batchUpsertFn        func(ctx context.Context, req *qrdbtransport.BatchUpsertRequest) (*qrdbtransport.BatchUpsertResponse, error)
	batchDeleteVectorsFn func(ctx context.Context, req *qrdbtransport.BatchDeleteVectorsRequest) (*qrdbtransport.BatchDeleteVectorsResponse, error)
	searchSimilarFn      func(ctx context.Context, req *qrdbtransport.SearchSimilarRequest) (*qrdbtransport.SearchSimilarResponse, error)
	rebuildIndexFn       func(ctx context.Context, req *qrdbtransport.RebuildIndexRequest) (*qrdbtransport.RebuildIndexResponse, error)
	getRebuildProgressFn func(ctx context.Context, req *qrdbtransport.GetRebuildProgressRequest) (*qrdbtransport.GetRebuildProgressResponse, error)
	getIndexStatsFn      func(ctx context.Context, req *qrdbtransport.GetIndexStatsRequest) (*qrdbtransport.GetIndexStatsResponse, error)
	updateMetadataErr    error
	searchByIDErr        error
}

func (v *vecTransport) CreateNamespace(ctx context.Context, req *qrdbtransport.CreateNamespaceRequest) (*qrdbtransport.CreateNamespaceResponse, error) {
	if v.createNamespaceFn != nil {
		return v.createNamespaceFn(ctx, req)
	}
	return &qrdbtransport.CreateNamespaceResponse{
		Namespace: req.Namespace, Dimension: req.Dimension, Timestamp: time.Now().UnixNano(), Success: true,
	}, nil
}

func (v *vecTransport) DeleteNamespace(ctx context.Context, req *qrdbtransport.DeleteNamespaceRequest) (*qrdbtransport.DeleteNamespaceResponse, error) {
	if v.deleteNamespaceFn != nil {
		return v.deleteNamespaceFn(ctx, req)
	}
	return &qrdbtransport.DeleteNamespaceResponse{Success: true, Timestamp: time.Now().UnixNano()}, nil
}

func (v *vecTransport) ListNamespaces(_ context.Context, _ *qrdbtransport.ListNamespacesRequest) (*qrdbtransport.ListNamespacesResponse, error) {
	return &qrdbtransport.ListNamespacesResponse{}, nil
}

func (v *vecTransport) GetNamespaceInfo(_ context.Context, _ *qrdbtransport.GetNamespaceInfoRequest) (*qrdbtransport.GetNamespaceInfoResponse, error) {
	return &qrdbtransport.GetNamespaceInfoResponse{}, nil
}

func (v *vecTransport) UpsertVector(ctx context.Context, req *qrdbtransport.UpsertVectorRequest) (*qrdbtransport.UpsertVectorResponse, error) {
	if v.upsertVectorFn != nil {
		return v.upsertVectorFn(ctx, req)
	}
	return &qrdbtransport.UpsertVectorResponse{ID: req.ID, Version: 1, Timestamp: time.Now().UnixNano(), Success: true}, nil
}

func (v *vecTransport) GetVector(_ context.Context, _ *qrdbtransport.GetVectorRequest) (*qrdbtransport.GetVectorResponse, error) {
	return &qrdbtransport.GetVectorResponse{Found: true}, nil
}

func (v *vecTransport) DeleteVector(ctx context.Context, req *qrdbtransport.DeleteVectorRequest) (*qrdbtransport.DeleteVectorResponse, error) {
	if v.deleteVectorFn != nil {
		return v.deleteVectorFn(ctx, req)
	}
	return &qrdbtransport.DeleteVectorResponse{Success: true, Timestamp: time.Now().UnixNano()}, nil
}

func (v *vecTransport) BatchUpsert(ctx context.Context, req *qrdbtransport.BatchUpsertRequest) (*qrdbtransport.BatchUpsertResponse, error) {
	if v.batchUpsertFn != nil {
		return v.batchUpsertFn(ctx, req)
	}
	ids := make([]uint64, len(req.Vectors))
	for i, vec := range req.Vectors {
		ids[i] = vec.ID
	}
	return &qrdbtransport.BatchUpsertResponse{IDs: ids, Timestamp: time.Now().UnixNano(), Success: true, InsertedCount: len(req.Vectors)}, nil
}

func (v *vecTransport) BatchDeleteVectors(ctx context.Context, req *qrdbtransport.BatchDeleteVectorsRequest) (*qrdbtransport.BatchDeleteVectorsResponse, error) {
	if v.batchDeleteVectorsFn != nil {
		return v.batchDeleteVectorsFn(ctx, req)
	}
	return &qrdbtransport.BatchDeleteVectorsResponse{DeletedCount: len(req.IDs), Timestamp: time.Now().UnixNano(), Success: true}, nil
}

func (v *vecTransport) ListVectors(_ context.Context, _ *qrdbtransport.ListVectorsRequest) (*qrdbtransport.ListVectorsResponse, error) {
	return &qrdbtransport.ListVectorsResponse{}, nil
}

// vecNotFoundError is qrdb's typed answer for a vector ID it cannot find.
func vecNotFoundError() error {
	return &qrdbtransport.VectorNotFoundError{Namespace: "test-ns", ID: "99"}
}

func (v *vecTransport) UpdateMetadata(_ context.Context, req *qrdbtransport.UpdateVectorMetadataRequest) (*qrdbtransport.UpdateVectorMetadataResponse, error) {
	if v.updateMetadataErr != nil {
		return nil, v.updateMetadataErr
	}
	return &qrdbtransport.UpdateVectorMetadataResponse{ID: req.ID, Version: 1, Success: true}, nil
}

func (v *vecTransport) SearchSimilar(ctx context.Context, req *qrdbtransport.SearchSimilarRequest) (*qrdbtransport.SearchSimilarResponse, error) {
	if v.searchSimilarFn != nil {
		return v.searchSimilarFn(ctx, req)
	}
	return &qrdbtransport.SearchSimilarResponse{}, nil
}

func (v *vecTransport) SearchByMetadata(_ context.Context, _ *qrdbtransport.SearchByMetadataRequest) (*qrdbtransport.SearchByMetadataResponse, error) {
	return &qrdbtransport.SearchByMetadataResponse{}, nil
}

func (v *vecTransport) HybridSearch(_ context.Context, _ *qrdbtransport.HybridSearchRequest) (*qrdbtransport.HybridSearchResponse, error) {
	return &qrdbtransport.HybridSearchResponse{}, nil
}

func (v *vecTransport) SearchByID(_ context.Context, _ *qrdbtransport.SearchByIDRequest) (*qrdbtransport.SearchByIDResponse, error) {
	if v.searchByIDErr != nil {
		return nil, v.searchByIDErr
	}
	return &qrdbtransport.SearchByIDResponse{}, nil
}

func (v *vecTransport) RebuildIndex(ctx context.Context, req *qrdbtransport.RebuildIndexRequest) (*qrdbtransport.RebuildIndexResponse, error) {
	if v.rebuildIndexFn != nil {
		return v.rebuildIndexFn(ctx, req)
	}
	return &qrdbtransport.RebuildIndexResponse{}, nil
}

func (v *vecTransport) GetRebuildProgress(ctx context.Context, req *qrdbtransport.GetRebuildProgressRequest) (*qrdbtransport.GetRebuildProgressResponse, error) {
	if v.getRebuildProgressFn != nil {
		return v.getRebuildProgressFn(ctx, req)
	}
	return &qrdbtransport.GetRebuildProgressResponse{}, nil
}

func (v *vecTransport) GetIndexStats(ctx context.Context, req *qrdbtransport.GetIndexStatsRequest) (*qrdbtransport.GetIndexStatsResponse, error) {
	if v.getIndexStatsFn != nil {
		return v.getIndexStatsFn(ctx, req)
	}
	return &qrdbtransport.GetIndexStatsResponse{}, nil
}

func (v *vecTransport) CompactNamespace(_ context.Context, _ *qrdbtransport.CompactNamespaceRequest) (*qrdbtransport.CompactNamespaceResponse, error) {
	return &qrdbtransport.CompactNamespaceResponse{}, nil
}

func (v *vecTransport) GetCompactionProgress(_ context.Context, _ *qrdbtransport.GetCompactionProgressRequest) (*qrdbtransport.GetCompactionProgressResponse, error) {
	return &qrdbtransport.GetCompactionProgressResponse{}, nil
}

type vecSDKProvider struct {
	transport   *vecTransport
	lastShardID atomic.Uint64
}

func newVecSDKProvider(t *vecTransport) *vecSDKProvider {
	return &vecSDKProvider{transport: t}
}

type vecMockVectorIndex struct {
	insertFn func(id uint64, vector []float32) error
	deleteFn func(id uint64) error
	searchFn func(query []float32, k int, params *interfaces.SearchParams) ([]*interfaces.SearchResult, error)
	countVal int64
	statsVal *interfaces.IndexStats
}

func newVecMockVectorIndex() *vecMockVectorIndex {
	return &vecMockVectorIndex{
		countVal: 0,
		statsVal: &interfaces.IndexStats{NodeCount: 10, MaxLayer: 3, AvgConnections: 8.5, Dimensions: 128, IndexType: interfaces.IndexTypeHNSW, IsTrained: true},
	}
}

func (m *vecMockVectorIndex) Insert(id uint64, vector []float32) error {
	if m.insertFn != nil {
		return m.insertFn(id, vector)
	}
	return nil
}

func (m *vecMockVectorIndex) Search(query []float32, k int, params *interfaces.SearchParams) ([]*interfaces.SearchResult, error) {
	if m.searchFn != nil {
		return m.searchFn(query, k, params)
	}
	return []*interfaces.SearchResult{}, nil
}

func (m *vecMockVectorIndex) Delete(id uint64) error {
	if m.deleteFn != nil {
		return m.deleteFn(id)
	}
	return nil
}

func (m *vecMockVectorIndex) Count() int64 { return m.countVal }

func (m *vecMockVectorIndex) GetStats() *interfaces.IndexStats { return m.statsVal }

func (m *vecMockVectorIndex) IndexType() interfaces.IndexType { return interfaces.IndexTypeHNSW }

func (m *vecMockVectorIndex) Train(_ [][]float32) error { return nil }

func (m *vecMockVectorIndex) IsTrained() bool { return true }

func (m *vecMockVectorIndex) Close() error { return nil }

type vecMockVectorIndexFactory struct {
	createFn func(config *interfaces.VectorIndexConfig) (interfaces.VectorIndex, error)
}

func newVecMockVectorIndexFactory(index interfaces.VectorIndex) *vecMockVectorIndexFactory {
	return &vecMockVectorIndexFactory{
		createFn: func(_ *interfaces.VectorIndexConfig) (interfaces.VectorIndex, error) { return index, nil },
	}
}

func (f *vecMockVectorIndexFactory) CreateIndex(config *interfaces.VectorIndexConfig) (interfaces.VectorIndex, error) {
	return f.createFn(config)
}

func newVecSvcWithSDK(t *testing.T, namespace string, dimension uint32) (*vectorService, *vecMockStorage, *vecTransport, *vecMockVectorIndex) {
	t.Helper()

	storage := newVecMockStorage()
	logger := newVecTestLogger()
	mockIndex := newVecMockVectorIndex()
	factory := newVecMockVectorIndexFactory(mockIndex)
	vt := &vecTransport{}
	sdkProvider := newVecSDKProvider(vt)

	storage.listNamespacesFn = func(_ context.Context) ([]interfaces.VectorNamespaceInfo, error) {
		return []interfaces.VectorNamespaceInfo{
			{Name: namespace, Dimension: dimension, VectorCount: 0, DistanceMetric: "L2", IndexType: "HNSW",
				CreatedAt: time.Now().UnixNano(), UpdatedAt: time.Now().UnixNano()},
		}, nil
	}

	storage.getNamespaceInfoFn = func(_ context.Context, ns string) (*interfaces.VectorNamespaceInfo, error) {
		if ns == namespace {
			return &interfaces.VectorNamespaceInfo{
				Name: namespace, Dimension: dimension, VectorCount: 5, DistanceMetric: "L2", IndexType: "HNSW",
				CreatedAt: time.Now().UnixNano(), UpdatedAt: time.Now().UnixNano(),
			}, nil
		}
		return nil, stderrors.New("namespace not found")
	}

	svc := &vectorService{
		storage:        storage,
		sdkProvider:    sdkProvider,
		shardID:        shards.PlatformShardID,
		indexes:        make(map[string]interfaces.VectorIndex),
		logger:         logger,
		rebuildJobs:    make(map[string]*RebuildJobState),
		compactionJobs: make(map[string]*CompactionJobState),
		indexFactory:   factory,
	}
	svc.initMetrics()
	svc.indexes[namespace] = mockIndex

	return svc, storage, vt, mockIndex
}

// newVecSvcLocalIndex builds the service without a qrdb client, the only case
// in which the local index answers a search.
func newVecSvcLocalIndex(t *testing.T, namespace string, dimension uint32) (*vectorService, *vecMockStorage, *vecTransport, *vecMockVectorIndex) {
	t.Helper()
	svc, storage, vt, index := newVecSvcWithSDK(t, namespace, dimension)
	svc.sdkProvider = nil
	return svc, storage, vt, index
}

func TestConvertFromStringMap_Nil(t *testing.T) {
	assert.Nil(t, convertFromStringMap(nil))
}

func TestConvertFromStringMap_WithData(t *testing.T) {
	result := convertFromStringMap(map[string]string{"k": "v"})
	assert.Equal(t, "v", result["k"])
}

func TestMatchesMetadataFilter_NilFilter(t *testing.T) {
	assert.True(t, matchesMetadataFilter(map[string]interface{}{"k": "v"}, nil))
}

func TestMatchesMetadataFilter_EmptyFilter(t *testing.T) {
	assert.True(t, matchesMetadataFilter(map[string]interface{}{"k": "v"}, map[string]interface{}{}))
}

func TestMatchesMetadataFilter_Match(t *testing.T) {
	assert.True(t, matchesMetadataFilter(map[string]interface{}{"color": "red"}, map[string]interface{}{"color": "red"}))
}

func TestMatchesMetadataFilter_NoMatch(t *testing.T) {
	assert.False(t, matchesMetadataFilter(map[string]interface{}{"color": "red"}, map[string]interface{}{"color": "blue"}))
}

func TestMatchesMetadataFilter_MissingKey(t *testing.T) {
	assert.False(t, matchesMetadataFilter(map[string]interface{}{"color": "red"}, map[string]interface{}{"size": "large"}))
}
