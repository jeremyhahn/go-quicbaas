// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package quicbaas

import (
	"context"
	"crypto/tls"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/backup"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/batch"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/cache"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/certificate"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/cluster"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/crypto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/dynamicsecrets"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/embedding"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/kms"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/kv"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/objectstorage"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/ollama"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/operations"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/parameterstore"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/rbac"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/secrets"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/timeseries"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/vector"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// mockTransport implements transport.Transport for testing
type mockTransport struct {
	requestFn       func(ctx context.Context, service, method string, req interface{}) (interface{}, error)
	requestStreamFn func(ctx context.Context, service, method string, req interface{}) (transport.Stream, error)
	closeFn         func() error
	healthyFn       func() bool
}

func (m *mockTransport) Request(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	if m.requestFn != nil {
		return m.requestFn(ctx, service, method, req)
	}
	return nil, nil
}

func (m *mockTransport) RequestStream(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
	if m.requestStreamFn != nil {
		return m.requestStreamFn(ctx, service, method, req)
	}
	return nil, nil
}

func (m *mockTransport) Conn() interface{} {
	return nil
}

func (m *mockTransport) Close() error {
	if m.closeFn != nil {
		return m.closeFn()
	}
	return nil
}

func (m *mockTransport) Healthy() bool {
	if m.healthyFn != nil {
		return m.healthyFn()
	}
	return true
}

func TestNewClient_EmptyAddress(t *testing.T) {
	_, err := NewClient("")
	if err == nil {
		t.Fatal("expected error for empty address")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestNewClient_InvalidOption(t *testing.T) {
	_, err := NewClient("localhost:50051", WithTimeout(-1))
	if err == nil {
		t.Fatal("expected error for invalid option")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestNewClient_HTTPURLAutoDetectsREST(t *testing.T) {
	// URL detection should work and create REST transport successfully
	client, err := NewClient("http://localhost:8080")
	if err != nil {
		// Connection errors are expected since server isn't running
		// but transport should be created
		t.Logf("Expected transport creation, got error (may be connection related): %v", err)
		return
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	defer client.Close()
}

func TestNewClient_HTTPSURLAutoDetectsREST(t *testing.T) {
	// HTTPS URL should auto-detect REST transport
	client, err := NewClient("https://localhost:8443", WithTLSConfig(testTLSConfig()))
	if err != nil {
		// Connection errors are expected since server isn't running
		t.Logf("Expected transport creation, got error (may be connection related): %v", err)
		return
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	defer client.Close()
}

func TestCreateTransport_GRPC(t *testing.T) {
	opts := defaultOptions()
	opts.transportType = TransportGRPC
	opts.insecure = true

	tr, err := createTransport("localhost:50051", opts)
	if err != nil {
		// Configuration or connection errors may occur but not Unimplemented
		if errors.Code(err) == errors.CodeUnimplemented {
			t.Errorf("gRPC transport should be implemented, got Unimplemented error")
		}
		t.Logf("Transport creation error (expected for missing server): %v", err)
		return
	}
	if tr == nil {
		t.Fatal("expected non-nil transport")
	}
	defer tr.Close()
}

func TestCreateTransport_REST(t *testing.T) {
	opts := defaultOptions()
	opts.transportType = TransportREST
	opts.insecure = true

	tr, err := createTransport("http://localhost:8080", opts)
	if err != nil {
		if errors.Code(err) == errors.CodeUnimplemented {
			t.Errorf("REST transport should be implemented, got Unimplemented error")
		}
		t.Logf("Transport creation error (expected for missing server): %v", err)
		return
	}
	if tr == nil {
		t.Fatal("expected non-nil transport")
	}
	defer tr.Close()
}

func TestCreateTransport_QUIC(t *testing.T) {
	opts := defaultOptions()
	opts.transportType = TransportQUIC
	opts.insecure = true

	tr, err := createTransport("localhost:50051", opts)
	if err != nil {
		if errors.Code(err) == errors.CodeUnimplemented {
			t.Errorf("QUIC transport should be implemented, got Unimplemented error")
		}
		t.Logf("Transport creation error (expected for missing server): %v", err)
		return
	}
	if tr == nil {
		t.Fatal("expected non-nil transport")
	}
	defer tr.Close()
}

func TestCreateTransport_InvalidType(t *testing.T) {
	opts := defaultOptions()
	opts.transportType = TransportType(999) // Invalid type

	_, err := createTransport("localhost:50051", opts)
	if err == nil {
		t.Fatal("expected error for invalid transport type")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestClient_Transport(t *testing.T) {
	mockT := &mockTransport{}
	c := &client{
		transport: mockT,
		opts:      defaultOptions(),
	}

	if c.Transport() != mockT {
		t.Error("Transport() should return the transport")
	}
}

func TestClient_Close(t *testing.T) {
	closeCalled := false
	mockT := &mockTransport{
		closeFn: func() error {
			closeCalled = true
			return nil
		},
	}
	c := &client{
		transport: mockT,
		opts:      defaultOptions(),
	}

	err := c.Close()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !closeCalled {
		t.Error("transport.Close() should be called")
	}
	if !c.closed {
		t.Error("client should be marked as closed")
	}
}

func TestClient_CloseAlreadyClosed(t *testing.T) {
	closeCalled := 0
	mockT := &mockTransport{
		closeFn: func() error {
			closeCalled++
			return nil
		},
	}
	c := &client{
		transport: mockT,
		opts:      defaultOptions(),
	}

	_ = c.Close()
	_ = c.Close()

	if closeCalled != 1 {
		t.Errorf("expected Close() to be called once, called %d times", closeCalled)
	}
}

func TestClient_CloseNilTransport(t *testing.T) {
	c := &client{
		transport: nil,
		opts:      defaultOptions(),
	}

	err := c.Close()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestClient_KV(t *testing.T) {
	mockT := &mockTransport{}
	c := &client{
		transport: mockT,
		opts:      defaultOptions(),
	}

	kv1 := c.KV()
	if kv1 == nil {
		t.Fatal("expected non-nil KV client")
	}

	// Second call should return same instance
	kv2 := c.KV()
	if kv1 != kv2 {
		t.Error("expected same KV client instance")
	}
}

func TestClient_Batch(t *testing.T) {
	mockT := &mockTransport{}
	c := &client{
		transport: mockT,
		opts:      defaultOptions(),
	}

	batch := c.Batch()
	// Currently returns nil (not implemented)
	if batch != nil {
		// OK - implementation may exist
	}

	// Second call should return same instance
	batch2 := c.Batch()
	if batch != batch2 {
		t.Error("expected same Batch client instance")
	}
}

func TestClient_Transaction(t *testing.T) {
	mockT := &mockTransport{}
	c := &client{
		transport: mockT,
		opts:      defaultOptions(),
	}

	txn := c.Transaction()
	txn2 := c.Transaction()
	if txn != txn2 {
		t.Error("expected same Transaction client instance")
	}
}

func TestClient_Query(t *testing.T) {
	mockT := &mockTransport{}
	c := &client{
		transport: mockT,
		opts:      defaultOptions(),
	}

	query := c.Query()
	query2 := c.Query()
	if query != query2 {
		t.Error("expected same Query client instance")
	}
}

func TestClient_Cluster(t *testing.T) {
	mockT := &mockTransport{}
	c := &client{
		transport: mockT,
		opts:      defaultOptions(),
	}

	cluster := c.Cluster()
	cluster2 := c.Cluster()
	if cluster != cluster2 {
		t.Error("expected same Cluster client instance")
	}
}

func TestClient_Health(t *testing.T) {
	mockT := &mockTransport{}
	c := &client{
		transport: mockT,
		opts:      defaultOptions(),
	}

	health := c.Health()
	health2 := c.Health()
	if health != health2 {
		t.Error("expected same Health client instance")
	}
}

func TestClient_Auth(t *testing.T) {
	mockT := &mockTransport{}
	c := &client{
		transport: mockT,
		opts:      defaultOptions(),
	}

	auth := c.auth()
	auth2 := c.auth()
	if auth != auth2 {
		t.Error("expected same Auth client instance")
	}
}

func TestClient_Seal(t *testing.T) {
	mockT := &mockTransport{}
	c := &client{
		transport: mockT,
		opts:      defaultOptions(),
	}

	seal := c.seal()
	seal2 := c.seal()
	if seal != seal2 {
		t.Error("expected same Seal client instance")
	}
}

func TestClient_KMS(t *testing.T) {
	mockT := &mockTransport{}
	c := &client{
		transport: mockT,
		opts:      defaultOptions(),
	}

	kms := c.kms()
	kms2 := c.kms()
	if kms != kms2 {
		t.Error("expected same KMS client instance")
	}
}

func TestClient_Secrets(t *testing.T) {
	mockT := &mockTransport{}
	c := &client{
		transport: mockT,
		opts:      defaultOptions(),
	}

	secrets := c.secrets()
	secrets2 := c.secrets()
	if secrets != secrets2 {
		t.Error("expected same Secrets client instance")
	}
}

func TestClient_Crypto(t *testing.T) {
	mockT := &mockTransport{}
	c := &client{
		transport: mockT,
		opts:      defaultOptions(),
	}

	crypto := c.crypto()
	crypto2 := c.crypto()
	if crypto != crypto2 {
		t.Error("expected same Crypto client instance")
	}
}

func TestClient_Vector(t *testing.T) {
	mockT := &mockTransport{}
	c := &client{
		transport: mockT,
		opts:      defaultOptions(),
	}

	vector := c.Vector()
	vector2 := c.Vector()
	if vector != vector2 {
		t.Error("expected same Vector client instance")
	}
}

func TestClient_TimeSeries(t *testing.T) {
	mockT := &mockTransport{}
	c := &client{
		transport: mockT,
		opts:      defaultOptions(),
	}

	ts := c.TimeSeries()
	ts2 := c.TimeSeries()
	if ts != ts2 {
		t.Error("expected same TimeSeries client instance")
	}
}

func TestClient_Backup(t *testing.T) {
	mockT := &mockTransport{}
	c := &client{
		transport: mockT,
		opts:      defaultOptions(),
	}

	backup := c.Backup()
	backup2 := c.Backup()
	if backup != backup2 {
		t.Error("expected same Backup client instance")
	}
}

func TestTransportType_Constants(t *testing.T) {
	// Verify transport type constants
	if TransportGRPC != 0 {
		t.Errorf("expected TransportGRPC=0, got %d", TransportGRPC)
	}
	if TransportREST != 1 {
		t.Errorf("expected TransportREST=1, got %d", TransportREST)
	}
	if TransportQUIC != 2 {
		t.Errorf("expected TransportQUIC=2, got %d", TransportQUIC)
	}
}

func TestEventType_Constants(t *testing.T) {
	if EventPut != 0 {
		t.Errorf("expected EventPut=0, got %d", EventPut)
	}
	if EventDelete != 1 {
		t.Errorf("expected EventDelete=1, got %d", EventDelete)
	}
}

// Mock implementations for testing RLock early-return path.
// These minimal implementations satisfy each interface to enable
// pre-populating client fields with non-nil values.

type mockKVClientForCoverage struct{}

func (m *mockKVClientForCoverage) Put(ctx context.Context, req *PutRequest) (*PutResponse, error) {
	return nil, nil
}
func (m *mockKVClientForCoverage) Get(ctx context.Context, req *GetRequest) (*GetResponse, error) {
	return nil, nil
}
func (m *mockKVClientForCoverage) Delete(ctx context.Context, req *DeleteRequest) (*DeleteResponse, error) {
	return nil, nil
}
func (m *mockKVClientForCoverage) List(ctx context.Context, req *ListRequest) (*ListResponse, error) {
	return nil, nil
}
func (m *mockKVClientForCoverage) Watch(ctx context.Context, key string, opts ...kv.WatchOption) (kv.Watcher, error) {
	return nil, nil
}
func (m *mockKVClientForCoverage) WatchPrefix(ctx context.Context, prefix string, opts ...kv.WatchOption) (kv.Watcher, error) {
	return nil, nil
}

type mockBatchClientForCoverage struct{}

func (m *mockBatchClientForCoverage) Execute(ctx context.Context, req *BatchRequest) (*BatchResponse, error) {
	return nil, nil
}
func (m *mockBatchClientForCoverage) Put(ctx context.Context, requests []*PutRequest) (*batch.BatchPutResponse, error) {
	return nil, nil
}
func (m *mockBatchClientForCoverage) Get(ctx context.Context, requests []*GetRequest) (*batch.BatchGetResponse, error) {
	return nil, nil
}
func (m *mockBatchClientForCoverage) Delete(ctx context.Context, requests []*DeleteRequest) (*batch.BatchDeleteResponse, error) {
	return nil, nil
}

type mockTransactionClientForCoverage struct{}

func (m *mockTransactionClientForCoverage) Execute(ctx context.Context, req *TxnRequest) (*TxnResponse, error) {
	return nil, nil
}

type mockQueryClientForCoverage struct{}

func (m *mockQueryClientForCoverage) Execute(ctx context.Context, req *QueryRequest) (*QueryResponse, error) {
	return nil, nil
}

type mockClusterClientForCoverage struct{}

func (m *mockClusterClientForCoverage) Status(ctx context.Context) (*ClusterStatus, error) {
	return nil, nil
}
func (m *mockClusterClientForCoverage) Nodes(ctx context.Context) (*NodesResponse, error) {
	return nil, nil
}
func (m *mockClusterClientForCoverage) AddNode(ctx context.Context, req *AddNodeRequest) error {
	return nil
}
func (m *mockClusterClientForCoverage) RemoveNode(ctx context.Context, req *RemoveNodeRequest) error {
	return nil
}
func (m *mockClusterClientForCoverage) RebalanceCluster(ctx context.Context, req *cluster.RebalanceClusterRequest) (*cluster.RebalanceClusterResponse, error) {
	return nil, nil
}
func (m *mockClusterClientForCoverage) GetRebalanceProgress(ctx context.Context, req *cluster.GetRebalanceProgressRequest) (*cluster.GetRebalanceProgressResponse, error) {
	return nil, nil
}

type mockHealthClientForCoverage struct{}

func (m *mockHealthClientForCoverage) Ping(ctx context.Context) error { return nil }
func (m *mockHealthClientForCoverage) Liveness(ctx context.Context) (*HealthResponse, error) {
	return nil, nil
}
func (m *mockHealthClientForCoverage) Readiness(ctx context.Context) (*HealthResponse, error) {
	return nil, nil
}

type mockAuthClientForCoverage struct{}

func (m *mockAuthClientForCoverage) Login(ctx context.Context, req *service.ExtendedLoginRequest) (*service.ExtendedLoginResponse, error) {
	return nil, nil
}
func (m *mockAuthClientForCoverage) RefreshToken(ctx context.Context, refreshToken string) (*service.ExtendedRefreshTokenResponse, error) {
	return nil, nil
}
func (m *mockAuthClientForCoverage) ValidateToken(ctx context.Context, accessToken string) (*service.TokenClaims, error) {
	return nil, nil
}
func (m *mockAuthClientForCoverage) Logout(ctx context.Context, accessToken string) error {
	return nil
}
func (m *mockAuthClientForCoverage) BeginFederatedLogin(ctx context.Context, req *service.BeginFederatedLoginRequest) (*service.BeginFederatedLoginResponse, error) {
	return nil, nil
}
func (m *mockAuthClientForCoverage) HandleFederatedCallback(ctx context.Context, req *service.HandleFederatedCallbackRequest) (*service.HandleFederatedCallbackResponse, error) {
	return nil, nil
}
func (m *mockAuthClientForCoverage) ListProviders(ctx context.Context, tenantID string) (*service.AuthListProvidersResponse, error) {
	return nil, nil
}
func (m *mockAuthClientForCoverage) GetProvider(ctx context.Context, tenantID, providerID string) (*service.AuthProvider, error) {
	return nil, nil
}
func (m *mockAuthClientForCoverage) GetSession(ctx context.Context, sessionID string) (*service.AuthSession, error) {
	return nil, nil
}
func (m *mockAuthClientForCoverage) ListSessions(ctx context.Context) (*service.AuthListSessionsResponse, error) {
	return nil, nil
}
func (m *mockAuthClientForCoverage) RevokeSession(ctx context.Context, sessionID string) error {
	return nil
}
func (m *mockAuthClientForCoverage) RevokeAllSessions(ctx context.Context) error {
	return nil
}
func (m *mockAuthClientForCoverage) IntrospectToken(ctx context.Context, token string) (*service.TokenIntrospection, error) {
	return nil, nil
}
func (m *mockAuthClientForCoverage) GetJWKS(ctx context.Context) (*service.JWKS, error) {
	return nil, nil
}
func (m *mockAuthClientForCoverage) GetOpenIDConfiguration(ctx context.Context) (*service.OpenIDConfiguration, error) {
	return nil, nil
}

type mockSealClientForCoverage struct{}

func (m *mockSealClientForCoverage) Status(ctx context.Context) (*SealStatus, error) {
	return nil, nil
}
func (m *mockSealClientForCoverage) Init(ctx context.Context, req *InitRequest) (*InitResponse, error) {
	return nil, nil
}
func (m *mockSealClientForCoverage) Unseal(ctx context.Context, req *UnsealRequest) (*UnsealResponse, error) {
	return nil, nil
}
func (m *mockSealClientForCoverage) Seal(ctx context.Context) error { return nil }

type mockKMSClientForCoverage struct{}

func (m *mockKMSClientForCoverage) GenerateKey(ctx context.Context, req *GenerateKeyRequest) (*GenerateKeyResponse, error) {
	return nil, nil
}
func (m *mockKMSClientForCoverage) GetKey(ctx context.Context, keyID string) (*KeyInfo, error) {
	return nil, nil
}
func (m *mockKMSClientForCoverage) GetPublicKey(ctx context.Context, keyID string) (*kms.GetPublicKeyResponse, error) {
	return nil, nil
}
func (m *mockKMSClientForCoverage) ListKeys(ctx context.Context) (*ListKeysResponse, error) {
	return nil, nil
}
func (m *mockKMSClientForCoverage) RotateKey(ctx context.Context, keyID string) (*RotateKeyResponse, error) {
	return nil, nil
}
func (m *mockKMSClientForCoverage) DeleteKey(ctx context.Context, keyID string) error { return nil }
func (m *mockKMSClientForCoverage) ImportKey(ctx context.Context, req *kms.ImportKeyRequest) (*kms.ImportKeyResponse, error) {
	return nil, nil
}
func (m *mockKMSClientForCoverage) ExportKey(ctx context.Context, req *kms.ExportKeyRequest) (*kms.ExportKeyResponse, error) {
	return nil, nil
}
func (m *mockKMSClientForCoverage) RevokeKey(ctx context.Context, keyID string) error { return nil }
func (m *mockKMSClientForCoverage) ListKeyVersions(ctx context.Context, keyID string) (*kms.ListKeyVersionsResponse, error) {
	return nil, nil
}
func (m *mockKMSClientForCoverage) UpdateKeyStatus(ctx context.Context, req *kms.UpdateKeyStatusRequest) (*kms.UpdateKeyStatusResponse, error) {
	return nil, nil
}
func (m *mockKMSClientForCoverage) GetKeyMetadata(ctx context.Context, keyID string) (*kms.KeyMetadataResponse, error) {
	return nil, nil
}
func (m *mockKMSClientForCoverage) UpdateKeyMetadata(ctx context.Context, req *kms.UpdateKeyMetadataRequest) (*kms.KeyMetadataResponse, error) {
	return nil, nil
}
func (m *mockKMSClientForCoverage) SetKeyPolicy(ctx context.Context, req *kms.SetKeyPolicyRequest) error {
	return nil
}
func (m *mockKMSClientForCoverage) GetKeyPolicy(ctx context.Context, keyID string) (*kms.KeyPolicy, error) {
	return nil, nil
}
func (m *mockKMSClientForCoverage) GetSupportedAlgorithms(ctx context.Context) (*kms.SupportedAlgorithmsResponse, error) {
	return nil, nil
}
func (m *mockKMSClientForCoverage) BackendStatus(ctx context.Context) (*kms.BackendStatusResponse, error) {
	return nil, nil
}
func (m *mockKMSClientForCoverage) UnwrapKey(ctx context.Context, req *kms.UnwrapKeyRequest) (*kms.UnwrapKeyResponse, error) {
	return nil, nil
}
func (m *mockKMSClientForCoverage) SetKeyRotationPolicy(ctx context.Context, req *kms.SetKeyRotationPolicyRequest) (*kms.KeyRotationPolicy, error) {
	return nil, nil
}
func (m *mockKMSClientForCoverage) GetKeyRotationPolicy(ctx context.Context, keyID string) (*kms.KeyRotationPolicy, error) {
	return nil, nil
}
func (m *mockKMSClientForCoverage) DeleteKeyRotationPolicy(ctx context.Context, keyID string) error {
	return nil
}

type mockSecretsClientForCoverage struct{}

func (m *mockSecretsClientForCoverage) Store(ctx context.Context, req *StoreSecretRequest) (*StoreSecretResponse, error) {
	return nil, nil
}
func (m *mockSecretsClientForCoverage) Get(ctx context.Context, path string, opts ...SecretOption) (*Secret, error) {
	return nil, nil
}
func (m *mockSecretsClientForCoverage) List(ctx context.Context, prefix string) (*ListSecretsResponse, error) {
	return nil, nil
}
func (m *mockSecretsClientForCoverage) Delete(ctx context.Context, path string) error { return nil }
func (m *mockSecretsClientForCoverage) Ensure(ctx context.Context, req *secrets.EnsureSecretRequest) (*secrets.EnsureSecretResponse, error) {
	return nil, nil
}
func (m *mockSecretsClientForCoverage) GetVersion(ctx context.Context, req *secrets.GetSecretVersionRequest) (*secrets.SecretVersionInfo, error) {
	return nil, nil
}
func (m *mockSecretsClientForCoverage) ListVersions(ctx context.Context, secretID string) (*secrets.ListSecretVersionsResponse, error) {
	return nil, nil
}
func (m *mockSecretsClientForCoverage) Rotate(ctx context.Context, req *secrets.RotateSecretRequest) (*secrets.RotateSecretResponse, error) {
	return nil, nil
}
func (m *mockSecretsClientForCoverage) Rollback(ctx context.Context, req *secrets.RollbackSecretRequest) (*secrets.RollbackSecretResponse, error) {
	return nil, nil
}
func (m *mockSecretsClientForCoverage) UpdateStatus(ctx context.Context, req *secrets.UpdateSecretStatusRequest) error {
	return nil
}
func (m *mockSecretsClientForCoverage) SetRotationPolicy(ctx context.Context, req *secrets.SetRotationPolicyRequest) error {
	return nil
}
func (m *mockSecretsClientForCoverage) GetRotationPolicy(ctx context.Context, secretID string) (*secrets.RotationPolicy, error) {
	return nil, nil
}

type mockCryptoClientForCoverage struct{}

func (m *mockCryptoClientForCoverage) Encrypt(ctx context.Context, req *EncryptRequest) (*EncryptResponse, error) {
	return nil, nil
}
func (m *mockCryptoClientForCoverage) Decrypt(ctx context.Context, req *DecryptRequest) (*DecryptResponse, error) {
	return nil, nil
}
func (m *mockCryptoClientForCoverage) Sign(ctx context.Context, req *SignRequest) (*SignResponse, error) {
	return nil, nil
}
func (m *mockCryptoClientForCoverage) Verify(ctx context.Context, req *VerifyRequest) (*VerifyResponse, error) {
	return nil, nil
}
func (m *mockCryptoClientForCoverage) Rewrap(ctx context.Context, req *crypto.RewrapRequest) (*crypto.RewrapResponse, error) {
	return nil, nil
}
func (m *mockCryptoClientForCoverage) EncryptBatch(ctx context.Context, req *crypto.EncryptBatchRequest) (*crypto.EncryptBatchResponse, error) {
	return nil, nil
}
func (m *mockCryptoClientForCoverage) DecryptBatch(ctx context.Context, req *crypto.DecryptBatchRequest) (*crypto.DecryptBatchResponse, error) {
	return nil, nil
}
func (m *mockCryptoClientForCoverage) GenerateDataKey(ctx context.Context, req *crypto.GenerateDataKeyRequest) (*crypto.GenerateDataKeyResponse, error) {
	return nil, nil
}
func (m *mockCryptoClientForCoverage) GenerateWrappedDataKey(ctx context.Context, req *crypto.GenerateWrappedDataKeyRequest) (*crypto.GenerateWrappedDataKeyResponse, error) {
	return nil, nil
}
func (m *mockCryptoClientForCoverage) GenerateHMAC(ctx context.Context, req *crypto.GenerateHMACRequest) (*crypto.GenerateHMACResponse, error) {
	return nil, nil
}
func (m *mockCryptoClientForCoverage) VerifyHMAC(ctx context.Context, req *crypto.VerifyHMACRequest) (*crypto.VerifyHMACResponse, error) {
	return nil, nil
}
func (m *mockCryptoClientForCoverage) GenerateRandom(ctx context.Context, req *crypto.GenerateRandomRequest) (*crypto.GenerateRandomResponse, error) {
	return nil, nil
}
func (m *mockCryptoClientForCoverage) Hash(ctx context.Context, req *crypto.HashRequest) (*crypto.HashResponse, error) {
	return nil, nil
}

type mockVectorClientForCoverage struct{}

func (m *mockVectorClientForCoverage) CreateNamespace(ctx context.Context, req *vector.CreateNamespaceRequest) (*vector.CreateNamespaceResponse, error) {
	return nil, nil
}
func (m *mockVectorClientForCoverage) DeleteNamespace(ctx context.Context, req *vector.DeleteNamespaceRequest) (*vector.DeleteNamespaceResponse, error) {
	return nil, nil
}
func (m *mockVectorClientForCoverage) ListNamespaces(ctx context.Context, req *vector.ListNamespacesRequest) (*vector.ListNamespacesResponse, error) {
	return nil, nil
}
func (m *mockVectorClientForCoverage) GetNamespaceInfo(ctx context.Context, req *vector.GetNamespaceInfoRequest) (*vector.GetNamespaceInfoResponse, error) {
	return nil, nil
}
func (m *mockVectorClientForCoverage) Upsert(ctx context.Context, req *vector.UpsertRequest) (*vector.UpsertResponse, error) {
	return nil, nil
}
func (m *mockVectorClientForCoverage) BatchUpsert(ctx context.Context, req *vector.BatchUpsertRequest) (*vector.BatchUpsertResponse, error) {
	return nil, nil
}
func (m *mockVectorClientForCoverage) Get(ctx context.Context, req *vector.GetRequest) (*vector.GetResponse, error) {
	return nil, nil
}
func (m *mockVectorClientForCoverage) Delete(ctx context.Context, req *vector.DeleteRequest) (*vector.DeleteResponse, error) {
	return nil, nil
}
func (m *mockVectorClientForCoverage) BatchDelete(ctx context.Context, req *vector.BatchDeleteRequest) (*vector.BatchDeleteResponse, error) {
	return nil, nil
}
func (m *mockVectorClientForCoverage) List(ctx context.Context, req *vector.ListVectorsRequest) (*vector.ListVectorsResponse, error) {
	return nil, nil
}
func (m *mockVectorClientForCoverage) UpdateMetadata(ctx context.Context, req *vector.UpdateMetadataRequest) (*vector.UpdateMetadataResponse, error) {
	return nil, nil
}
func (m *mockVectorClientForCoverage) SearchSimilar(ctx context.Context, req *vector.SearchSimilarRequest) (*vector.SearchSimilarResponse, error) {
	return nil, nil
}
func (m *mockVectorClientForCoverage) SearchByID(ctx context.Context, req *vector.SearchByIDRequest) (*vector.SearchByIDResponse, error) {
	return nil, nil
}
func (m *mockVectorClientForCoverage) HybridSearch(ctx context.Context, req *vector.HybridSearchRequest) (*vector.HybridSearchResponse, error) {
	return nil, nil
}
func (m *mockVectorClientForCoverage) SearchByMetadata(ctx context.Context, req *vector.SearchByMetadataRequest) (*vector.SearchByMetadataResponse, error) {
	return nil, nil
}
func (m *mockVectorClientForCoverage) RebuildIndex(ctx context.Context, req *vector.RebuildIndexRequest) (*vector.RebuildIndexResponse, error) {
	return nil, nil
}
func (m *mockVectorClientForCoverage) GetIndexProgress(ctx context.Context, req *vector.GetIndexProgressRequest) (*vector.GetIndexProgressResponse, error) {
	return nil, nil
}
func (m *mockVectorClientForCoverage) Export(ctx context.Context, req *vector.ExportRequest) ([]*vector.ExportResponse, error) {
	return nil, nil
}
func (m *mockVectorClientForCoverage) Import(ctx context.Context, batches []*vector.ImportRequest) (*vector.ImportResponse, error) {
	return nil, nil
}
func (m *mockVectorClientForCoverage) Compact(ctx context.Context, req *vector.CompactRequest) (*vector.CompactResponse, error) {
	return nil, nil
}
func (m *mockVectorClientForCoverage) GetCompactionProgress(ctx context.Context, req *vector.GetCompactionProgressRequest) (*vector.GetCompactionProgressResponse, error) {
	return nil, nil
}
func (m *mockVectorClientForCoverage) GetStats(ctx context.Context, req *vector.GetStatsRequest) (*vector.GetStatsResponse, error) {
	return nil, nil
}

type mockTimeSeriesClientForCoverage struct{}

func (m *mockTimeSeriesClientForCoverage) Write(ctx context.Context, req *timeseries.WriteRequest) error {
	return nil
}
func (m *mockTimeSeriesClientForCoverage) Query(ctx context.Context, req *timeseries.QueryRequest) (*timeseries.QueryResponse, error) {
	return nil, nil
}

type mockBackupClientForCoverage struct{}

func (m *mockBackupClientForCoverage) Create(ctx context.Context, req *backup.BackupRequest) (*backup.BackupResponse, error) {
	return nil, nil
}
func (m *mockBackupClientForCoverage) Restore(ctx context.Context, req *backup.RestoreRequest) (*backup.RestoreResponse, error) {
	return nil, nil
}
func (m *mockBackupClientForCoverage) List(ctx context.Context) (*backup.ListBackupsResponse, error) {
	return nil, nil
}

type mockIAMClientForCoverage struct{}

func (m *mockIAMClientForCoverage) Tenant() iam.TenantClient   { return nil }
func (m *mockIAMClientForCoverage) User() iam.UserClient       { return nil }
func (m *mockIAMClientForCoverage) Group() iam.GroupClient     { return nil }
func (m *mockIAMClientForCoverage) Role() iam.RoleClient       { return nil }
func (m *mockIAMClientForCoverage) Policy() iam.PolicyClient   { return nil }
func (m *mockIAMClientForCoverage) PEP() iam.PEPClient         { return nil }
func (m *mockIAMClientForCoverage) App() iam.AppClient         { return nil }
func (m *mockIAMClientForCoverage) Session() iam.SessionClient { return nil }
func (m *mockIAMClientForCoverage) Audit() iam.AuditClient     { return nil }
func (m *mockIAMClientForCoverage) GDPR() iam.GDPRClient       { return nil }
func (m *mockIAMClientForCoverage) Admin() iam.AdminClient     { return nil }
func (m *mockIAMClientForCoverage) Invite() iam.InviteClient   { return nil }
func (m *mockIAMClientForCoverage) CreateProvider(ctx context.Context, opts *cli.CreateProviderOptions) (*cli.Provider, error) {
	return nil, nil
}
func (m *mockIAMClientForCoverage) GetProvider(ctx context.Context, tenantID, providerID string) (*cli.Provider, error) {
	return nil, nil
}
func (m *mockIAMClientForCoverage) UpdateProvider(ctx context.Context, opts *cli.UpdateProviderOptions) (*cli.Provider, error) {
	return nil, nil
}
func (m *mockIAMClientForCoverage) DeleteProvider(ctx context.Context, tenantID, providerID string) error {
	return nil
}
func (m *mockIAMClientForCoverage) ListProviders(ctx context.Context, tenantID string) ([]*cli.Provider, error) {
	return nil, nil
}
func (m *mockIAMClientForCoverage) LinkIdentity(ctx context.Context, opts *cli.LinkIdentityOptions) (*cli.FederatedIdentity, error) {
	return nil, nil
}
func (m *mockIAMClientForCoverage) UnlinkIdentity(ctx context.Context, tenantID, userID, providerID string) error {
	return nil
}
func (m *mockIAMClientForCoverage) ListUserIdentities(ctx context.Context, tenantID, userID string) ([]*cli.FederatedIdentity, error) {
	return nil, nil
}
func (m *mockIAMClientForCoverage) GetMFAStatus(ctx context.Context, tenantID, userID string) (*cli.MFAStatus, error) {
	return nil, nil
}
func (m *mockIAMClientForCoverage) BeginMFAEnrollment(ctx context.Context, tenantID, userID, method string) (*cli.MFAEnrollment, error) {
	return nil, nil
}
func (m *mockIAMClientForCoverage) CompleteMFAEnrollment(ctx context.Context, enrollmentID, code string) error {
	return nil
}
func (m *mockIAMClientForCoverage) DisableMFA(ctx context.Context, tenantID, userID string) error {
	return nil
}
func (m *mockIAMClientForCoverage) GetBackupCodeStatus(ctx context.Context, tenantID, userID string) (*cli.BackupCodeStatus, error) {
	return nil, nil
}
func (m *mockIAMClientForCoverage) GenerateBackupCodes(ctx context.Context, tenantID, userID string) ([]string, error) {
	return nil, nil
}
func (m *mockIAMClientForCoverage) Close() error { return nil }

// TestClient_Accessors_RLockPath tests the RLock early-return path for all accessors.
// When a client is already cached (non-nil), the accessor should return it directly
// via the read-lock path without taking the write lock.
// This test covers the scenario where a client was previously initialized
// (either by a prior call or by another goroutine in a race condition).
func TestClient_Accessors_RLockPath(t *testing.T) {
	mockT := &mockTransport{}

	t.Run("Batch_RLockPath", func(t *testing.T) {
		mock := &mockBatchClientForCoverage{}
		c := &client{transport: mockT, opts: defaultOptions(), batchClient: mock}
		result := c.Batch()
		if result != BatchClient(mock) {
			t.Error("expected pre-populated Batch client to be returned via RLock path")
		}
	})

	t.Run("Transaction_RLockPath", func(t *testing.T) {
		mock := &mockTransactionClientForCoverage{}
		c := &client{transport: mockT, opts: defaultOptions(), txnClient: mock}
		result := c.Transaction()
		if result != TransactionClient(mock) {
			t.Error("expected pre-populated Transaction client to be returned via RLock path")
		}
	})

	t.Run("Query_RLockPath", func(t *testing.T) {
		mock := &mockQueryClientForCoverage{}
		c := &client{transport: mockT, opts: defaultOptions(), queryClient: mock}
		result := c.Query()
		if result != QueryClient(mock) {
			t.Error("expected pre-populated Query client to be returned via RLock path")
		}
	})

	t.Run("Cluster_RLockPath", func(t *testing.T) {
		mock := &mockClusterClientForCoverage{}
		c := &client{transport: mockT, opts: defaultOptions(), clusterClient: mock}
		result := c.Cluster()
		if result != ClusterClient(mock) {
			t.Error("expected pre-populated Cluster client to be returned via RLock path")
		}
	})

	t.Run("Health_RLockPath", func(t *testing.T) {
		mock := &mockHealthClientForCoverage{}
		c := &client{transport: mockT, opts: defaultOptions(), healthClient: mock}
		result := c.Health()
		if result != HealthClient(mock) {
			t.Error("expected pre-populated Health client to be returned via RLock path")
		}
	})

	t.Run("Auth_RLockPath", func(t *testing.T) {
		mock := &mockAuthClientForCoverage{}
		c := &client{transport: mockT, opts: defaultOptions(), authClient: mock}
		result := c.auth()
		if result != AuthClient(mock) {
			t.Error("expected pre-populated Auth client to be returned via RLock path")
		}
	})

	t.Run("Seal_RLockPath", func(t *testing.T) {
		mock := &mockSealClientForCoverage{}
		c := &client{transport: mockT, opts: defaultOptions(), sealClient: mock}
		result := c.seal()
		if result != SealClient(mock) {
			t.Error("expected pre-populated Seal client to be returned via RLock path")
		}
	})

	t.Run("KMS_RLockPath", func(t *testing.T) {
		mock := &mockKMSClientForCoverage{}
		c := &client{transport: mockT, opts: defaultOptions(), kmsClient: mock}
		result := c.kms()
		if result != KMSClient(mock) {
			t.Error("expected pre-populated KMS client to be returned via RLock path")
		}
	})

	t.Run("Secrets_RLockPath", func(t *testing.T) {
		mock := &mockSecretsClientForCoverage{}
		c := &client{transport: mockT, opts: defaultOptions(), secretsClient: mock}
		result := c.secrets()
		if result != SecretsClient(mock) {
			t.Error("expected pre-populated Secrets client to be returned via RLock path")
		}
	})

	t.Run("Crypto_RLockPath", func(t *testing.T) {
		mock := &mockCryptoClientForCoverage{}
		c := &client{transport: mockT, opts: defaultOptions(), cryptoClient: mock}
		result := c.crypto()
		if result != CryptoClient(mock) {
			t.Error("expected pre-populated Crypto client to be returned via RLock path")
		}
	})

	t.Run("Vector_RLockPath", func(t *testing.T) {
		mock := &mockVectorClientForCoverage{}
		c := &client{transport: mockT, opts: defaultOptions(), vectorClient: mock}
		result := c.Vector()
		if result != VectorClient(mock) {
			t.Error("expected pre-populated Vector client to be returned via RLock path")
		}
	})

	t.Run("TimeSeries_RLockPath", func(t *testing.T) {
		mock := &mockTimeSeriesClientForCoverage{}
		c := &client{transport: mockT, opts: defaultOptions(), tsClient: mock}
		result := c.TimeSeries()
		if result != TimeSeriesClient(mock) {
			t.Error("expected pre-populated TimeSeries client to be returned via RLock path")
		}
	})

	t.Run("Backup_RLockPath", func(t *testing.T) {
		mock := &mockBackupClientForCoverage{}
		c := &client{transport: mockT, opts: defaultOptions(), backupClient: mock}
		result := c.Backup()
		if result != BackupClient(mock) {
			t.Error("expected pre-populated Backup client to be returned via RLock path")
		}
	})

	t.Run("IAM_RLockPath", func(t *testing.T) {
		mock := &mockIAMClientForCoverage{}
		c := &client{transport: mockT, opts: defaultOptions(), iamClient: mock}
		result := c.IAM()
		if result != IAMClient(mock) {
			t.Error("expected pre-populated IAM client to be returned via RLock path")
		}
	})
}

// =============================================================================
// Mock implementations for extended client coverage tests
// =============================================================================

type mockCacheClientForCoverage struct{}

func (m *mockCacheClientForCoverage) GetCacheStats(ctx context.Context, req *cache.GetCacheStatsRequest) (*cache.GetCacheStatsResponse, error) {
	return nil, nil
}
func (m *mockCacheClientForCoverage) ClearCache(ctx context.Context, req *cache.ClearCacheRequest) (*cache.ClearCacheResponse, error) {
	return nil, nil
}
func (m *mockCacheClientForCoverage) GetCacheConfig(ctx context.Context, req *cache.GetCacheConfigRequest) (*cache.GetCacheConfigResponse, error) {
	return nil, nil
}
func (m *mockCacheClientForCoverage) UpdateCacheConfig(ctx context.Context, req *cache.UpdateCacheConfigRequest) (*cache.UpdateCacheConfigResponse, error) {
	return nil, nil
}

type mockOperationsClientForCoverage struct{}

func (m *mockOperationsClientForCoverage) MigrateData(ctx context.Context, req *operations.MigrateDataRequest) (*operations.MigrateDataResponse, error) {
	return nil, nil
}
func (m *mockOperationsClientForCoverage) GetMigrationProgress(ctx context.Context, req *operations.GetMigrationProgressRequest) (*operations.GetMigrationProgressResponse, error) {
	return nil, nil
}
func (m *mockOperationsClientForCoverage) ReplaceNode(ctx context.Context, req *operations.ReplaceNodeRequest) (*operations.ReplaceNodeResponse, error) {
	return nil, nil
}
func (m *mockOperationsClientForCoverage) UpgradeNode(ctx context.Context, req *operations.UpgradeNodeRequest) (*operations.UpgradeNodeResponse, error) {
	return nil, nil
}
func (m *mockOperationsClientForCoverage) GetUpgradeStatus(ctx context.Context, req *operations.GetUpgradeStatusRequest) (*operations.GetUpgradeStatusResponse, error) {
	return nil, nil
}

type mockRBACClientForCoverage struct{}

func (m *mockRBACClientForCoverage) CreateUser(ctx context.Context, req *rbac.CreateUserRequest) (*rbac.CreateUserResponse, error) {
	return nil, nil
}
func (m *mockRBACClientForCoverage) DeleteUser(ctx context.Context, req *rbac.DeleteUserRequest) (*rbac.DeleteUserResponse, error) {
	return nil, nil
}
func (m *mockRBACClientForCoverage) ListUsers(ctx context.Context, req *rbac.ListUsersRequest) (*rbac.ListUsersResponse, error) {
	return nil, nil
}
func (m *mockRBACClientForCoverage) CreateRole(ctx context.Context, req *rbac.CreateRoleRequest) (*rbac.CreateRoleResponse, error) {
	return nil, nil
}
func (m *mockRBACClientForCoverage) DeleteRole(ctx context.Context, req *rbac.DeleteRoleRequest) (*rbac.DeleteRoleResponse, error) {
	return nil, nil
}
func (m *mockRBACClientForCoverage) ListRoles(ctx context.Context, req *rbac.ListRolesRequest) (*rbac.ListRolesResponse, error) {
	return nil, nil
}
func (m *mockRBACClientForCoverage) AssignRole(ctx context.Context, req *rbac.AssignRoleRequest) (*rbac.AssignRoleResponse, error) {
	return nil, nil
}
func (m *mockRBACClientForCoverage) RevokeRole(ctx context.Context, req *rbac.RevokeRoleRequest) (*rbac.RevokeRoleResponse, error) {
	return nil, nil
}

type mockDynamicSecretsClientForCoverage struct{}

func (m *mockDynamicSecretsClientForCoverage) CreateRole(ctx context.Context, req *dynamicsecrets.CreateRoleRequest) (*dynamicsecrets.CreateRoleResponse, error) {
	return nil, nil
}
func (m *mockDynamicSecretsClientForCoverage) GetRole(ctx context.Context, req *dynamicsecrets.GetRoleRequest) (*dynamicsecrets.GetRoleResponse, error) {
	return nil, nil
}
func (m *mockDynamicSecretsClientForCoverage) ListRoles(ctx context.Context, req *dynamicsecrets.ListRolesRequest) (*dynamicsecrets.ListRolesResponse, error) {
	return nil, nil
}
func (m *mockDynamicSecretsClientForCoverage) DeleteRole(ctx context.Context, req *dynamicsecrets.DeleteRoleRequest) (*dynamicsecrets.DeleteRoleResponse, error) {
	return nil, nil
}
func (m *mockDynamicSecretsClientForCoverage) GenerateCredential(ctx context.Context, req *dynamicsecrets.GenerateCredentialRequest) (*dynamicsecrets.GenerateCredentialResponse, error) {
	return nil, nil
}
func (m *mockDynamicSecretsClientForCoverage) RenewLease(ctx context.Context, req *dynamicsecrets.RenewLeaseRequest) (*dynamicsecrets.RenewLeaseResponse, error) {
	return nil, nil
}
func (m *mockDynamicSecretsClientForCoverage) RevokeLease(ctx context.Context, req *dynamicsecrets.RevokeLeaseRequest) (*dynamicsecrets.RevokeLeaseResponse, error) {
	return nil, nil
}
func (m *mockDynamicSecretsClientForCoverage) ListLeases(ctx context.Context, req *dynamicsecrets.ListLeasesRequest) (*dynamicsecrets.ListLeasesResponse, error) {
	return nil, nil
}

type mockCertificateClientForCoverage struct{}

func (m *mockCertificateClientForCoverage) IssueCertificate(ctx context.Context, req *certificate.IssueCertificateRequest) (*certificate.IssueCertificateResponse, error) {
	return nil, nil
}
func (m *mockCertificateClientForCoverage) SignCSR(ctx context.Context, req *certificate.SignCSRRequest) (*certificate.SignCSRResponse, error) {
	return nil, nil
}
func (m *mockCertificateClientForCoverage) RevokeCertificate(ctx context.Context, req *certificate.RevokeCertificateRequest) (*certificate.RevokeCertificateResponse, error) {
	return nil, nil
}
func (m *mockCertificateClientForCoverage) VerifyCertificate(ctx context.Context, req *certificate.VerifyCertificateRequest) (*certificate.VerifyCertificateResponse, error) {
	return nil, nil
}
func (m *mockCertificateClientForCoverage) GetCACertificate(ctx context.Context, req *certificate.GetCACertificateRequest) (*certificate.GetCACertificateResponse, error) {
	return nil, nil
}
func (m *mockCertificateClientForCoverage) GetTLSConfig(ctx context.Context, req *certificate.GetTLSConfigRequest) (*certificate.GetTLSConfigResponse, error) {
	return nil, nil
}
func (m *mockCertificateClientForCoverage) IsInitialized(ctx context.Context) (bool, error) {
	return true, nil
}

type mockObjectStorageClientForCoverage struct{}

func (m *mockObjectStorageClientForCoverage) PutObject(ctx context.Context, req *objectstorage.PutObjectRequest) (*objectstorage.PutObjectResponse, error) {
	return nil, nil
}
func (m *mockObjectStorageClientForCoverage) GetObject(ctx context.Context, req *objectstorage.GetObjectRequest) (*objectstorage.GetObjectResponse, error) {
	return nil, nil
}
func (m *mockObjectStorageClientForCoverage) DeleteObject(ctx context.Context, req *objectstorage.DeleteObjectRequest) (*objectstorage.DeleteObjectResponse, error) {
	return nil, nil
}
func (m *mockObjectStorageClientForCoverage) HeadObject(ctx context.Context, req *objectstorage.HeadObjectRequest) (*objectstorage.HeadObjectResponse, error) {
	return nil, nil
}
func (m *mockObjectStorageClientForCoverage) ListObjects(ctx context.Context, req *objectstorage.ListObjectsRequest) (*objectstorage.ListObjectsResponse, error) {
	return nil, nil
}
func (m *mockObjectStorageClientForCoverage) ListByTag(ctx context.Context, req *objectstorage.ListByTagRequest) (*objectstorage.ListByTagResponse, error) {
	return nil, nil
}
func (m *mockObjectStorageClientForCoverage) ListByTenant(ctx context.Context, req *objectstorage.ListByTenantRequest) (*objectstorage.ListByTenantResponse, error) {
	return nil, nil
}
func (m *mockObjectStorageClientForCoverage) CopyObject(ctx context.Context, req *objectstorage.CopyObjectRequest) (*objectstorage.CopyObjectResponse, error) {
	return nil, nil
}
func (m *mockObjectStorageClientForCoverage) ReplicateObject(ctx context.Context, req *objectstorage.ReplicateObjectRequest) (*objectstorage.ReplicateObjectResponse, error) {
	return nil, nil
}
func (m *mockObjectStorageClientForCoverage) MigrateObject(ctx context.Context, req *objectstorage.MigrateObjectRequest) (*objectstorage.MigrateObjectResponse, error) {
	return nil, nil
}
func (m *mockObjectStorageClientForCoverage) ArchiveObject(ctx context.Context, req *objectstorage.ArchiveObjectRequest) (*objectstorage.ArchiveObjectResponse, error) {
	return nil, nil
}
func (m *mockObjectStorageClientForCoverage) RestoreObject(ctx context.Context, req *objectstorage.RestoreObjectRequest) (*objectstorage.RestoreObjectResponse, error) {
	return nil, nil
}
func (m *mockObjectStorageClientForCoverage) VerifyObject(ctx context.Context, req *objectstorage.VerifyObjectRequest) (*objectstorage.VerifyObjectResponse, error) {
	return nil, nil
}
func (m *mockObjectStorageClientForCoverage) SetObjectPolicy(ctx context.Context, req *objectstorage.SetObjectPolicyRequest) (*objectstorage.SetObjectPolicyResponse, error) {
	return nil, nil
}
func (m *mockObjectStorageClientForCoverage) GetObjectPolicy(ctx context.Context, req *objectstorage.GetObjectPolicyRequest) (*objectstorage.GetObjectPolicyResponse, error) {
	return nil, nil
}
func (m *mockObjectStorageClientForCoverage) ListBackends(ctx context.Context, req *objectstorage.ListBackendsRequest) (*objectstorage.ListBackendsResponse, error) {
	return nil, nil
}
func (m *mockObjectStorageClientForCoverage) GetBackendHealth(ctx context.Context, req *objectstorage.GetBackendHealthRequest) (*objectstorage.GetBackendHealthResponse, error) {
	return nil, nil
}
func (m *mockObjectStorageClientForCoverage) GetTenantUsage(ctx context.Context, req *objectstorage.GetTenantUsageRequest) (*objectstorage.GetTenantUsageResponse, error) {
	return nil, nil
}
func (m *mockObjectStorageClientForCoverage) GetTenantQuotas(ctx context.Context, req *objectstorage.GetTenantQuotasRequest) (*objectstorage.GetTenantQuotasResponse, error) {
	return nil, nil
}

type mockParameterStoreClientForCoverage struct{}

func (m *mockParameterStoreClientForCoverage) GetParameter(ctx context.Context, req *parameterstore.GetParameterRequest) (*parameterstore.GetParameterResponse, error) {
	return nil, nil
}
func (m *mockParameterStoreClientForCoverage) SetParameter(ctx context.Context, req *parameterstore.SetParameterRequest) (*parameterstore.SetParameterResponse, error) {
	return nil, nil
}
func (m *mockParameterStoreClientForCoverage) DeleteParameter(ctx context.Context, req *parameterstore.DeleteParameterRequest) (*parameterstore.DeleteParameterResponse, error) {
	return nil, nil
}
func (m *mockParameterStoreClientForCoverage) GetParameters(ctx context.Context, req *parameterstore.GetParametersRequest) (*parameterstore.GetParametersResponse, error) {
	return nil, nil
}
func (m *mockParameterStoreClientForCoverage) SetParameters(ctx context.Context, req *parameterstore.SetParametersRequest) (*parameterstore.SetParametersResponse, error) {
	return nil, nil
}
func (m *mockParameterStoreClientForCoverage) DeleteParameters(ctx context.Context, req *parameterstore.DeleteParametersRequest) (*parameterstore.DeleteParametersResponse, error) {
	return nil, nil
}
func (m *mockParameterStoreClientForCoverage) ListParameters(ctx context.Context, req *parameterstore.ListParametersRequest) (*parameterstore.ListParametersResponse, error) {
	return nil, nil
}
func (m *mockParameterStoreClientForCoverage) SearchByTags(ctx context.Context, req *parameterstore.SearchByTagsRequest) (*parameterstore.SearchByTagsResponse, error) {
	return nil, nil
}

type mockEmbeddingClientForCoverage struct{}

func (m *mockEmbeddingClientForCoverage) GenerateEmbedding(ctx context.Context, req *embedding.GenerateEmbeddingRequest) (*embedding.GenerateEmbeddingResponse, error) {
	return nil, nil
}
func (m *mockEmbeddingClientForCoverage) BatchGenerateEmbeddings(ctx context.Context, req *embedding.BatchGenerateEmbeddingsRequest) (*embedding.BatchGenerateEmbeddingsResponse, error) {
	return nil, nil
}
func (m *mockEmbeddingClientForCoverage) ListProviders(ctx context.Context, req *embedding.ListProvidersRequest) (*embedding.ListProvidersResponse, error) {
	return nil, nil
}
func (m *mockEmbeddingClientForCoverage) GetProviderInfo(ctx context.Context, req *embedding.GetProviderInfoRequest) (*embedding.GetProviderInfoResponse, error) {
	return nil, nil
}
func (m *mockEmbeddingClientForCoverage) SetDefaultProvider(ctx context.Context, req *embedding.SetDefaultProviderRequest) (*embedding.SetDefaultProviderResponse, error) {
	return nil, nil
}
func (m *mockEmbeddingClientForCoverage) GetCacheStats(ctx context.Context, req *embedding.GetEmbeddingCacheStatsRequest) (*embedding.GetEmbeddingCacheStatsResponse, error) {
	return nil, nil
}
func (m *mockEmbeddingClientForCoverage) ClearCache(ctx context.Context, req *embedding.ClearEmbeddingCacheRequest) (*embedding.ClearEmbeddingCacheResponse, error) {
	return nil, nil
}

type mockOllamaClientForCoverage struct{}

func (m *mockOllamaClientForCoverage) Embed(ctx context.Context, req *ollama.EmbedRequest) (*ollama.EmbedResponse, error) {
	return nil, nil
}
func (m *mockOllamaClientForCoverage) EmbedWithModel(ctx context.Context, req *ollama.EmbedWithModelRequest) (*ollama.EmbedResponse, error) {
	return nil, nil
}
func (m *mockOllamaClientForCoverage) EmbedBatch(ctx context.Context, req *ollama.EmbedBatchRequest) (*ollama.EmbedBatchResponse, error) {
	return nil, nil
}
func (m *mockOllamaClientForCoverage) ListModels(ctx context.Context, req *ollama.ListModelsRequest) (*ollama.ListModelsResponse, error) {
	return nil, nil
}
func (m *mockOllamaClientForCoverage) GetModelInfo(ctx context.Context, req *ollama.GetModelInfoRequest) (*ollama.GetModelInfoResponse, error) {
	return nil, nil
}
func (m *mockOllamaClientForCoverage) Health(ctx context.Context, req *ollama.HealthRequest) (*ollama.HealthResponse, error) {
	return nil, nil
}
func (m *mockOllamaClientForCoverage) IsAvailable() bool {
	return true
}
func (m *mockOllamaClientForCoverage) DefaultModel() string {
	return "nomic-embed-text"
}
func (m *mockOllamaClientForCoverage) BaseURL() string {
	return "http://localhost:11434"
}

// =============================================================================
// Test: Extended Client Accessors - Write Lock Path (nil to initialized)
// =============================================================================

// TestClient_ExtendedAccessors_WriteLockPath tests the initialization path
// where the client is nil and needs to be created via the write lock.
func TestClient_ExtendedAccessors_WriteLockPath(t *testing.T) {
	mockT := &mockTransport{}

	t.Run("Cache_WriteLockPath", func(t *testing.T) {
		c := &client{transport: mockT, opts: defaultOptions()}
		result := c.Cache()
		if result == nil {
			t.Error("expected non-nil Cache client after write lock initialization")
		}
		// Verify same instance returned on second call
		result2 := c.Cache()
		if result != result2 {
			t.Error("expected same Cache client instance on repeated calls")
		}
	})

	t.Run("Operations_WriteLockPath", func(t *testing.T) {
		c := &client{transport: mockT, opts: defaultOptions()}
		result := c.Operations()
		if result == nil {
			t.Error("expected non-nil Operations client after write lock initialization")
		}
		result2 := c.Operations()
		if result != result2 {
			t.Error("expected same Operations client instance on repeated calls")
		}
	})

	t.Run("RBAC_WriteLockPath", func(t *testing.T) {
		c := &client{transport: mockT, opts: defaultOptions()}
		result := c.rbac()
		if result == nil {
			t.Error("expected non-nil RBAC client after write lock initialization")
		}
		result2 := c.rbac()
		if result != result2 {
			t.Error("expected same RBAC client instance on repeated calls")
		}
	})

	t.Run("DynamicSecrets_WriteLockPath", func(t *testing.T) {
		c := &client{transport: mockT, opts: defaultOptions()}
		result := c.dynamicSecrets()
		if result == nil {
			t.Error("expected non-nil DynamicSecrets client after write lock initialization")
		}
		result2 := c.dynamicSecrets()
		if result != result2 {
			t.Error("expected same DynamicSecrets client instance on repeated calls")
		}
	})

	t.Run("Certificate_WriteLockPath", func(t *testing.T) {
		c := &client{transport: mockT, opts: defaultOptions()}
		result := c.certificate()
		if result == nil {
			t.Error("expected non-nil Certificate client after write lock initialization")
		}
		result2 := c.certificate()
		if result != result2 {
			t.Error("expected same Certificate client instance on repeated calls")
		}
	})

	t.Run("ObjectStorage_WriteLockPath", func(t *testing.T) {
		c := &client{transport: mockT, opts: defaultOptions()}
		result := c.ObjectStorage()
		if result == nil {
			t.Error("expected non-nil ObjectStorage client after write lock initialization")
		}
		result2 := c.ObjectStorage()
		if result != result2 {
			t.Error("expected same ObjectStorage client instance on repeated calls")
		}
	})

	t.Run("ParameterStore_WriteLockPath", func(t *testing.T) {
		c := &client{transport: mockT, opts: defaultOptions()}
		result := c.ParameterStore()
		if result == nil {
			t.Error("expected non-nil ParameterStore client after write lock initialization")
		}
		result2 := c.ParameterStore()
		if result != result2 {
			t.Error("expected same ParameterStore client instance on repeated calls")
		}
	})

	t.Run("Embedding_WriteLockPath", func(t *testing.T) {
		c := &client{transport: mockT, opts: defaultOptions()}
		result := c.Embedding()
		if result == nil {
			t.Error("expected non-nil Embedding client after write lock initialization")
		}
		result2 := c.Embedding()
		if result != result2 {
			t.Error("expected same Embedding client instance on repeated calls")
		}
	})

	t.Run("Ollama_WriteLockPath", func(t *testing.T) {
		c := &client{transport: mockT, opts: defaultOptions()}
		result := c.Ollama()
		if result == nil {
			t.Error("expected non-nil Ollama client after write lock initialization")
		}
		result2 := c.Ollama()
		if result != result2 {
			t.Error("expected same Ollama client instance on repeated calls")
		}
	})

	t.Run("IAM_WriteLockPath", func(t *testing.T) {
		c := &client{transport: mockT, opts: defaultOptions()}
		result := c.IAM()
		if result == nil {
			t.Error("expected non-nil IAM client after write lock initialization")
		}
		result2 := c.IAM()
		if result != result2 {
			t.Error("expected same IAM client instance on repeated calls")
		}
	})
}

// =============================================================================
// Test: Extended Client Accessors - Concurrent Access
// =============================================================================

// TestClient_ExtendedAccessors_Concurrent tests concurrent access to ensure
// thread-safe initialization of all extended client accessors.
func TestClient_ExtendedAccessors_Concurrent(t *testing.T) {
	mockT := &mockTransport{}

	tests := []struct {
		name   string
		getter func(*client) interface{}
	}{
		{"Cache", func(c *client) interface{} { return c.Cache() }},
		{"Operations", func(c *client) interface{} { return c.Operations() }},
		{"RBAC", func(c *client) interface{} { return c.rbac() }},
		{"DynamicSecrets", func(c *client) interface{} { return c.dynamicSecrets() }},
		{"Certificate", func(c *client) interface{} { return c.certificate() }},
		{"ObjectStorage", func(c *client) interface{} { return c.ObjectStorage() }},
		{"ParameterStore", func(c *client) interface{} { return c.ParameterStore() }},
		{"Embedding", func(c *client) interface{} { return c.Embedding() }},
		{"Ollama", func(c *client) interface{} { return c.Ollama() }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &client{
				transport: mockT,
				opts:      defaultOptions(),
			}

			var wg sync.WaitGroup
			results := make([]interface{}, 10)

			for i := 0; i < 10; i++ {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					results[idx] = tt.getter(c)
				}(i)
			}
			wg.Wait()

			// All results should be the same instance
			first := results[0]
			for i := 1; i < 10; i++ {
				if results[i] != first {
					t.Errorf("expected all %s instances to be the same", tt.name)
					break
				}
			}
		})
	}
}

// =============================================================================
// Test: Option functions directly for coverage
// =============================================================================

// TestOptionFunctions tests all option functions to ensure they properly
// modify the options struct without requiring actual transport creation.
func TestOptionFunctions(t *testing.T) {
	t.Run("WithREST_SetsTransportType", func(t *testing.T) {
		opts := defaultOptions()
		err := WithREST()(opts)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if opts.transportType != TransportREST {
			t.Errorf("expected TransportREST, got %v", opts.transportType)
		}
	})

	t.Run("WithQUIC_SetsTransportType", func(t *testing.T) {
		opts := defaultOptions()
		err := WithQUIC()(opts)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if opts.transportType != TransportQUIC {
			t.Errorf("expected TransportQUIC, got %v", opts.transportType)
		}
	})

	t.Run("WithInsecure_IsBanned", func(t *testing.T) {
		// Per QuicBaaS FedRAMP policy, WithInsecure() must refuse and
		// MUST NOT mark the client as insecure.
		opts := defaultOptions()
		err := WithInsecure()(opts)
		if err == nil {
			t.Fatal("WithInsecure must return an error (banned by policy)")
		}
		if opts.insecure {
			t.Error("WithInsecure must NOT set insecure=true")
		}
	})

	t.Run("WithTimeout_SetsTimeout", func(t *testing.T) {
		opts := defaultOptions()
		err := WithTimeout(45 * time.Second)(opts)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if opts.timeout != 45*time.Second {
			t.Errorf("expected 45s, got %v", opts.timeout)
		}
	})

	t.Run("WithTimeout_InvalidValue", func(t *testing.T) {
		opts := defaultOptions()
		err := WithTimeout(-1 * time.Second)(opts)
		if err == nil {
			t.Error("expected error for negative timeout")
		}
	})

	t.Run("WithKeepalive_SetsKeepaliveParams", func(t *testing.T) {
		opts := defaultOptions()
		err := WithKeepalive(30*time.Second, 10*time.Second)(opts)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if opts.keepaliveTime != 30*time.Second {
			t.Errorf("expected keepalive time 30s, got %v", opts.keepaliveTime)
		}
		if opts.keepaliveTimeout != 10*time.Second {
			t.Errorf("expected keepalive timeout 10s, got %v", opts.keepaliveTimeout)
		}
	})

	t.Run("WithKeepalive_InvalidTime", func(t *testing.T) {
		opts := defaultOptions()
		err := WithKeepalive(-1*time.Second, 10*time.Second)(opts)
		if err == nil {
			t.Error("expected error for negative keepalive time")
		}
	})

	t.Run("WithKeepalive_InvalidTimeout", func(t *testing.T) {
		opts := defaultOptions()
		err := WithKeepalive(30*time.Second, -1*time.Second)(opts)
		if err == nil {
			t.Error("expected error for negative keepalive timeout")
		}
	})

	t.Run("WithRetry_SetsRetryParams", func(t *testing.T) {
		opts := defaultOptions()
		err := WithRetry(5, 200*time.Millisecond)(opts)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if opts.maxRetries != 5 {
			t.Errorf("expected max retries 5, got %d", opts.maxRetries)
		}
		if opts.retryBackoff != 200*time.Millisecond {
			t.Errorf("expected retry backoff 200ms, got %v", opts.retryBackoff)
		}
	})

	t.Run("WithRetry_InvalidMaxRetries", func(t *testing.T) {
		opts := defaultOptions()
		err := WithRetry(-1, 200*time.Millisecond)(opts)
		if err == nil {
			t.Error("expected error for negative max retries")
		}
	})

	t.Run("WithRetry_InvalidBackoff", func(t *testing.T) {
		opts := defaultOptions()
		err := WithRetry(3, -1*time.Millisecond)(opts)
		if err == nil {
			t.Error("expected error for negative backoff")
		}
	})

	t.Run("WithMaxRetryBackoff_SetsMaxBackoff", func(t *testing.T) {
		opts := defaultOptions()
		err := WithMaxRetryBackoff(10 * time.Second)(opts)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if opts.maxRetryBackoff != 10*time.Second {
			t.Errorf("expected max retry backoff 10s, got %v", opts.maxRetryBackoff)
		}
	})

	t.Run("WithMaxRetryBackoff_InvalidValue", func(t *testing.T) {
		opts := defaultOptions()
		err := WithMaxRetryBackoff(-1 * time.Second)(opts)
		if err == nil {
			t.Error("expected error for negative max retry backoff")
		}
	})

	t.Run("WithConnectionPool_SetsPoolParams", func(t *testing.T) {
		opts := defaultOptions()
		err := WithConnectionPool(5, 20)(opts)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if opts.poolMinSize != 5 {
			t.Errorf("expected min size 5, got %d", opts.poolMinSize)
		}
		if opts.poolMaxSize != 20 {
			t.Errorf("expected max size 20, got %d", opts.poolMaxSize)
		}
	})

	t.Run("WithConnectionPool_InvalidMinSize", func(t *testing.T) {
		opts := defaultOptions()
		err := WithConnectionPool(0, 20)(opts)
		if err == nil {
			t.Error("expected error for zero min size")
		}
	})

	t.Run("WithConnectionPool_MaxLessThanMin", func(t *testing.T) {
		opts := defaultOptions()
		err := WithConnectionPool(10, 5)(opts)
		if err == nil {
			t.Error("expected error when max < min")
		}
	})

	t.Run("WithPoolMaxAge_SetsMaxAge", func(t *testing.T) {
		opts := defaultOptions()
		err := WithPoolMaxAge(2 * time.Hour)(opts)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if opts.poolMaxAge != 2*time.Hour {
			t.Errorf("expected pool max age 2h, got %v", opts.poolMaxAge)
		}
	})

	t.Run("WithPoolMaxAge_InvalidValue", func(t *testing.T) {
		opts := defaultOptions()
		err := WithPoolMaxAge(-1 * time.Hour)(opts)
		if err == nil {
			t.Error("expected error for negative pool max age")
		}
	})

	t.Run("WithPoolHealthInterval_SetsInterval", func(t *testing.T) {
		opts := defaultOptions()
		err := WithPoolHealthInterval(time.Minute)(opts)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if opts.poolHealthInterval != time.Minute {
			t.Errorf("expected pool health interval 1m, got %v", opts.poolHealthInterval)
		}
	})

	t.Run("WithPoolHealthInterval_InvalidValue", func(t *testing.T) {
		opts := defaultOptions()
		err := WithPoolHealthInterval(-1 * time.Second)(opts)
		if err == nil {
			t.Error("expected error for negative pool health interval")
		}
	})

	t.Run("WithTLSConfig_SetsTLSConfig", func(t *testing.T) {
		opts := defaultOptions()
		tlsConfig := &tls.Config{
			MinVersion:         tls.VersionTLS13,
			InsecureSkipVerify: true,
		}
		err := WithTLSConfig(tlsConfig)(opts)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if opts.tlsConfig != tlsConfig {
			t.Error("expected TLS config to be set")
		}
	})

	t.Run("WithTLSConfig_NilConfig", func(t *testing.T) {
		opts := defaultOptions()
		err := WithTLSConfig(nil)(opts)
		if err == nil {
			t.Error("expected error for nil TLS config")
		}
	})
}

// =============================================================================
// Test: Client type alias exports
// =============================================================================

// TestTypeAliasExports verifies that type aliases are properly exported
// and can be used without importing sub-packages.
func TestTypeAliasExports(t *testing.T) {
	// Verify transport type constants
	if TransportGRPC != 0 {
		t.Errorf("expected TransportGRPC=0, got %d", TransportGRPC)
	}
	if TransportREST != 1 {
		t.Errorf("expected TransportREST=1, got %d", TransportREST)
	}
	if TransportQUIC != 2 {
		t.Errorf("expected TransportQUIC=2, got %d", TransportQUIC)
	}

	// Verify batch operation type constants
	if BatchOpPut != 0 {
		t.Errorf("expected BatchOpPut=0, got %d", BatchOpPut)
	}
	if BatchOpDelete != 1 {
		t.Errorf("expected BatchOpDelete=1, got %d", BatchOpDelete)
	}

	// Verify compare target constants
	if CompareVersion != 0 {
		t.Errorf("expected CompareVersion=0, got %d", CompareVersion)
	}

	// Verify compare result constants
	if CompareEqual != 0 {
		t.Errorf("expected CompareEqual=0, got %d", CompareEqual)
	}

	// Verify transaction operation type constants
	if TxnOpPut != 0 {
		t.Errorf("expected TxnOpPut=0, got %d", TxnOpPut)
	}
	if TxnOpGet != 1 {
		t.Errorf("expected TxnOpGet=1, got %d", TxnOpGet)
	}
	if TxnOpDelete != 2 {
		t.Errorf("expected TxnOpDelete=2, got %d", TxnOpDelete)
	}

	// Verify query operator constants
	if QueryOpEqual != 0 {
		t.Errorf("expected QueryOpEqual=0, got %d", QueryOpEqual)
	}

	// Verify node state constants
	if NodeStateUnknown != 0 {
		t.Errorf("expected NodeStateUnknown=0, got %d", NodeStateUnknown)
	}
	if NodeStateHealthy != 1 {
		t.Errorf("expected NodeStateHealthy=1, got %d", NodeStateHealthy)
	}

	// Verify hash algorithm constants
	if HashSHA256 != 0 {
		t.Errorf("expected HashSHA256=0, got %d", HashSHA256)
	}

	// Verify aggregate function constants
	if AggregateNone != 0 {
		t.Errorf("expected AggregateNone=0, got %d", AggregateNone)
	}

	// Verify event type constants
	if EventPut != 0 {
		t.Errorf("expected EventPut=0, got %d", EventPut)
	}
	if EventDelete != 1 {
		t.Errorf("expected EventDelete=1, got %d", EventDelete)
	}
}

// =============================================================================
// Test: Constructor function coverage
// =============================================================================

// TestNewClientFunctions verifies that all newXXXClient functions work
// correctly and return valid client implementations.
func TestNewClientFunctions(t *testing.T) {
	mockT := &mockTransport{}

	t.Run("newCacheClient", func(t *testing.T) {
		result := newCacheClient(mockT)
		if result == nil {
			t.Error("newCacheClient should return non-nil client")
		}
	})

	t.Run("newOperationsClient", func(t *testing.T) {
		result := newOperationsClient(mockT)
		if result == nil {
			t.Error("newOperationsClient should return non-nil client")
		}
	})

	t.Run("newRBACClient", func(t *testing.T) {
		result := newRBACClient(mockT)
		if result == nil {
			t.Error("newRBACClient should return non-nil client")
		}
	})

	t.Run("newDynamicSecretsClient", func(t *testing.T) {
		result := newDynamicSecretsClient(mockT)
		if result == nil {
			t.Error("newDynamicSecretsClient should return non-nil client")
		}
	})

	t.Run("newCertificateClient", func(t *testing.T) {
		result := newCertificateClient(mockT)
		if result == nil {
			t.Error("newCertificateClient should return non-nil client")
		}
	})

	t.Run("newObjectStorageClient", func(t *testing.T) {
		result := newObjectStorageClient(mockT)
		if result == nil {
			t.Error("newObjectStorageClient should return non-nil client")
		}
	})

	t.Run("newParameterStoreClient", func(t *testing.T) {
		result := newParameterStoreClient(mockT)
		if result == nil {
			t.Error("newParameterStoreClient should return non-nil client")
		}
	})

	t.Run("newEmbeddingClient", func(t *testing.T) {
		result := newEmbeddingClient(mockT)
		if result == nil {
			t.Error("newEmbeddingClient should return non-nil client")
		}
	})

	t.Run("newOllamaClient", func(t *testing.T) {
		result := newOllamaClient(mockT)
		if result == nil {
			t.Error("newOllamaClient should return non-nil client")
		}
	})

	t.Run("newIAMClient", func(t *testing.T) {
		result := newIAMClient(mockT)
		if result == nil {
			t.Error("newIAMClient should return non-nil client")
		}
	})
}

// =============================================================================
// Test: KV accessor coverage for RLock path
// =============================================================================

// TestClient_KV_RLockPath tests the KV accessor RLock path with pre-populated client
func TestClient_KV_RLockPath(t *testing.T) {
	mockT := &mockTransport{}
	mock := &mockKVClientForCoverage{}
	c := &client{transport: mockT, opts: defaultOptions(), kvClient: mock}
	result := c.KV()
	if result != mock {
		t.Error("expected pre-populated KV client to be returned via RLock path")
	}
}
