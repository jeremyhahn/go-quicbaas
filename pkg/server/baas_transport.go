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

package server

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// Compile-time interface assertions for the BaaS embedded transport.
var (
	_ transport.Transport            = (*baasTransport)(nil)
	_ transport.KVClient             = (*baasTransport)(nil)
	_ transport.AdminClient          = (*baasTransport)(nil)
	_ transport.MVCCClient           = (*baasTransport)(nil)
	_ transport.TransactionClient    = (*baasTransport)(nil)
	_ transport.BatchClient          = (*baasTransport)(nil)
	_ transport.WatchClient          = (*baasTransport)(nil)
	_ transport.LeaseClient          = (*baasTransport)(nil)
	_ transport.LockClient           = (*baasTransport)(nil)
	_ transport.TimeSeriesClient     = (*baasTransport)(nil)
	_ transport.VectorClient         = (*baasTransport)(nil)
	_ transport.UserClient           = (*baasTransport)(nil)
	_ transport.RoleClient           = (*baasTransport)(nil)
	_ transport.CertClient           = (*baasTransport)(nil)
	_ transport.BarrierClient        = (*baasTransport)(nil)
	_ transport.DEKClient            = (*baasTransport)(nil)
	_ transport.DatabaseClient       = (*baasTransport)(nil)
	_ transport.CeremonyClient       = (*baasTransport)(nil)
	_ transport.CredentialClient     = (*baasTransport)(nil)
	_ transport.SealStoreClient      = (*baasTransport)(nil)
	_ transport.CustodianGroupClient = (*baasTransport)(nil)
	_ transport.ShareClient          = (*baasTransport)(nil)
	_ transport.TenantClient         = (*baasTransport)(nil)
	_ transport.Client               = (*baasTransport)(nil)
)

// BaasTransportNotSupportedError indicates an operation that is not
// supported by the BaaS embedded transport.
type BaasTransportNotSupportedError struct {
	Operation string
}

// Error implements the error interface.
func (e *BaasTransportNotSupportedError) Error() string {
	return "baas embedded transport: " + e.Operation + " not supported"
}

// baasTransport implements the qrdb SDK transport.Client interface for
// BaaS embedded mode. KV operations delegate to the configured KVStore
// (memory, file, or PebbleDB). TimeSeries operations are implemented
// in baas_transport_timeseries.go and Vector operations are implemented
// in baas_transport_vector.go. Database operations are implemented in
// baas_transport_database.go and delegate to the DatabaseService when
// available. When vectorStorage is set, vector write operations delegate
// to the real PebbleVectorStorage so that data is visible to the
// vectorService read path. When vectorStorage is nil, vector operations
// fall back to the KV-backed implementation. When timeSeriesStorage is
// set, timeseries write and read operations delegate to the real PebbleDB
// timeseries storage so that data is visible to the timeSeriesService
// read path. When timeSeriesStorage is nil, timeseries operations fall
// back to the KV-backed implementation. Admin, MVCC, Transaction, Batch,
// Watch, Lease, Index, User, Role, Cert, Barrier, Ceremony, Credential,
// Collection, Backup, and Rebalance operations return
// BaasTransportNotSupportedError.
type baasTransport struct {
	store             interfaces.KVStore
	vectorStorage     interfaces.VectorStorage
	timeSeriesStorage interfaces.TimeSeriesStorage
	dbService         service.DatabaseService
	lockService       BaasLockService
	closed            atomic.Bool
}

// newBaasTransport creates a new BaaS embedded transport backed by
// the given KV store. An optional VectorStorage can be provided to
// route vector write operations to the real storage backend so that
// writes are visible to the vectorService read path. An optional
// TimeSeriesStorage can be provided to route timeseries operations
// to the real storage backend so that writes are visible to the
// timeSeriesService read path.
func newBaasTransport(store interfaces.KVStore, vectorStorage interfaces.VectorStorage, timeSeriesStorage interfaces.TimeSeriesStorage, dbService service.DatabaseService, lockService BaasLockService) *baasTransport {
	return &baasTransport{
		store:             store,
		vectorStorage:     vectorStorage,
		timeSeriesStorage: timeSeriesStorage,
		dbService:         dbService,
		lockService:       lockService,
	}
}

// ---------------------------------------------------------------------------
// transport.Transport
// ---------------------------------------------------------------------------

// Connect is a no-op for the BaaS embedded transport.
func (t *baasTransport) Connect(_ context.Context) error {
	return nil
}

// Close marks the transport as closed.
func (t *baasTransport) Close() error {
	t.closed.Store(true)
	return nil
}

// Healthy returns true when the transport has not been closed.
func (t *baasTransport) Healthy(_ context.Context) bool {
	return !t.closed.Load()
}

// Request dispatches a generic request. Not used by the BaaS transport;
// callers should use the typed KVClient methods instead.
func (t *baasTransport) Request(_ context.Context, method string, _, _ any) error {
	return &BaasTransportNotSupportedError{Operation: "Request(" + method + ")"}
}

// ---------------------------------------------------------------------------
// transport.KVClient
// ---------------------------------------------------------------------------

// Put stores a key-value pair in the in-memory store.
func (t *baasTransport) Put(ctx context.Context, key string, value []byte, _ ...transport.WriteOption) error {
	return t.store.Put(ctx, key, value)
}

// Get retrieves a value by key from the in-memory store.
// Returns KeyNotFoundError when the key does not exist.
func (t *baasTransport) Get(ctx context.Context, key string, _ ...transport.ReadOption) ([]byte, error) {
	data, err := t.store.Get(ctx, key)
	if err != nil {
		return nil, &transport.KeyNotFoundError{Key: key}
	}
	return data, nil
}

// Delete removes a key from the in-memory store.
func (t *baasTransport) Delete(ctx context.Context, key string, _ ...transport.WriteOption) error {
	return t.store.Delete(ctx, key)
}

// List returns sorted keys matching the given prefix.
func (t *baasTransport) List(ctx context.Context, prefix string, _ ...transport.ReadOption) ([]string, error) {
	return t.store.List(ctx, prefix)
}

// Scan returns all key-value pairs matching the given prefix.
func (t *baasTransport) Scan(ctx context.Context, prefix string, _ ...transport.ReadOption) (map[string][]byte, error) {
	pairs := make(map[string][]byte)
	err := t.store.Scan(ctx, prefix, func(key string, value []byte) error {
		pairs[key] = value
		return nil
	})
	if err != nil {
		return nil, err
	}
	return pairs, nil
}

// Exists checks whether a key exists in the in-memory store.
func (t *baasTransport) Exists(ctx context.Context, key string, _ ...transport.ReadOption) (bool, error) {
	return t.store.Exists(ctx, key)
}

// ---------------------------------------------------------------------------
// transport.AdminClient - stub implementations
// ---------------------------------------------------------------------------

func (t *baasTransport) Health(_ context.Context) (*transport.HealthResponse, error) {
	return &transport.HealthResponse{Status: "healthy"}, nil
}

func (t *baasTransport) Status(_ context.Context) (*transport.StatusResponse, error) {
	return &transport.StatusResponse{}, nil
}

func (t *baasTransport) Peers(_ context.Context) (*transport.PeersResponse, error) {
	return &transport.PeersResponse{}, nil
}

func (t *baasTransport) AddMember(_ context.Context, _, _ uint64, _ string) error {
	return &BaasTransportNotSupportedError{Operation: "AddMember"}
}

func (t *baasTransport) RemoveMember(_ context.Context, _, _ uint64) error {
	return &BaasTransportNotSupportedError{Operation: "RemoveMember"}
}

func (t *baasTransport) RequestSnapshot(_ context.Context, _ uint64) error {
	return &BaasTransportNotSupportedError{Operation: "RequestSnapshot"}
}

func (t *baasTransport) Drain(_ context.Context) error {
	return &BaasTransportNotSupportedError{Operation: "Drain"}
}

func (t *baasTransport) Capabilities(_ context.Context) (*transport.CapabilitiesResponse, error) {
	return &transport.CapabilitiesResponse{}, nil
}

func (t *baasTransport) StartShard(_ context.Context, _ *transport.StartShardRequest) error {
	return &BaasTransportNotSupportedError{Operation: "StartShard"}
}

func (t *baasTransport) StopShard(_ context.Context, _ uint64) error {
	return &BaasTransportNotSupportedError{Operation: "StopShard"}
}

func (t *baasTransport) GetShardState(_ context.Context, _ uint64) (*transport.ShardStateResponse, error) {
	return &transport.ShardStateResponse{}, nil
}

func (t *baasTransport) GetTopology(_ context.Context) (*transport.TopologyResponse, error) {
	return &transport.TopologyResponse{}, nil
}

// ---------------------------------------------------------------------------
// transport.MVCCClient - stub implementations
// ---------------------------------------------------------------------------

func (t *baasTransport) PutWithLease(_ context.Context, _ string, _ []byte, _ int64, _ ...transport.WriteOption) (*transport.MVCCKeyValue, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "PutWithLease"}
}

func (t *baasTransport) GetRevision(_ context.Context, _ string, _ ...transport.ReadOption) (*transport.MVCCKeyValue, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "GetRevision"}
}

func (t *baasTransport) GetAtRevision(_ context.Context, _ string, _ int64, _ ...transport.ReadOption) (*transport.MVCCKeyValue, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "GetAtRevision"}
}

func (t *baasTransport) Range(_ context.Context, _, _ []byte, _ transport.RangeOptions, _ ...transport.ReadOption) (*transport.RangeResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "Range"}
}

func (t *baasTransport) Compact(_ context.Context, _ int64) error {
	return &BaasTransportNotSupportedError{Operation: "Compact"}
}

func (t *baasTransport) CurrentRevision(_ context.Context) (int64, error) {
	return 0, &BaasTransportNotSupportedError{Operation: "CurrentRevision"}
}

// ---------------------------------------------------------------------------
// transport.TransactionClient - stub implementation
// ---------------------------------------------------------------------------

func (t *baasTransport) ExecuteTransaction(_ context.Context, _ *transport.TransactionRequest, _ ...transport.WriteOption) (*transport.TransactionResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "ExecuteTransaction"}
}

// ---------------------------------------------------------------------------
// transport.BatchClient - in-memory implementation
// ---------------------------------------------------------------------------

// ExecuteBatch applies operations atomically against the in-memory store.
func (t *baasTransport) ExecuteBatch(ctx context.Context, ops []transport.Op, _ ...transport.WriteOption) (*transport.BatchResponse, error) {
	responses := make([]transport.OpResponse, 0, len(ops))
	for _, op := range ops {
		switch op.Type {
		case transport.OpPut:
			err := t.store.Put(ctx, string(op.Key), op.Value)
			responses = append(responses, transport.OpResponse{Succeeded: err == nil})
		case transport.OpGet:
			data, err := t.store.Get(ctx, string(op.Key))
			resp := transport.OpResponse{Succeeded: err == nil}
			if err == nil {
				resp.Kvs = []*transport.MVCCKeyValue{{Key: op.Key, Value: data}}
			}
			responses = append(responses, resp)
		case transport.OpDelete:
			err := t.store.Delete(ctx, string(op.Key))
			responses = append(responses, transport.OpResponse{Succeeded: err == nil})
		default:
			responses = append(responses, transport.OpResponse{Succeeded: false})
		}
	}
	return &transport.BatchResponse{Responses: responses}, nil
}

// ImportFile is not supported by the BaaS embedded transport.
func (t *baasTransport) ImportFile(_ context.Context, _ string, _ io.Reader, _ transport.ImportOptions) (*transport.ImportResult, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "ImportFile"}
}

// ---------------------------------------------------------------------------
// transport.WatchClient - stub implementations
// ---------------------------------------------------------------------------

func (t *baasTransport) Watch(_ context.Context, _ string, _ transport.WatchOptions) (<-chan transport.WatchResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "Watch"}
}

func (t *baasTransport) WatchPrefix(_ context.Context, _ string, _ transport.WatchOptions) (<-chan transport.WatchResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "WatchPrefix"}
}

// ---------------------------------------------------------------------------
// transport.LeaseClient - stub implementations
// ---------------------------------------------------------------------------

func (t *baasTransport) LeaseGrant(_ context.Context, _ int64) (*transport.LeaseInfo, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "LeaseGrant"}
}

func (t *baasTransport) LeaseRevoke(_ context.Context, _ int64) error {
	return &BaasTransportNotSupportedError{Operation: "LeaseRevoke"}
}

func (t *baasTransport) LeaseKeepAlive(_ context.Context, _ int64) (*transport.LeaseInfo, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "LeaseKeepAlive"}
}

func (t *baasTransport) LeaseTimeToLive(_ context.Context, _ int64) (*transport.LeaseInfo, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "LeaseTimeToLive"}
}

func (t *baasTransport) LeaseList(_ context.Context) ([]*transport.LeaseInfo, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "LeaseList"}
}

// ---------------------------------------------------------------------------
// transport.UserClient - stub implementations
// ---------------------------------------------------------------------------

func (t *baasTransport) CreateUser(_ context.Context, _ *transport.CreateUserRequest) (*transport.User, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "CreateUser"}
}

func (t *baasTransport) GetUser(_ context.Context, _ string) (*transport.User, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "GetUser"}
}

func (t *baasTransport) UpdateUser(_ context.Context, _ *transport.UpdateUserRequest) (*transport.User, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "UpdateUser"}
}

func (t *baasTransport) DeleteUser(_ context.Context, _ string) error {
	return &BaasTransportNotSupportedError{Operation: "DeleteUser"}
}

func (t *baasTransport) ListUsers(_ context.Context, _ *transport.ListUsersRequest) (*transport.ListUsersResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "ListUsers"}
}

func (t *baasTransport) EnableUser(_ context.Context, _ string) error {
	return &BaasTransportNotSupportedError{Operation: "EnableUser"}
}

func (t *baasTransport) DisableUser(_ context.Context, _ string) error {
	return &BaasTransportNotSupportedError{Operation: "DisableUser"}
}

// ---------------------------------------------------------------------------
// transport.RoleClient - stub implementations
// ---------------------------------------------------------------------------

func (t *baasTransport) CreateRole(_ context.Context, _ *transport.CreateRoleRequest) (*transport.Role, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "CreateRole"}
}

func (t *baasTransport) GetRole(_ context.Context, _ string) (*transport.Role, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "GetRole"}
}

func (t *baasTransport) UpdateRole(_ context.Context, _ *transport.UpdateRoleRequest) (*transport.Role, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "UpdateRole"}
}

func (t *baasTransport) DeleteRole(_ context.Context, _ string) error {
	return &BaasTransportNotSupportedError{Operation: "DeleteRole"}
}

func (t *baasTransport) ListRoles(_ context.Context, _ *transport.ListRolesRequest) (*transport.ListRolesResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "ListRoles"}
}

func (t *baasTransport) AssignRole(_ context.Context, _, _ string) error {
	return &BaasTransportNotSupportedError{Operation: "AssignRole"}
}

func (t *baasTransport) UnassignRole(_ context.Context, _, _ string) error {
	return &BaasTransportNotSupportedError{Operation: "UnassignRole"}
}

func (t *baasTransport) ListPermissions(_ context.Context) (*transport.ListPermissionsResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "ListPermissions"}
}

// ---------------------------------------------------------------------------
// transport.CertClient - stub implementations
// ---------------------------------------------------------------------------

func (t *baasTransport) GenerateCert(_ context.Context, _ *transport.GenerateCertRequest) (*transport.CertBundle, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "GenerateCert"}
}

func (t *baasTransport) ExportCert(_ context.Context, _ *transport.ExportCertRequest) (*transport.ExportCertResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "ExportCert"}
}

func (t *baasTransport) RevokeCert(_ context.Context, _ string) error {
	return &BaasTransportNotSupportedError{Operation: "RevokeCert"}
}

func (t *baasTransport) ListCerts(_ context.Context, _ string) (*transport.ListCertsResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "ListCerts"}
}

// ---------------------------------------------------------------------------
// transport.BarrierClient - stub implementations
// ---------------------------------------------------------------------------

func (t *baasTransport) BarrierInitialize(_ context.Context, _ *transport.BarrierInitializeRequest) error {
	return &BaasTransportNotSupportedError{Operation: "BarrierInitialize"}
}

func (t *baasTransport) BarrierUnseal(_ context.Context, _ *transport.BarrierUnsealRequest) error {
	return &BaasTransportNotSupportedError{Operation: "BarrierUnseal"}
}

func (t *baasTransport) BarrierSeal(_ context.Context) error {
	return &BaasTransportNotSupportedError{Operation: "BarrierSeal"}
}

func (t *baasTransport) BarrierStatus(_ context.Context) (*transport.BarrierStatusResponse, error) {
	return &transport.BarrierStatusResponse{}, nil
}

func (t *baasTransport) BarrierInitializeShamir(_ context.Context, _ *transport.BarrierInitializeShamirRequest) (*transport.BarrierInitializeShamirResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "BarrierInitializeShamir"}
}

func (t *baasTransport) BarrierUnsealWithShare(_ context.Context, _ *transport.BarrierUnsealShareRequest) (*transport.BarrierUnsealShareResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "BarrierUnsealWithShare"}
}

func (t *baasTransport) BarrierUnsealWithShares(_ context.Context, _ *transport.BarrierUnsealSharesRequest) error {
	return &BaasTransportNotSupportedError{Operation: "BarrierUnsealWithShares"}
}

func (t *baasTransport) BarrierShamirListShares(_ context.Context) (*transport.BarrierShamirSharesResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "BarrierShamirListShares"}
}

func (t *baasTransport) BarrierShamirDeleteShare(_ context.Context, _ *transport.BarrierShamirDeleteShareRequest) error {
	return &BaasTransportNotSupportedError{Operation: "BarrierShamirDeleteShare"}
}

func (t *baasTransport) BarrierShamirDeleteAllShares(_ context.Context) error {
	return &BaasTransportNotSupportedError{Operation: "BarrierShamirDeleteAllShares"}
}

func (t *baasTransport) BarrierShamirVerify(_ context.Context) error {
	return &BaasTransportNotSupportedError{Operation: "BarrierShamirVerify"}
}

func (t *baasTransport) BarrierRekey(_ context.Context, _ *transport.BarrierRekeyRequest) (*transport.BarrierRekeyResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "BarrierRekey"}
}

func (t *baasTransport) BarrierGenerateRecoveryKeys(_ context.Context, _ *transport.BarrierGenerateRecoveryKeysRequest) (*transport.BarrierRecoveryKeysResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "BarrierGenerateRecoveryKeys"}
}

func (t *baasTransport) BarrierRecoverWithKeys(_ context.Context, _ *transport.BarrierRecoverWithKeysRequest) error {
	return &BaasTransportNotSupportedError{Operation: "BarrierRecoverWithKeys"}
}

func (t *baasTransport) BarrierDeleteRecoveryKeys(_ context.Context) error {
	return &BaasTransportNotSupportedError{Operation: "BarrierDeleteRecoveryKeys"}
}

func (t *baasTransport) BarrierHasRecoveryKeys(_ context.Context) (*transport.BarrierHasRecoveryKeysResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "BarrierHasRecoveryKeys"}
}

func (t *baasTransport) BarrierGenerateRootToken(_ context.Context, _ *transport.BarrierGenerateRootTokenRequest) (*transport.BarrierRootTokenResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "BarrierGenerateRootToken"}
}

// ---------------------------------------------------------------------------
// transport.DatabaseClient - implemented in baas_transport_database.go
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// transport.CeremonyClient - stub implementations
// ---------------------------------------------------------------------------

func (t *baasTransport) CeremonyInitialize(_ context.Context, _ *transport.CeremonyInitializeRequest) (*transport.CeremonyInitializeResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "CeremonyInitialize"}
}

func (t *baasTransport) CeremonyStatus(_ context.Context) (*transport.CeremonyStatusResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "CeremonyStatus"}
}

func (t *baasTransport) CeremonyClaimCertBegin(_ context.Context, _ *transport.ClaimCertBeginRequest) (*transport.ClaimCertBeginResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "CeremonyClaimCertBegin"}
}

func (t *baasTransport) CeremonyClaimCertComplete(_ context.Context, _ *transport.ClaimCertCompleteRequest) (*transport.ClaimCertCompleteResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "CeremonyClaimCertComplete"}
}

func (t *baasTransport) CeremonyClaimShare(_ context.Context, _ *transport.ClaimShareRequest) (*transport.ClaimShareResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "CeremonyClaimShare"}
}

func (t *baasTransport) CeremonySignCSR(_ context.Context, _ *transport.SignCSRInitRequest) (*transport.SignCSRInitResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "CeremonySignCSR"}
}

// ---------------------------------------------------------------------------
// transport.CredentialClient - stub implementations
// ---------------------------------------------------------------------------

func (t *baasTransport) CredentialSubmit(_ context.Context, _ *transport.CredentialSubmitRequest) error {
	return &BaasTransportNotSupportedError{Operation: "CredentialSubmit"}
}

func (t *baasTransport) CredentialStrategy(_ context.Context) (*transport.CredentialStrategyResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "CredentialStrategy"}
}

func (t *baasTransport) CredentialDelete(_ context.Context, _ string) error {
	return &BaasTransportNotSupportedError{Operation: "CredentialDelete"}
}

// ---------------------------------------------------------------------------
// transport.CollectionClient - stub implementations
// ---------------------------------------------------------------------------

func (t *baasTransport) CreateCollection(_ context.Context, _ transport.CreateCollectionRequest) (*transport.CollectionInfo, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "CreateCollection"}
}

func (t *baasTransport) GetCollectionInfo(_ context.Context, _, _ string) (*transport.CollectionInfo, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "GetCollectionInfo"}
}

func (t *baasTransport) ListCollections(_ context.Context, _ string) (*transport.ListCollectionsResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "ListCollections"}
}

func (t *baasTransport) DropCollection(_ context.Context, _, _ string) error {
	return &BaasTransportNotSupportedError{Operation: "DropCollection"}
}

func (t *baasTransport) InsertDocument(_ context.Context, _ transport.InsertDocumentRequest) (*transport.InsertDocumentResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "InsertDocument"}
}

func (t *baasTransport) GetDocument(_ context.Context, _, _ string, _ uint64) (*transport.DocumentResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "GetDocument"}
}

func (t *baasTransport) UpdateDocument(_ context.Context, _ transport.UpdateDocumentRequest) error {
	return &BaasTransportNotSupportedError{Operation: "UpdateDocument"}
}

func (t *baasTransport) DeleteDocument(_ context.Context, _, _ string, _ uint64) error {
	return &BaasTransportNotSupportedError{Operation: "DeleteDocument"}
}

func (t *baasTransport) ListDocuments(_ context.Context, _ transport.ListDocumentsRequest) (*transport.ListDocumentsResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "ListDocuments"}
}

// ---------------------------------------------------------------------------
// transport.BackupClient - stub implementations
// ---------------------------------------------------------------------------

func (t *baasTransport) CreateBackup(_ context.Context, _ *transport.BackupRequest) (*transport.BackupInfo, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "CreateBackup"}
}

func (t *baasTransport) ListBackups(_ context.Context) ([]transport.BackupInfo, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "ListBackups"}
}

func (t *baasTransport) GetBackup(_ context.Context, _ string) (*transport.BackupInfo, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "GetBackup"}
}

func (t *baasTransport) DeleteBackup(_ context.Context, _ string) error {
	return &BaasTransportNotSupportedError{Operation: "DeleteBackup"}
}

func (t *baasTransport) RestoreBackup(_ context.Context, _ *transport.RestoreRequest) (*transport.RestoreResult, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "RestoreBackup"}
}

func (t *baasTransport) VerifyBackup(_ context.Context, _ string) (*transport.VerifyResult, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "VerifyBackup"}
}

// ---------------------------------------------------------------------------
// transport.RebalanceClient - stub implementations
// ---------------------------------------------------------------------------

func (t *baasTransport) GetShardStats(_ context.Context, _ uint64) (*transport.ShardStatsResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "GetShardStats"}
}

func (t *baasTransport) CreateRebalancePlan(_ context.Context, _ *transport.RebalancePlanRequest) (*transport.RebalancePlan, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "CreateRebalancePlan"}
}

func (t *baasTransport) ExecuteRebalance(_ context.Context, _ *transport.RebalancePlan) (*transport.RebalanceResult, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "ExecuteRebalance"}
}

func (t *baasTransport) GetImbalanceReport(_ context.Context) (*transport.ImbalanceReport, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "GetImbalanceReport"}
}

func (t *baasTransport) IndexCreate(_ context.Context, _, _ string, _ transport.IndexConfig) error {
	return &BaasTransportNotSupportedError{Operation: "IndexCreate"}
}

func (t *baasTransport) IndexDrop(_ context.Context, _ string) error {
	return &BaasTransportNotSupportedError{Operation: "IndexDrop"}
}

func (t *baasTransport) IndexQuery(_ context.Context, _, _ string) (*transport.IndexQueryResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "IndexQuery"}
}

func (t *baasTransport) IndexScan(_ context.Context, _, _, _ string) (*transport.IndexScanResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "IndexScan"}
}

func (t *baasTransport) IndexList(_ context.Context) (*transport.IndexListResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "IndexList"}
}

// ---------------------------------------------------------------------------
// baasTransportStore manages per-shard in-memory stores and their
// associated transports for the BaaS embedded transport mode.
// ---------------------------------------------------------------------------

// BaasTransportStoreError indicates a failure creating a per-shard store.
type BaasTransportStoreError struct {
	ShardID uint64
	Backend string
	Err     error
}

// Error implements the error interface.
func (e *BaasTransportStoreError) Error() string {
	return fmt.Sprintf("baas transport store: shard %d backend %s: %v", e.ShardID, e.Backend, e.Err)
}

// Unwrap returns the underlying error.
func (e *BaasTransportStoreError) Unwrap() error {
	return e.Err
}

// baasTransportStore manages per-shard KVStore instances and creates
// baasTransport wrappers on demand. Supports memory, file, and pebble
// backends based on storage configuration. When vectorStorage is set
// and a shard matches the vector shard ID, the transport delegates
// vector write operations to the real vector storage backend. When
// timeSeriesStorage is set and a shard matches the timeseries shard
// ID, the transport delegates timeseries operations to the real
// timeseries storage backend. Thread-safe via sync.Map.
type baasTransportStore struct {
	storageCfg        *config.StorageConfig
	vectorStorage     interfaces.VectorStorage
	vectorShardID     uint64
	timeSeriesStorage interfaces.TimeSeriesStorage
	timeSeriesShardID uint64
	dbService         service.DatabaseService
	lockService       BaasLockService
	stores            sync.Map // map[uint64]interfaces.KVStore
}

// newBaasTransportStore creates a new BaaS transport store with the
// given storage configuration. When storageCfg is nil or backend is
// empty, defaults to in-memory storage.
func newBaasTransportStore(storageCfg *config.StorageConfig) *baasTransportStore {
	return &baasTransportStore{storageCfg: storageCfg}
}

// backend returns the effective storage backend type.
func (s *baasTransportStore) backend() string {
	if s.storageCfg == nil || s.storageCfg.Backend == "" {
		return config.StorageBackendMemory
	}
	return s.storageCfg.Backend
}

// dataDir returns the effective data directory.
func (s *baasTransportStore) dataDir() string {
	if s.storageCfg == nil {
		return ""
	}
	return s.storageCfg.DataDir
}

// storeForShard returns the KVStore for the given shard, creating one
// on first access. The store type depends on the storage backend config:
//   - "memory": MemoryKVStore (in-memory, no persistence)
//   - "file":   FileKVStore (file-per-key persistence)
//   - "pebble": PebbleKVStore (PebbleDB high-performance persistence)
func (s *baasTransportStore) storeForShard(shardID uint64) (interfaces.KVStore, error) {
	if existing, ok := s.stores.Load(shardID); ok {
		return existing.(interfaces.KVStore), nil //nolint:errcheck // type guaranteed by map contract
	}

	store, err := s.createStore(shardID)
	if err != nil {
		return nil, err
	}

	actual, _ := s.stores.LoadOrStore(shardID, store)
	return actual.(interfaces.KVStore), nil //nolint:errcheck // type guaranteed by map contract
}

// createStore creates a new KVStore for the given shard based on the
// configured storage backend.
func (s *baasTransportStore) createStore(shardID uint64) (interfaces.KVStore, error) {
	switch s.backend() {
	case config.StorageBackendPebble:
		dir := filepath.Join(s.dataDir(), fmt.Sprintf("shard-%d", shardID))
		store, err := NewPebbleKVStore(dir)
		if err != nil {
			return nil, &BaasTransportStoreError{ShardID: shardID, Backend: "pebble", Err: err}
		}
		return store, nil

	case config.StorageBackendFile:
		dir := filepath.Join(s.dataDir(), fmt.Sprintf("shard-%d", shardID))
		if err := os.MkdirAll(dir, 0750); err != nil {
			return nil, &BaasTransportStoreError{ShardID: shardID, Backend: "file", Err: err}
		}
		store, err := NewFileKVStore(dir)
		if err != nil {
			return nil, &BaasTransportStoreError{ShardID: shardID, Backend: "file", Err: err}
		}
		return store, nil

	default:
		return NewMemoryKVStore(), nil
	}
}

// transportForShard returns a baasTransport backed by the shard's KVStore.
// If the shard matches the configured vector shard ID and vectorStorage
// is available, the transport will delegate vector write operations to
// the real vector storage backend. If the shard matches the configured
// timeseries shard ID and timeSeriesStorage is available, the transport
// will delegate timeseries operations to the real timeseries storage backend.
// Returns an error if the store cannot be created for persistent backends.
func (s *baasTransportStore) transportForShard(shardID uint64) (*baasTransport, error) {
	store, err := s.storeForShard(shardID)
	if err != nil {
		return nil, err
	}

	// Inject vector storage into the transport for the vector shard
	// so that writes go to the same PebbleVectorStorage that the
	// vectorService reads from, eliminating the write-read mismatch.
	var vs interfaces.VectorStorage
	if s.vectorStorage != nil && shardID == s.vectorShardID {
		vs = s.vectorStorage
	}

	// Inject timeseries storage into the transport for the timeseries
	// shard so that writes go to the same PebbleDB timeseries storage
	// that the timeSeriesService reads from, eliminating the write-read
	// mismatch.
	var ts interfaces.TimeSeriesStorage
	if s.timeSeriesStorage != nil && shardID == s.timeSeriesShardID {
		ts = s.timeSeriesStorage
	}

	return newBaasTransport(store, vs, ts, s.dbService, s.lockService), nil
}

// Close closes all per-shard stores that implement io.Closer (PebbleDB, File).
// Memory stores do not require closing.
func (s *baasTransportStore) Close() error {
	var firstErr error
	s.stores.Range(func(key, value any) bool {
		if closer, ok := value.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		s.stores.Delete(key)
		return true
	})
	return firstErr
}

// ---------------------------------------------------------------------------
// transport.DEKClient - stub implementations
// ---------------------------------------------------------------------------

func (t *baasTransport) DEKInitialize(_ context.Context, _ *transport.DEKInitializeRequest) error {
	return &BaasTransportNotSupportedError{Operation: "DEKInitialize"}
}

func (t *baasTransport) DEKGetStatus(_ context.Context) (*transport.DEKStatusResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "DEKGetStatus"}
}

func (t *baasTransport) DEKRotate(_ context.Context) error {
	return &BaasTransportNotSupportedError{Operation: "DEKRotate"}
}

func (t *baasTransport) DEKGetCurrent(_ context.Context) (*transport.DEKCurrentResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "DEKGetCurrent"}
}

func (t *baasTransport) DEKSyncToNode(_ context.Context, _ *transport.DEKSyncRequest) error {
	return &BaasTransportNotSupportedError{Operation: "DEKSyncToNode"}
}

func (t *baasTransport) TenantDEKGetOrCreate(_ context.Context, _ *transport.TenantDEKRequest) (*transport.TenantDEKResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "TenantDEKGetOrCreate"}
}

func (t *baasTransport) TenantDEKRotate(_ context.Context, _ *transport.TenantDEKRotateRequest) error {
	return &BaasTransportNotSupportedError{Operation: "TenantDEKRotate"}
}

func (t *baasTransport) TenantDEKDelete(_ context.Context, _ *transport.TenantDEKDeleteRequest) error {
	return &BaasTransportNotSupportedError{Operation: "TenantDEKDelete"}
}

func (t *baasTransport) TenantDEKList(_ context.Context) (*transport.TenantDEKListResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "TenantDEKList"}
}

func (t *baasTransport) TenantDEKGetStatus(_ context.Context, _ *transport.TenantDEKRequest) (*transport.TenantDEKStatusResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "TenantDEKGetStatus"}
}

// ---------------------------------------------------------------------------
// transport.SealStoreClient - stub implementations
// ---------------------------------------------------------------------------

func (t *baasTransport) SealStorePut(_ context.Context, _ *transport.SealStorePutRequest) error {
	return &BaasTransportNotSupportedError{Operation: "SealStorePut"}
}

func (t *baasTransport) SealStoreGet(_ context.Context, _ *transport.SealStoreGetRequest) (*transport.SealStoreGetResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "SealStoreGet"}
}

func (t *baasTransport) SealStoreDelete(_ context.Context, _ *transport.SealStoreDeleteRequest) error {
	return &BaasTransportNotSupportedError{Operation: "SealStoreDelete"}
}

func (t *baasTransport) SealStoreList(_ context.Context) (*transport.SealStoreListResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "SealStoreList"}
}

func (t *baasTransport) SealStoreReseal(_ context.Context, _ *transport.SealStoreResealRequest) error {
	return &BaasTransportNotSupportedError{Operation: "SealStoreReseal"}
}

func (t *baasTransport) SealStoreStatus(_ context.Context) (*transport.SealStoreStatusResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "SealStoreStatus"}
}

// ---------------------------------------------------------------------------
// transport.CustodianGroupClient - stub implementations
// ---------------------------------------------------------------------------

func (t *baasTransport) CreateCustodianGroup(_ context.Context, _ *transport.CreateCustodianGroupRequest) (*transport.CreateCustodianGroupResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "CreateCustodianGroup"}
}

func (t *baasTransport) GetCustodianGroup(_ context.Context, _ string) (*transport.GetCustodianGroupResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "GetCustodianGroup"}
}

func (t *baasTransport) ListCustodianGroups(_ context.Context) (*transport.ListCustodianGroupsResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "ListCustodianGroups"}
}

func (t *baasTransport) DeleteCustodianGroup(_ context.Context, _ string) error {
	return &BaasTransportNotSupportedError{Operation: "DeleteCustodianGroup"}
}

func (t *baasTransport) AddCustodianMember(_ context.Context, _ *transport.AddCustodianMemberRequest) (*transport.AddCustodianMemberResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "AddCustodianMember"}
}

func (t *baasTransport) RemoveCustodianMember(_ context.Context, _ *transport.RemoveCustodianMemberRequest) error {
	return &BaasTransportNotSupportedError{Operation: "RemoveCustodianMember"}
}

func (t *baasTransport) DistributeShares(_ context.Context, _ *transport.DistributeSharesRequest) (*transport.DistributeSharesResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "DistributeShares"}
}

// ---------------------------------------------------------------------------
// transport.ShareClient - stub implementations
// ---------------------------------------------------------------------------

func (t *baasTransport) SubmitShare(_ context.Context, _ *transport.SubmitShareRequest) (*transport.SubmitShareResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "SubmitShare"}
}

func (t *baasTransport) ListShares(_ context.Context) (*transport.ListSharesResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "ListShares"}
}

func (t *baasTransport) GetShareCollectionStatus(_ context.Context, _ string) (*transport.ShareCollectionStatus, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "GetShareCollectionStatus"}
}

// ---------------------------------------------------------------------------
// transport.TenantClient - stub implementations
// ---------------------------------------------------------------------------

func (t *baasTransport) CreateTenant(_ context.Context, _ *transport.CreateTenantRequest) (*transport.CreateTenantResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "CreateTenant"}
}

func (t *baasTransport) GetTenant(_ context.Context, _ string) (*transport.GetTenantResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "GetTenant"}
}

func (t *baasTransport) ListTenants(_ context.Context) (*transport.ListTenantsResponse, error) {
	return nil, &BaasTransportNotSupportedError{Operation: "ListTenants"}
}

func (t *baasTransport) DeleteTenant(_ context.Context, _ string) error {
	return &BaasTransportNotSupportedError{Operation: "DeleteTenant"}
}

func (t *baasTransport) TenantBarrierInit(_ context.Context, _ *transport.TenantBarrierInitRequest) error {
	return &BaasTransportNotSupportedError{Operation: "TenantBarrierInit"}
}

func (t *baasTransport) TenantBarrierUnseal(_ context.Context, _ *transport.TenantBarrierUnsealRequest) error {
	return &BaasTransportNotSupportedError{Operation: "TenantBarrierUnseal"}
}
