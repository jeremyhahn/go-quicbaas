// Copyright 2026 Jeremy Hahn
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package quicbaas

import (
	"context"
	"strings"
	"sync"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	qrdbembedded "github.com/jeremyhahn/go-qrdb/sdk/go/transport/embedded"
	qrdbgrpc "github.com/jeremyhahn/go-qrdb/sdk/go/transport/grpc"
	qrdbmcp "github.com/jeremyhahn/go-qrdb/sdk/go/transport/mcp"
	qrdbquic "github.com/jeremyhahn/go-qrdb/sdk/go/transport/quic"
	qrdbrest "github.com/jeremyhahn/go-qrdb/sdk/go/transport/rest"
	qrdbunix "github.com/jeremyhahn/go-qrdb/sdk/go/transport/unix"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// QRDBClient is a re-export of the go-qrdb SDK Client so consumers do not
// need to import go-qrdb/sdk/go directly.
type QRDBClient = qrdbsdk.Client

// =============================================================================
// QRDB SDK Type Re-exports
// =============================================================================
//
// The following type aliases re-export go-qrdb transport interfaces so that
// QuicBaaS SDK consumers can reference QRDB types without importing
// go-qrdb/sdk/go directly.

// QRDBTransport is a re-export of the qrdb Transport interface.
type QRDBTransport = qrdbtransport.Transport

// QRDBKVClient is a re-export of the qrdb KVClient interface.
type QRDBKVClient = qrdbtransport.KVClient

// QRDBAdminClient is a re-export of the qrdb AdminClient interface.
type QRDBAdminClient = qrdbtransport.AdminClient

// QRDBMVCCClient is a re-export of the qrdb MVCCClient interface.
type QRDBMVCCClient = qrdbtransport.MVCCClient

// QRDBTransactionClient is a re-export of the qrdb TransactionClient interface.
type QRDBTransactionClient = qrdbtransport.TransactionClient

// QRDBBatchClient is a re-export of the qrdb BatchClient interface.
type QRDBBatchClient = qrdbtransport.BatchClient

// QRDBWatchClient is a re-export of the qrdb WatchClient interface.
type QRDBWatchClient = qrdbtransport.WatchClient

// QRDBLeaseClient is a re-export of the qrdb LeaseClient interface.
type QRDBLeaseClient = qrdbtransport.LeaseClient

// QRDBTimeSeriesClient is a re-export of the qrdb TimeSeriesClient interface.
type QRDBTimeSeriesClient = qrdbtransport.TimeSeriesClient

// QRDBVectorClient is a re-export of the qrdb VectorClient interface.
type QRDBVectorClient = qrdbtransport.VectorClient

// QRDBUserClient is a re-export of the qrdb UserClient interface.
type QRDBUserClient = qrdbtransport.UserClient

// QRDBRoleClient is a re-export of the qrdb RoleClient interface.
type QRDBRoleClient = qrdbtransport.RoleClient

// QRDBCertClient is a re-export of the qrdb CertClient interface.
type QRDBCertClient = qrdbtransport.CertClient

// QRDBBarrierClient is a re-export of the qrdb BarrierClient interface.
type QRDBBarrierClient = qrdbtransport.BarrierClient

// QRDBDatabaseClient is a re-export of the qrdb DatabaseClient interface.
type QRDBDatabaseClient = qrdbtransport.DatabaseClient

// QRDBCeremonyClient is a re-export of the qrdb CeremonyClient interface.
type QRDBCeremonyClient = qrdbtransport.CeremonyClient

// QRDBCollectionClient is a re-export of the qrdb CollectionClient interface.
type QRDBCollectionClient = qrdbtransport.CollectionClient

// QRDBCredentialClient is a re-export of the qrdb CredentialClient interface.
type QRDBCredentialClient = qrdbtransport.CredentialClient

// QRDBBackupClient is a re-export of the qrdb BackupClient interface.
type QRDBBackupClient = qrdbtransport.BackupClient

// QRDBRebalanceClient is a re-export of the qrdb RebalanceClient interface.
type QRDBRebalanceClient = qrdbtransport.RebalanceClient

// QRDBSealStoreClient is a re-export of the qrdb SealStoreClient interface.
type QRDBSealStoreClient = qrdbtransport.SealStoreClient

// QRDBCustodianGroupClient is a re-export of the qrdb CustodianGroupClient interface.
type QRDBCustodianGroupClient = qrdbtransport.CustodianGroupClient

// QRDBShareClient is a re-export of the qrdb ShareClient interface.
type QRDBShareClient = qrdbtransport.ShareClient

// QRDBTenantClient is a re-export of the qrdb TenantClient interface.
type QRDBTenantClient = qrdbtransport.TenantClient

// QRDBDEKClient is a re-export of the qrdb DEKClient interface.
type QRDBDEKClient = qrdbtransport.DEKClient

// QRDBIndexClient is a re-export of the qrdb IndexClient interface.
type QRDBIndexClient = qrdbtransport.IndexClient

// =============================================================================
// QRDB Seal Store Request/Response Re-exports
// =============================================================================

// QRDBSealStorePutRequest is a re-export of the qrdb SealStorePutRequest type.
type QRDBSealStorePutRequest = qrdbtransport.SealStorePutRequest

// QRDBSealStoreGetRequest is a re-export of the qrdb SealStoreGetRequest type.
type QRDBSealStoreGetRequest = qrdbtransport.SealStoreGetRequest

// QRDBSealStoreGetResponse is a re-export of the qrdb SealStoreGetResponse type.
type QRDBSealStoreGetResponse = qrdbtransport.SealStoreGetResponse

// QRDBSealStoreDeleteRequest is a re-export of the qrdb SealStoreDeleteRequest type.
type QRDBSealStoreDeleteRequest = qrdbtransport.SealStoreDeleteRequest

// QRDBSealStoreListResponse is a re-export of the qrdb SealStoreListResponse type.
type QRDBSealStoreListResponse = qrdbtransport.SealStoreListResponse

// QRDBSealStoreResealRequest is a re-export of the qrdb SealStoreResealRequest type.
type QRDBSealStoreResealRequest = qrdbtransport.SealStoreResealRequest

// QRDBSealStoreStatusResponse is a re-export of the qrdb SealStoreStatusResponse type.
type QRDBSealStoreStatusResponse = qrdbtransport.SealStoreStatusResponse

// =============================================================================
// QRDB Custodian Group Request/Response Re-exports
// =============================================================================

// QRDBCustodianGroupInfo is a re-export of the qrdb CustodianGroupInfo type.
type QRDBCustodianGroupInfo = qrdbtransport.CustodianGroupInfo

// QRDBCustodianMemberInfo is a re-export of the qrdb CustodianMemberInfo type.
type QRDBCustodianMemberInfo = qrdbtransport.CustodianMemberInfo

// QRDBCreateCustodianGroupRequest is a re-export of the qrdb CreateCustodianGroupRequest type.
type QRDBCreateCustodianGroupRequest = qrdbtransport.CreateCustodianGroupRequest

// QRDBCreateCustodianGroupResponse is a re-export of the qrdb CreateCustodianGroupResponse type.
type QRDBCreateCustodianGroupResponse = qrdbtransport.CreateCustodianGroupResponse

// QRDBGetCustodianGroupResponse is a re-export of the qrdb GetCustodianGroupResponse type.
type QRDBGetCustodianGroupResponse = qrdbtransport.GetCustodianGroupResponse

// QRDBListCustodianGroupsResponse is a re-export of the qrdb ListCustodianGroupsResponse type.
type QRDBListCustodianGroupsResponse = qrdbtransport.ListCustodianGroupsResponse

// QRDBAddCustodianMemberRequest is a re-export of the qrdb AddCustodianMemberRequest type.
type QRDBAddCustodianMemberRequest = qrdbtransport.AddCustodianMemberRequest

// QRDBAddCustodianMemberResponse is a re-export of the qrdb AddCustodianMemberResponse type.
type QRDBAddCustodianMemberResponse = qrdbtransport.AddCustodianMemberResponse

// QRDBRemoveCustodianMemberRequest is a re-export of the qrdb RemoveCustodianMemberRequest type.
type QRDBRemoveCustodianMemberRequest = qrdbtransport.RemoveCustodianMemberRequest

// QRDBDistributeSharesRequest is a re-export of the qrdb DistributeSharesRequest type.
type QRDBDistributeSharesRequest = qrdbtransport.DistributeSharesRequest

// QRDBDistributeSharesResponse is a re-export of the qrdb DistributeSharesResponse type.
type QRDBDistributeSharesResponse = qrdbtransport.DistributeSharesResponse

// =============================================================================
// QRDB Share Request/Response Re-exports
// =============================================================================

// QRDBSubmitShareRequest is a re-export of the qrdb SubmitShareRequest type.
type QRDBSubmitShareRequest = qrdbtransport.SubmitShareRequest

// QRDBSubmitShareResponse is a re-export of the qrdb SubmitShareResponse type.
type QRDBSubmitShareResponse = qrdbtransport.SubmitShareResponse

// QRDBShareInfo is a re-export of the qrdb ShareInfo type.
type QRDBShareInfo = qrdbtransport.ShareInfo

// QRDBListSharesResponse is a re-export of the qrdb ListSharesResponse type.
type QRDBListSharesResponse = qrdbtransport.ListSharesResponse

// QRDBShareCollectionStatus is a re-export of the qrdb ShareCollectionStatus type.
type QRDBShareCollectionStatus = qrdbtransport.ShareCollectionStatus

// =============================================================================
// QRDB Tenant Request/Response Re-exports
// =============================================================================

// QRDBTenantInfo is a re-export of the qrdb TenantInfo type.
type QRDBTenantInfo = qrdbtransport.TenantInfo

// QRDBCreateTenantRequest is a re-export of the qrdb CreateTenantRequest type.
type QRDBCreateTenantRequest = qrdbtransport.CreateTenantRequest

// QRDBCreateTenantResponse is a re-export of the qrdb CreateTenantResponse type.
type QRDBCreateTenantResponse = qrdbtransport.CreateTenantResponse

// QRDBGetTenantResponse is a re-export of the qrdb GetTenantResponse type.
type QRDBGetTenantResponse = qrdbtransport.GetTenantResponse

// QRDBListTenantsResponse is a re-export of the qrdb ListTenantsResponse type.
type QRDBListTenantsResponse = qrdbtransport.ListTenantsResponse

// QRDBTenantBarrierInitRequest is a re-export of the qrdb TenantBarrierInitRequest type.
type QRDBTenantBarrierInitRequest = qrdbtransport.TenantBarrierInitRequest

// QRDBTenantBarrierUnsealRequest is a re-export of the qrdb TenantBarrierUnsealRequest type.
type QRDBTenantBarrierUnsealRequest = qrdbtransport.TenantBarrierUnsealRequest

// QRDBTenantBarrierSealRequest is a re-export of the qrdb TenantBarrierSealRequest type.
type QRDBTenantBarrierSealRequest = qrdbtransport.TenantBarrierSealRequest

// QRDBTenantBarrierStatusRequest is a re-export of the qrdb TenantBarrierStatusRequest type.
type QRDBTenantBarrierStatusRequest = qrdbtransport.TenantBarrierStatusRequest

// QRDBTenantBarrierStatusResponse is a re-export of the qrdb TenantBarrierStatusResponse type.
type QRDBTenantBarrierStatusResponse = qrdbtransport.TenantBarrierStatusResponse

// QRDBTenantBarrierInitResponse is a re-export of the qrdb TenantBarrierInitResponse type.
// For the Shamir strategy it carries the generated M-of-N shares (returned
// exactly once, never persisted) plus the threshold/total.
type QRDBTenantBarrierInitResponse = qrdbtransport.TenantBarrierInitResponse

// QRDBTenantBarrierUnsealShareRequest is a re-export of the qrdb TenantBarrierUnsealShareRequest type.
type QRDBTenantBarrierUnsealShareRequest = qrdbtransport.TenantBarrierUnsealShareRequest

// QRDBTenantBarrierUnsealProgress is a re-export of the qrdb TenantBarrierUnsealProgress type.
type QRDBTenantBarrierUnsealProgress = qrdbtransport.TenantBarrierUnsealProgress

// =============================================================================
// QRDB Barrier Request/Response Re-exports
// =============================================================================

// QRDBBarrierInitializeRequest is a re-export of the qrdb BarrierInitializeRequest type.
type QRDBBarrierInitializeRequest = qrdbtransport.BarrierInitializeRequest

// QRDBBarrierInitializeShamirRequest is a re-export of the qrdb BarrierInitializeShamirRequest type.
type QRDBBarrierInitializeShamirRequest = qrdbtransport.BarrierInitializeShamirRequest

// QRDBBarrierInitializeShamirResponse is a re-export of the qrdb BarrierInitializeShamirResponse type.
type QRDBBarrierInitializeShamirResponse = qrdbtransport.BarrierInitializeShamirResponse

// QRDBBarrierUnsealRequest is a re-export of the qrdb BarrierUnsealRequest type.
type QRDBBarrierUnsealRequest = qrdbtransport.BarrierUnsealRequest

// QRDBBarrierUnsealShareRequest is a re-export of the qrdb BarrierUnsealShareRequest type.
type QRDBBarrierUnsealShareRequest = qrdbtransport.BarrierUnsealShareRequest

// QRDBBarrierUnsealShareResponse is a re-export of the qrdb BarrierUnsealShareResponse type.
type QRDBBarrierUnsealShareResponse = qrdbtransport.BarrierUnsealShareResponse

// QRDBBarrierStatusResponse is a re-export of the qrdb BarrierStatusResponse type.
type QRDBBarrierStatusResponse = qrdbtransport.BarrierStatusResponse

// QRDBBarrierRekeyRequest is a re-export of the qrdb BarrierRekeyRequest type.
type QRDBBarrierRekeyRequest = qrdbtransport.BarrierRekeyRequest

// QRDBBarrierRekeyResponse is a re-export of the qrdb BarrierRekeyResponse type.
type QRDBBarrierRekeyResponse = qrdbtransport.BarrierRekeyResponse

// QRDBBarrierUnsealSharesRequest is a re-export of the qrdb BarrierUnsealSharesRequest type.
type QRDBBarrierUnsealSharesRequest = qrdbtransport.BarrierUnsealSharesRequest

// QRDBBarrierGenerateRecoveryKeysRequest is a re-export of the qrdb BarrierGenerateRecoveryKeysRequest type.
type QRDBBarrierGenerateRecoveryKeysRequest = qrdbtransport.BarrierGenerateRecoveryKeysRequest

// QRDBBarrierRecoveryKeysResponse is a re-export of the qrdb BarrierRecoveryKeysResponse type.
type QRDBBarrierRecoveryKeysResponse = qrdbtransport.BarrierRecoveryKeysResponse

// QRDBBarrierRecoverWithKeysRequest is a re-export of the qrdb BarrierRecoverWithKeysRequest type.
type QRDBBarrierRecoverWithKeysRequest = qrdbtransport.BarrierRecoverWithKeysRequest

// QRDBBarrierHasRecoveryKeysResponse is a re-export of the qrdb BarrierHasRecoveryKeysResponse type.
type QRDBBarrierHasRecoveryKeysResponse = qrdbtransport.BarrierHasRecoveryKeysResponse

// QRDBBarrierGenerateRootTokenRequest is a re-export of the qrdb BarrierGenerateRootTokenRequest type.
type QRDBBarrierGenerateRootTokenRequest = qrdbtransport.BarrierGenerateRootTokenRequest

// QRDBBarrierRootTokenResponse is a re-export of the qrdb BarrierRootTokenResponse type.
type QRDBBarrierRootTokenResponse = qrdbtransport.BarrierRootTokenResponse

// QRDBBarrierEnableEncryptionRequest is a re-export of the qrdb BarrierEnableEncryptionRequest type.
type QRDBBarrierEnableEncryptionRequest = qrdbtransport.BarrierEnableEncryptionRequest

// QRDBBarrierEnableEncryptionResponse is a re-export of the qrdb BarrierEnableEncryptionResponse type.
type QRDBBarrierEnableEncryptionResponse = qrdbtransport.BarrierEnableEncryptionResponse

// =============================================================================
// QRDB Security Officer / Share-Claim Request/Response Re-exports
// =============================================================================

// QRDBOfficerRegisterRequest is a re-export of the qrdb OfficerRegisterRequest type.
type QRDBOfficerRegisterRequest = qrdbtransport.OfficerRegisterRequest

// QRDBOfficerRegisterResponse is a re-export of the qrdb OfficerRegisterResponse type.
type QRDBOfficerRegisterResponse = qrdbtransport.OfficerRegisterResponse

// QRDBOfficerStatus is a re-export of the qrdb OfficerStatus type.
type QRDBOfficerStatus = qrdbtransport.OfficerStatus

// QRDBShareClaimBeginRequest is a re-export of the qrdb ShareClaimBeginRequest type.
type QRDBShareClaimBeginRequest = qrdbtransport.ShareClaimBeginRequest

// QRDBShareClaimBeginResponse is a re-export of the qrdb ShareClaimBeginResponse type.
type QRDBShareClaimBeginResponse = qrdbtransport.ShareClaimBeginResponse

// QRDBShareClaimCompleteRequest is a re-export of the qrdb ShareClaimCompleteRequest type.
type QRDBShareClaimCompleteRequest = qrdbtransport.ShareClaimCompleteRequest

// QRDBShareClaimCompleteResponse is a re-export of the qrdb ShareClaimCompleteResponse type.
type QRDBShareClaimCompleteResponse = qrdbtransport.ShareClaimCompleteResponse

// QRDBShamirSealVoteRequest is a re-export of the qrdb ShamirSealVoteRequest type.
type QRDBShamirSealVoteRequest = qrdbtransport.ShamirSealVoteRequest

// QRDBShamirSealVoteResponse is a re-export of the qrdb ShamirSealVoteResponse type.
type QRDBShamirSealVoteResponse = qrdbtransport.ShamirSealVoteResponse

// =============================================================================
// QRDB Ceremony Request/Response Re-exports
// =============================================================================

// QRDBCeremonyOfficerRequest is a re-export of the qrdb CeremonyOfficerRequest type.
type QRDBCeremonyOfficerRequest = qrdbtransport.CeremonyOfficerRequest

// QRDBCeremonyInitializeRequest is a re-export of the qrdb CeremonyInitializeRequest type.
type QRDBCeremonyInitializeRequest = qrdbtransport.CeremonyInitializeRequest

// QRDBCeremonyInitializeResponse is a re-export of the qrdb CeremonyInitializeResponse type.
type QRDBCeremonyInitializeResponse = qrdbtransport.CeremonyInitializeResponse

// QRDBCeremonyStatusResponse is a re-export of the qrdb CeremonyStatusResponse type.
type QRDBCeremonyStatusResponse = qrdbtransport.CeremonyStatusResponse

// QRDBClaimCertBeginRequest is a re-export of the qrdb ClaimCertBeginRequest type.
type QRDBClaimCertBeginRequest = qrdbtransport.ClaimCertBeginRequest

// QRDBClaimCertBeginResponse is a re-export of the qrdb ClaimCertBeginResponse type.
type QRDBClaimCertBeginResponse = qrdbtransport.ClaimCertBeginResponse

// QRDBClaimCertCompleteRequest is a re-export of the qrdb ClaimCertCompleteRequest type.
type QRDBClaimCertCompleteRequest = qrdbtransport.ClaimCertCompleteRequest

// QRDBClaimCertCompleteResponse is a re-export of the qrdb ClaimCertCompleteResponse type.
type QRDBClaimCertCompleteResponse = qrdbtransport.ClaimCertCompleteResponse

// QRDBClaimShareRequest is a re-export of the qrdb ClaimShareRequest type.
type QRDBClaimShareRequest = qrdbtransport.ClaimShareRequest

// QRDBClaimShareResponse is a re-export of the qrdb ClaimShareResponse type.
type QRDBClaimShareResponse = qrdbtransport.ClaimShareResponse

// QRDBSignCSRInitRequest is a re-export of the qrdb SignCSRInitRequest type.
type QRDBSignCSRInitRequest = qrdbtransport.SignCSRInitRequest

// QRDBSignCSRInitResponse is a re-export of the qrdb SignCSRInitResponse type.
type QRDBSignCSRInitResponse = qrdbtransport.SignCSRInitResponse

// =============================================================================
// QRDB KVStore Re-exports
// =============================================================================

// QRDBKVStore is a re-export of the qrdb KVStore interface.
type QRDBKVStore = xkmssdk.QRDBKVStore

// =============================================================================
// QRDB DAO Re-exports
// =============================================================================

// QRDBEntity is a re-export of the qrdb Entity interface.
type QRDBEntity = xkmssdk.QRDBEntity

// QRDBGenericDAO is a re-export of the qrdb GenericDAO interface.
type QRDBGenericDAO[E xkmssdk.QRDBEntity] = xkmssdk.QRDBGenericDAO[E]

// QRDBDAOOption is a re-export of the qrdb DAO Option type.
type QRDBDAOOption = xkmssdk.QRDBDAOOption

// QRDBPageQuery is a re-export of the qrdb PageQuery type.
type QRDBPageQuery = xkmssdk.QRDBPageQuery

// QRDBPageResult is a re-export of the qrdb PageResult type.
type QRDBPageResult[E any] = xkmssdk.QRDBPageResult[E]

// NewDAO creates a new GenericDAO backed by the given KVStore.
func NewDAO[E xkmssdk.QRDBEntity](store QRDBKVStore, entityType string, newEntity func() E, opts ...QRDBDAOOption) (QRDBGenericDAO[E], error) {
	return xkmssdk.QRDBNewDAO(store, entityType, newEntity, opts...)
}

// IsDAONotFound returns true if the error indicates an entity was not found.
var IsDAONotFound = xkmssdk.QRDBIsDAONotFound

// =============================================================================
// Database Error Re-exports
// =============================================================================
//
// DatabaseError replaces the legacy QBError name from go-qrdb, providing
// a project-neutral error type for database operations.

// DatabaseError is a structured database error with error code, operation
// context, and optional retry semantics.
type DatabaseError = xkmssdk.QRDBError

// DatabaseErrorCode represents a database error classification.
type DatabaseErrorCode = xkmssdk.QRDBErrorCode

// Database error code constants.
const (
	ErrNotFound    = xkmssdk.QRDBErrCodeNotFound
	ErrInternal    = xkmssdk.QRDBErrCodeInternal
	ErrConflict    = xkmssdk.QRDBErrCodeConflict
	ErrTimeout     = xkmssdk.QRDBErrCodeTimeout
	ErrUnavailable = xkmssdk.QRDBErrCodeUnavailable
)

// NewDatabaseError creates a new DatabaseError with the given code and operation.
func NewDatabaseError(code DatabaseErrorCode, op string, message string) *DatabaseError {
	return xkmssdk.QRDBNewError(code, op, message)
}

// IsDatabaseError extracts a DatabaseError from the error chain if present.
func IsDatabaseError(err error, target **DatabaseError) bool {
	return xkmssdk.QRDBAsError(err, target)
}

// IsDatabaseErrorCode checks if the error matches the given database error code.
func IsDatabaseErrorCode(err error, code DatabaseErrorCode) bool {
	return xkmssdk.QRDBIsErrorCode(err, code)
}

// qrdbState holds the lazily-initialized qrdb client and any init error.
type qrdbState struct {
	once   sync.Once
	client *qrdbsdk.Client
	err    error
}

// qrdbClient returns the lazily-initialized QRDB SDK client. The transport is
// chosen to match the quicbaas transport type and address.
func (c *client) qrdbClient() (*QRDBClient, error) {
	c.qrdb.once.Do(func() {
		c.qrdb.client, c.qrdb.err = c.buildQRDBClient()
	})
	return c.qrdb.client, c.qrdb.err
}

// buildQRDBClient constructs the qrdb transport and client from the
// existing clientOptions.
//
// Dispatch order:
//  1. If WithEmbeddedQRDB has installed qrdb servicers, the qrdb sub-client
//     is built over the in-process qrdb embedded transport regardless of
//     the quicbaas transport selection. This is the path quicbaasd takes
//     when it composes itself with WithEmbedded(facade) + WithEmbeddedQRDB:
//     the quicbaas surface talks to the in-process facade, the qrdb surface
//     talks to the in-process qrdb services, and no network listener is
//     required.
//  2. If the quicbaas transport is TransportEmbedded but no qrdb servicers
//     have been installed, the qrdb sub-client cannot fall through to the
//     network (there is no listener) — return an explicit configuration
//     error so callers see the missing wiring immediately.
//  3. Otherwise, the qrdb transport is constructed against the configured
//     address. The transport's Connect method is invoked to perform the
//     underlying handshake; we swallow the QRDB-level Health-RPC mismatch
//     ("health check failed") because QuicBaaS does not export that RPC
//     even though every other RPC works against the same connection.
func (c *client) buildQRDBClient() (*qrdbsdk.Client, error) {
	// Embedded qrdb: dispatch in-process whenever servicers have been
	// supplied, regardless of the quicbaas transport selection.
	if c.opts.embeddedQRDB != nil {
		return c.buildQRDBEmbeddedClient()
	}
	// TransportEmbedded without qrdb servicers cannot dial — fail fast
	// with a helpful error so callers know to add WithEmbeddedQRDB(...).
	if c.opts.transportType == TransportEmbedded {
		return nil, sdkerrors.NewInvalidInput("buildQRDBClient",
			"TransportEmbedded requires WithEmbeddedQRDB(...) — the qrdb sub-client cannot fall through to a network address without an in-process servicer set")
	}

	t, err := c.buildQRDBTransport()
	if err != nil {
		return nil, err
	}
	// QRDB transports are lazy — they construct without dialing. Call
	// Connect now so the underlying gRPC/QUIC client is ready before
	// any qrdbsdk operation (e.g. Barrier/SealStore) tries to use it.
	// Without this, the first call panics with a nil-pointer dereference
	// inside the transport.
	//
	// qrdb's Connect performs a health check via the QRDBService.Health
	// RPC. QuicBaaS does not export that RPC, so the health check fails
	// even though the connection itself is healthy and other RPCs (like
	// BarrierStatus) work. We swallow that one specific error so the SDK
	// stays usable.
	connectCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if connErr := t.Connect(connectCtx); connErr != nil {
		if !strings.Contains(connErr.Error(), "health check failed") {
			return nil, connErr
		}
	}
	return qrdbsdk.NewClient(t)
}

// buildQRDBTransport constructs the go-qrdb transport that matches the active
// quicbaas transport selection and TLS posture.
//
// The Unix selection has a TLS subtlety: quicbaasd serves its Unix socket
// with the *same* TLS-credentialed gRPC server it uses for TCP (see
// pkg/server/lifecycle.go startUnixSocketListener), so a client must speak
// TLS over the socket. go-qrdb's dedicated unix transport, however, dials
// with insecure credentials by design (Unix sockets give kernel-level
// isolation) — that mismatch produces "error reading server preface: EOF".
//
// To match quicbaasd without weakening the standalone-qrdb unix path, when a
// TLS config is present we route the qrdb unix sub-client through go-qrdb's
// gRPC transport pointed at a "unix://<socket>" target. go-qrdb's gRPC
// transport leaves the unix:// scheme intact for gRPC's unix name resolver
// and dials with TLS credentials whenever a TLSConfig is set — exactly what
// the quicbaas-native unix transport already does. Only when no TLS config is
// available (standalone/dev) do we fall back to go-qrdb's insecure unix
// transport.
func (c *client) buildQRDBTransport() (qrdbtransport.Transport, error) {
	var opts []qrdbtransport.Option
	opts = append(opts, qrdbtransport.WithAddress(c.address))
	if c.opts.tlsEnabled && c.opts.tlsConfig != nil {
		opts = append(opts, qrdbtransport.WithTLSConfig(c.opts.tlsConfig))
	}
	if c.opts.timeout > 0 {
		opts = append(opts, qrdbtransport.WithTimeout(c.opts.timeout))
	}

	switch c.opts.transportType {
	case TransportREST:
		return qrdbrest.New(opts...)
	case TransportQUIC:
		return qrdbquic.New(opts...)
	case TransportUnix:
		return c.buildQRDBUnixTransport()
	case TransportMCP:
		return qrdbmcp.New(opts...)
	default:
		return qrdbgrpc.New(opts...)
	}
}

// buildQRDBUnixTransport builds the qrdb sub-client transport for the Unix
// socket. When a TLS config is configured (the quicbaasd case), it dials the
// socket over go-qrdb's gRPC transport with TLS so it matches quicbaasd's TLS
// Unix listener. Without TLS it uses go-qrdb's native insecure unix transport,
// preserving standalone-qrdb behavior.
func (c *client) buildQRDBUnixTransport() (qrdbtransport.Transport, error) {
	socketPath := c.opts.unixSocketPath
	if socketPath == "" {
		socketPath = c.address
	}

	if c.opts.tlsEnabled && c.opts.tlsConfig != nil {
		opts := []qrdbtransport.Option{
			qrdbtransport.WithAddress(unixGRPCTarget(socketPath)),
			qrdbtransport.WithTLSConfig(c.opts.tlsConfig),
		}
		if c.opts.timeout > 0 {
			opts = append(opts, qrdbtransport.WithTimeout(c.opts.timeout))
		}
		return qrdbgrpc.New(opts...)
	}

	opts := []qrdbtransport.Option{qrdbtransport.WithAddress(socketPath)}
	if c.opts.timeout > 0 {
		opts = append(opts, qrdbtransport.WithTimeout(c.opts.timeout))
	}
	return qrdbunix.New(opts...)
}

// unixGRPCTarget normalizes a Unix socket path into a gRPC dial target the
// go-qrdb gRPC transport accepts. The transport strips http(s):// schemes but
// leaves unix:// intact for gRPC's unix name resolver, so a "unix://<path>"
// target dials the socket directly. Paths that already carry a unix scheme are
// returned unchanged.
func unixGRPCTarget(socketPath string) string {
	if strings.HasPrefix(socketPath, "unix://") || strings.HasPrefix(socketPath, "unix:") {
		return socketPath
	}
	return "unix://" + socketPath
}

// buildQRDBEmbeddedClient builds the qrdb SDK client over the in-process
// embedded transport using the servicers supplied via WithEmbeddedQRDB.
// Returns a typed configuration error when the option has not been set.
//
// Optional servicers (Barrier, Ceremony, Credential, Tenant, etc.) are
// installed via the qrdb embedded transport's functional options when
// non-nil; calls against an absent servicer return a NilServiceError from
// the qrdb embedded transport itself.
func (c *client) buildQRDBEmbeddedClient() (*qrdbsdk.Client, error) {
	if c.opts.embeddedQRDB == nil {
		return nil, sdkerrors.NewInvalidInput("buildQRDBEmbeddedClient",
			"TransportEmbedded requires WithEmbeddedQRDB(...) — pass qrdb servicers to enable in-process dispatch")
	}
	s := c.opts.embeddedQRDB
	if s.KV == nil || s.Admin == nil {
		return nil, sdkerrors.NewInvalidInput("buildQRDBEmbeddedClient",
			"WithEmbeddedQRDB requires non-nil KV and Admin servicers")
	}

	var embOpts []qrdbembedded.Option
	if s.Barrier != nil {
		embOpts = append(embOpts, qrdbembedded.WithBarrierService(s.Barrier))
	}
	if s.Ceremony != nil {
		embOpts = append(embOpts, qrdbembedded.WithCeremonyService(s.Ceremony))
	}
	if s.CeremonyCA != nil {
		embOpts = append(embOpts, qrdbembedded.WithCeremonyCA(s.CeremonyCA))
	}
	if s.Credential != nil {
		embOpts = append(embOpts, qrdbembedded.WithCredentialService(s.Credential))
	}
	if s.Tenant != nil {
		embOpts = append(embOpts, qrdbembedded.WithTenantService(s.Tenant))
	}
	if s.CustodianGroup != nil {
		embOpts = append(embOpts, qrdbembedded.WithCustodianGroupService(s.CustodianGroup))
	}
	if s.Share != nil {
		embOpts = append(embOpts, qrdbembedded.WithShareService(s.Share))
	}
	if s.AuthFactor != nil {
		embOpts = append(embOpts, qrdbembedded.WithAuthFactorService(s.AuthFactor))
	}
	if s.UserAuthFactor != nil {
		embOpts = append(embOpts, qrdbembedded.WithUserAuthFactorService(s.UserAuthFactor))
	}
	if s.User != nil {
		embOpts = append(embOpts, qrdbembedded.WithUserService(s.User))
	}

	t, err := qrdbembedded.New(s.KV, s.Admin, embOpts...)
	if err != nil {
		return nil, err
	}
	// Connect is a no-op for the qrdb embedded transport; skip it. The
	// network-transport Connect-with-health-check workaround does not
	// apply here because the embedded transport never dials anything.
	return qrdbsdk.NewClient(t)
}

// DataStore returns a KVStore suitable for constructing DAOs.
// Uses the QRDB transport layer.
func (c *client) DataStore() (QRDBKVStore, error) {
	qrdb, err := c.qrdbClient()
	if err != nil {
		return nil, err
	}
	return qrdb.KVStore(), nil
}
