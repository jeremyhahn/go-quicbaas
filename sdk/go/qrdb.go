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
	"io"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc/metadata"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	qrdbembedded "github.com/jeremyhahn/go-qrdb/sdk/go/transport/embedded"
	qrdbgrpc "github.com/jeremyhahn/go-qrdb/sdk/go/transport/grpc"
	qrdbmcp "github.com/jeremyhahn/go-qrdb/sdk/go/transport/mcp"
	qrdbrest "github.com/jeremyhahn/go-qrdb/sdk/go/transport/rest"
	qrdbunix "github.com/jeremyhahn/go-qrdb/sdk/go/transport/unix"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	qbtransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
	qbmcp "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/rest"
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

// QRDBUser is a re-export of the qrdb User identity model.
type QRDBUser = qrdbtransport.User

// ComputeSPKIThumbprint is a re-export of qrdb's canonical mTLS identity
// algorithm (SHA-256 over SubjectPublicKeyInfo, base64url without padding).
// Consumers must call it rather than reimplement it.
var ComputeSPKIThumbprint = qrdbsdk.ComputeSPKIThumbprint

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

// QRDBEmbeddingClient is a re-export of go-qrdb's EmbeddingClient interface:
// go-qrdb's embedding service (Embed, BatchEmbed, ListEmbeddingModels).
// Reach it through AI().QRDBEmbedding().
type QRDBEmbeddingClient = qrdbtransport.EmbeddingClient

// QRDBEmbedRequest is a re-export of the qrdb EmbedRequest type.
type QRDBEmbedRequest = qrdbtransport.EmbedRequest

// QRDBEmbedResponse is a re-export of the qrdb EmbedResponse type.
type QRDBEmbedResponse = qrdbtransport.EmbedResponse

// QRDBBatchEmbedRequest is a re-export of the qrdb BatchEmbedRequest type.
type QRDBBatchEmbedRequest = qrdbtransport.BatchEmbedRequest

// QRDBBatchEmbedResponse is a re-export of the qrdb BatchEmbedResponse type.
type QRDBBatchEmbedResponse = qrdbtransport.BatchEmbedResponse

// QRDBListEmbeddingModelsResponse is a re-export of the qrdb
// ListEmbeddingModelsResponse type.
type QRDBListEmbeddingModelsResponse = qrdbtransport.ListEmbeddingModelsResponse

// QRDBEmbeddingModelInfo is a re-export of the qrdb EmbeddingModelInfo type.
type QRDBEmbeddingModelInfo = qrdbtransport.EmbeddingModelInfo

// QRDBIndexClient is a re-export of the qrdb IndexClient interface.
type QRDBIndexClient = qrdbtransport.IndexClient

// QRDBDataService re-exports go-qrdb's data services: KV, batch,
// transaction, MVCC, collection and index. Each accessor returns the go-qrdb
// client itself (by alias), carried on the transport the QuicBaaS client was
// built with; go-quicbaas wraps none of them. The go-qrdb SDK client
// (*QRDBClient) satisfies it.
type QRDBDataService interface {
	KV() QRDBKVClient
	Batch() QRDBBatchClient
	Transaction() QRDBTransactionClient
	MVCC() QRDBMVCCClient
	Collections() QRDBCollectionClient
	Index() QRDBIndexClient
}

var _ QRDBDataService = (*QRDBClient)(nil)

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

// The rest of the go-qrdb TenantClient surface (#1656): the request and
// response types of every TenantClient method, re-exported so an sdk/go
// caller can drive tenants and per-tenant barriers without importing go-qrdb.
type (
	QRDBUpdateTenantRequest              = qrdbtransport.UpdateTenantRequest
	QRDBUpdateTenantResponse             = qrdbtransport.UpdateTenantResponse
	QRDBUpdateTenantQuotasRequest        = qrdbtransport.UpdateTenantQuotasRequest
	QRDBUpdateTenantQuotasResponse       = qrdbtransport.UpdateTenantQuotasResponse
	QRDBUpdateTenantEntitlementsRequest  = qrdbtransport.UpdateTenantEntitlementsRequest
	QRDBUpdateTenantEntitlementsResponse = qrdbtransport.UpdateTenantEntitlementsResponse
	QRDBTenantQuotas                     = qrdbtransport.TenantQuotas
	QRDBTenantEntitlements               = qrdbtransport.TenantEntitlements
	QRDBListDeletedTenantsRequest        = qrdbtransport.ListDeletedTenantsRequest
	QRDBListDeletedTenantsResponse       = qrdbtransport.ListDeletedTenantsResponse
	QRDBDeletedTenant                    = qrdbtransport.DeletedTenant
	QRDBReconcileDeletedTenantsResponse  = qrdbtransport.ReconcileDeletedTenantsResponse
	QRDBTenantOfficerRegisterRequest     = qrdbtransport.TenantOfficerRegisterRequest
	QRDBTenantOfficerClaimStatusRequest  = qrdbtransport.TenantOfficerClaimStatusRequest
	QRDBTenantShareClaimBeginRequest     = qrdbtransport.TenantShareClaimBeginRequest
	QRDBTenantShareClaimCompleteRequest  = qrdbtransport.TenantShareClaimCompleteRequest
	QRDBTenantShamirSealVoteRequest      = qrdbtransport.TenantShamirSealVoteRequest
	QRDBTenantDatabaseAccessDeniedError  = qrdbtransport.TenantDatabaseAccessDeniedError
)

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

// QRDBBeginClaimShareRequest is a re-export of the qrdb
// BeginClaimShareRequest type.
type QRDBBeginClaimShareRequest = qrdbtransport.BeginClaimShareRequest

// QRDBBeginClaimShareResponse is a re-export of the qrdb
// BeginClaimShareResponse type.
type QRDBBeginClaimShareResponse = qrdbtransport.BeginClaimShareResponse

// QRDBCompleteClaimShareRequest is a re-export of the qrdb
// CompleteClaimShareRequest type.
type QRDBCompleteClaimShareRequest = qrdbtransport.CompleteClaimShareRequest

// QRDBCompleteClaimShareResponse is a re-export of the qrdb
// CompleteClaimShareResponse type.
type QRDBCompleteClaimShareResponse = qrdbtransport.CompleteClaimShareResponse

// QRDBSignCSRInitRequest is a re-export of the qrdb SignCSRInitRequest type.
type QRDBSignCSRInitRequest = qrdbtransport.SignCSRInitRequest

// QRDBSignCSRInitResponse is a re-export of the qrdb SignCSRInitResponse type.
type QRDBSignCSRInitResponse = qrdbtransport.SignCSRInitResponse

// =============================================================================
// QRDB KVStore Re-exports
// =============================================================================

// QRDBKVStore is a re-export of the qrdb KVStore interface.
type QRDBKVStore = qrdbsdk.KVStore

// =============================================================================
// QRDB DAO Re-exports
// =============================================================================

// QRDBEntity is a re-export of the qrdb Entity interface.
type QRDBEntity = qrdbsdk.Entity

// QRDBGenericDAO is a re-export of the qrdb GenericDAO interface.
type QRDBGenericDAO[E qrdbsdk.Entity] = qrdbsdk.GenericDAO[E]

// QRDBDAOOption is a re-export of the qrdb DAO Option type.
type QRDBDAOOption = qrdbsdk.DAOOption

// QRDBPageQuery is a re-export of the qrdb PageQuery type.
type QRDBPageQuery = qrdbsdk.PageQuery

// QRDBPageResult is a re-export of the qrdb PageResult type.
type QRDBPageResult[E any] = qrdbsdk.PageResult[E]

// NewDAO creates a new GenericDAO backed by the given KVStore.
func NewDAO[E qrdbsdk.Entity](store QRDBKVStore, entityType string, newEntity func() E, opts ...QRDBDAOOption) (QRDBGenericDAO[E], error) {
	return qrdbsdk.NewDAO(store, entityType, newEntity, opts...)
}

// IsDAONotFound returns true if the error indicates an entity was not found.
var IsDAONotFound = qrdbsdk.IsDAONotFound

// =============================================================================
// Database Error Re-exports
// =============================================================================
//
// DatabaseError replaces the legacy QBError name from go-qrdb, providing
// a project-neutral error type for database operations.

// DatabaseError is a structured database error with error code, operation
// context, and optional retry semantics.
type DatabaseError = qrdbsdk.DatabaseError

// DatabaseErrorCode represents a database error classification.
type DatabaseErrorCode = qrdbsdk.ErrorCode

// Database error code constants.
const (
	ErrNotFound    = qrdbsdk.ErrCodeNotFound
	ErrInternal    = qrdbsdk.ErrCodeInternal
	ErrConflict    = qrdbsdk.ErrCodeConflict
	ErrTimeout     = qrdbsdk.ErrCodeTimeout
	ErrUnavailable = qrdbsdk.ErrCodeUnavailable
)

// NewDatabaseError creates a new DatabaseError with the given code and operation.
func NewDatabaseError(code DatabaseErrorCode, op string, message string) *DatabaseError {
	return qrdbsdk.NewError(code, op, message)
}

// IsDatabaseError extracts a DatabaseError from the error chain if present.
func IsDatabaseError(err error, target **DatabaseError) bool {
	return qrdbsdk.AsError(err, target)
}

// IsDatabaseErrorCode checks if the error matches the given database error code.
func IsDatabaseErrorCode(err error, code DatabaseErrorCode) bool {
	return qrdbsdk.IsErrorCode(err, code)
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

// closeQRDB closes the qrdb sub-client this client built, and makes any later
// qrdbClient call fail, so Close and a first QRDBData call cannot race into a
// client that is never closed. The embedded sub-client's transport only marks
// itself closed; the servicers and node the caller passed in stay open.
func (c *client) closeQRDB() error {
	c.qrdb.once.Do(func() {
		c.qrdb.err = sdkerrors.NewUnavailable("qrdbClient", "the client is closed")
	})
	if c.qrdb.client == nil {
		return nil
	}
	return c.qrdb.client.Close()
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
//
// The REST and MCP selections have a URL subtlety. Every other qrdb transport
// dials a bare "host:port", but go-qrdb's REST transport concatenates its
// request path straight onto the configured address and its MCP transport
// hands the address to a Streamable HTTP client — both need a
// scheme-qualified URL. Passing the quicbaas address through verbatim made
// every qrdb REST call die in url.Parse before a request was sent
// ("first path segment in URL cannot contain colon", go-quicbaas#405), and
// left MCP dialing an unparseable endpoint. Both selections normalize through
// the same helpers the quicbaas-native REST/MCP transports use, so one
// address spelling works across every transport.
func (c *client) buildQRDBTransport() (qrdbtransport.Transport, error) {
	tlsConfigured := c.opts.tlsEnabled && c.opts.tlsConfig != nil

	buildOpts := func(address string) []qrdbtransport.Option {
		opts := []qrdbtransport.Option{qrdbtransport.WithAddress(address)}
		if tlsConfigured {
			// A clone: go-qrdb's HTTP/2 transports let net/http add "h2"
			// to the config they hold, and the caller's config must not
			// change under the SDK's other transports (go-quicbaas#1734).
			opts = append(opts, qrdbtransport.WithTLSConfig(c.opts.tlsConfig.Clone()))
		}
		if c.opts.timeout > 0 {
			opts = append(opts, qrdbtransport.WithTimeout(c.opts.timeout))
		}
		return opts
	}

	address := qrdbSubClientAddress(c.opts.transportType, c.address, tlsConfigured)

	switch c.opts.transportType {
	case TransportREST, TransportQUIC:
		// quicbaas's QUIC transport is its REST transport over HTTP/3 on
		// the REST port, and the node serves no native qrdb-quic listener:
		// go-qrdb's QUIC transport offers wire.ProtocolV2 and is refused
		// ("tls: no application protocol", go-quicbaas#2533). The QUIC
		// selection reaches the same qrdb REST API at the same origin.
		// Barrier verbs go over HTTP/3 through the SDK transport
		// (Security().Barrier()); the other QRDB verb groups still use
		// go-qrdb's REST transport over TCP until go-qrdb's REST transport
		// accepts an HTTP/3 client (#2533).
		return qrdbrest.New(buildOpts(address)...)
	case TransportUnix:
		return c.buildQRDBUnixTransport()
	case TransportMCP:
		return qrdbmcp.New(buildOpts(address)...)
	default:
		return qrdbgrpc.New(buildOpts(address)...)
	}
}

// qrdbSubClientAddress returns the address the go-qrdb sub-client transport
// dials for the active quicbaas transport selection.
//
// gRPC dials a bare "host:port" and must not be handed a scheme. REST and
// QUIC need a scheme-qualified origin because both use go-qrdb's REST
// transport, which concatenates its request path onto the address: a
// quicbaasd node serves REST and HTTP/3 on its QUIC port and never offers
// go-qrdb's frame-protocol ALPN (go-quicbaas#2554). MCP needs the full
// Streamable HTTP endpoint URL, path included. Unix is resolved separately by
// buildQRDBUnixTransport, which chooses between the gRPC and unix transports
// on TLS posture, so it keeps the socket path verbatim here.
//
// QUIC always uses TLS, even for an explicit http:// address: the leading
// http:// is stripped so the origin is always https://, and a secret-bearing
// call such as BarrierUnseal never goes out as plaintext HTTP.
func qrdbSubClientAddress(transportType TransportType, address string, tlsConfigured bool) string {
	switch transportType {
	case TransportREST:
		return qbtransport.HTTPBaseURL(address, tlsConfigured)
	case TransportQUIC:
		if len(address) >= len("http://") && strings.EqualFold(address[:len("http://")], "http://") {
			address = address[len("http://"):]
		}
		return qbtransport.HTTPBaseURL(address, true)
	case TransportMCP:
		return qbtransport.HTTPEndpoint(address, tlsConfigured, qbmcp.MCPEndpointPath)
	default:
		return address
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
			qrdbtransport.WithTLSConfig(c.opts.tlsConfig.Clone()),
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
	if s.Embedding != nil {
		embOpts = append(embOpts, qrdbembedded.WithEmbeddingService(s.Embedding))
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

// qrdbEmbedding returns go-qrdb's embedding service client. Over REST and
// QUIC the quicbaas transport carries the call (QUIC on its HTTP/3 client), so
// the platform bearer is sent; on the other transports it comes from the qrdb sub-client. Over gRPC and Unix the
// configured bearer rides each call as authorization metadata; with no token
// set, go-qrdb's own client is returned. It errors when the qrdb client cannot
// be built, or when the active qrdb transport has no embedding surface.
func (c *client) qrdbEmbedding() (QRDBEmbeddingClient, error) {
	if rt, ok := c.transport.(interface {
		QRDBEmbedding() *rest.QRDBEmbeddingClient
	}); ok && (c.opts == nil || (c.opts.embeddedQRDB == nil && (c.opts.transportType == TransportREST || c.opts.transportType == TransportQUIC))) {
		return rt.QRDBEmbedding(), nil
	}
	if mt, ok := c.transport.(*qbmcp.Transport); ok && (c.opts == nil || (c.opts.embeddedQRDB == nil && c.opts.transportType == TransportMCP)) {
		return mt.QRDBEmbedding(), nil
	}
	qrdb, err := c.qrdbClient()
	if err != nil {
		return nil, err
	}
	if qrdb == nil {
		return nil, sdkerrors.NewUnavailable("qrdbEmbedding", "the qrdb client is not configured")
	}
	ec := qrdb.Embedding()
	if ec == nil {
		return nil, sdkerrors.NewUnavailable("qrdbEmbedding", "the qrdb transport does not serve the embedding service")
	}
	if c.opts != nil && c.opts.embeddedQRDB == nil && c.opts.authToken != "" && qrdbSubClientSpeaksGRPC(c.opts.transportType) {
		return &qrdbBearerEmbeddingClient{inner: ec, bearer: "Bearer " + c.opts.authToken}, nil
	}
	return ec, nil
}

// QRDBData returns go-qrdb's data services through a go-qrdb SDK client.
//
// Embedded: when the client carries QRDBEmbeddedServicers.Data (the embedded
// node's own client, whose transport carries the node's service registry) it
// serves the calls, so every data service reaches the node; otherwise this
// client's own go-qrdb client does. The node's KV, batch, transaction, MVCC
// and index services on the default database are the system tenant's: they
// do not resolve the context's tenant, so a call whose context binds another
// tenant (go-qrdb's ContextWithTenant, this SDK's transport.ContextWithTenant,
// or the tenant of the authenticated identity go-qrdb's WithIdentity carries)
// is refused rather than served from the system keyspace, as is a context
// naming two different tenants. Collections resolve (tenant, database)
// through the registry and pass through, with a tenant bound by
// transport.ContextWithTenant or carried by the authenticated identity bound
// as go-qrdb's binding.
//
// gRPC and Unix: a client configured with WithAuthToken sends the token as
// "authorization: Bearer <token>" metadata on every data call. REST, QUIC
// and MCP: go-qrdb's transports carry no bearer, so a client with an auth
// token gets an Unimplemented error rather than anonymous data calls. MCP: a
// client configured with WithMCPAuthToken or WithMCPCredentials gets the same
// Unimplemented error, since go-qrdb's MCP client carries neither credential.
//
// The qrdb connection this opens is released by the parent client's Close;
// after Close, QRDBData returns an error. A caller-supplied node client
// (QRDBEmbeddedServicers.Data) stays the caller's and is not closed.
func (c *client) QRDBData() (QRDBDataService, error) {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return nil, sdkerrors.NewUnavailable("QRDBData", "the client is closed")
	}
	if s := c.opts.embeddedQRDB; s != nil {
		data := s.Data
		if data == nil {
			qrdb, err := c.qrdbClient()
			if err != nil {
				return nil, err
			}
			data = qrdb
		}
		return &preparedQRDBData{inner: data, prepare: refuseTenantBoundQRDBCall, prepareCollections: scopeQRDBCollectionsTenant}, nil
	}
	if c.opts.authToken != "" {
		switch c.opts.transportType {
		case TransportREST, TransportQUIC, TransportMCP:
			return nil, sdkerrors.NewUnimplemented("QRDBData",
				"go-qrdb's REST, QUIC and MCP transports cannot carry the client's bearer token; use the gRPC transport for authenticated data calls")
		}
	}
	if c.opts.transportType == TransportMCP &&
		(c.opts.mcpAuthToken != "" || c.opts.mcpUsername != "" || c.opts.mcpPassword != "") {
		return nil, sdkerrors.NewUnimplemented("QRDBData",
			"go-qrdb's MCP transport cannot carry the client's MCP credentials; use the gRPC transport for authenticated data calls")
	}
	qrdb, err := c.qrdbClient()
	if err != nil {
		return nil, err
	}
	if c.opts.authToken == "" {
		return qrdb, nil
	}
	bearer := withQRDBBearer(c.opts.authToken)
	return &preparedQRDBData{inner: qrdb, prepare: bearer, prepareCollections: bearer}, nil
}

// qrdbCallTenant reads the tenant a data call's context binds, through
// go-qrdb's ContextWithTenant, this SDK's transport.ContextWithTenant, or the
// tenant of the authenticated identity go-qrdb's WithIdentity carries (the
// same sources pkg/tenantctx.FromContext reads on the server). bound is false
// when none is set. Two sources that name different tenants are refused as
// Forbidden rather than one silently winning; otherwise the first set source
// wins, in the order go-qrdb binding, SDK binding, identity. An authenticated
// identity with no tenant and no explicit binding is refused as Forbidden;
// only a context with no identity is the host process.
func qrdbCallTenant(ctx context.Context) (id string, bound bool, err error) {
	qrdbID, qrdbBound := qrdbsdk.TenantFromContext(ctx)
	if !qrdbBound {
		qrdbID = ""
	}
	sdkID := qbtransport.TenantFromContext(ctx)
	identityID := ""
	if identity := qrdbsdk.IdentityFromContext(ctx); identity != nil {
		identityID = identity.TenantID
	}
	sources := [...]struct{ id, from string }{
		{qrdbID, "go-qrdb binding"},
		{sdkID, "QuicBaaS SDK binding"},
		{identityID, "authenticated identity"},
	}
	first := -1
	for i, s := range sources {
		if s.id == "" {
			continue
		}
		if first < 0 {
			first = i
			continue
		}
		if s.id != sources[first].id {
			return "", false, sdkerrors.NewForbidden("QRDBData",
				"the call's context binds conflicting tenants "+sources[first].id+" ("+sources[first].from+") and "+s.id+" ("+s.from+")")
		}
	}
	if first < 0 {
		if qrdbsdk.IdentityFromContext(ctx) != nil {
			return "", false, sdkerrors.NewForbidden("QRDBData",
				"the call's authenticated identity carries no tenant")
		}
		return "", false, nil
	}
	return sources[first].id, true, nil
}

// scopeQRDBCollectionsTenant binds the tenant qrdbCallTenant resolves (from
// this SDK's transport.ContextWithTenant or the authenticated identity, when
// go-qrdb's ContextWithTenant is not already set) into go-qrdb's
// ContextWithTenant, so the embedded node's collection registry resolves the
// caller's tenant rather than the system tenant. A context binding
// conflicting tenants is refused.
func scopeQRDBCollectionsTenant(ctx context.Context) (context.Context, error) {
	id, bound, err := qrdbCallTenant(ctx)
	if err != nil {
		return nil, err
	}
	if !bound {
		return ctx, nil
	}
	if _, ok := qrdbsdk.TenantFromContext(ctx); ok {
		return ctx, nil
	}
	return qrdbsdk.ContextWithTenant(ctx, id), nil
}

// refuseTenantBoundQRDBCall refuses a data call whose context binds a tenant
// other than the system tenant, through go-qrdb's ContextWithTenant, this
// SDK's transport.ContextWithTenant or the authenticated identity's tenant
// (go-qrdb's WithIdentity): the embedded node's default-database KV,
// batch, transaction, MVCC and index services serve the system keyspace
// whatever the context names, so serving the call would let one tenant read
// and write another's (and the system's) data. Conflicting bindings are
// refused too.
func refuseTenantBoundQRDBCall(ctx context.Context) (context.Context, error) {
	id, bound, err := qrdbCallTenant(ctx)
	if err != nil {
		return nil, err
	}
	if bound && id != qrdbsdk.SystemTenantID {
		return nil, sdkerrors.NewForbidden("QRDBData",
			"the embedded node's default-database data services serve the system tenant only; refusing a call bound to tenant "+id)
	}
	return ctx, nil
}

// withQRDBBearer returns a prepare hook that adds the client's bearer token
// to a call's outgoing gRPC metadata, the per-call seam go-qrdb's gRPC
// transport forwards.
func withQRDBBearer(token string) func(context.Context) (context.Context, error) {
	return func(ctx context.Context) (context.Context, error) {
		return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token), nil
	}
}

// preparedQRDBData runs prepare before every KV, batch, transaction, MVCC and
// index call, and prepareCollections (when set) before every collection call.
// A prepare error is returned in place of the call.
type preparedQRDBData struct {
	inner              QRDBDataService
	prepare            func(context.Context) (context.Context, error)
	prepareCollections func(context.Context) (context.Context, error)
}

var _ QRDBDataService = (*preparedQRDBData)(nil)

func (d *preparedQRDBData) KV() QRDBKVClient {
	inner := d.inner.KV()
	kv := preparedKV{inner: inner, prepare: d.prepare}
	if g, ok := inner.(qrdbtransport.GuardedKVClient); ok {
		return preparedGuardedKV{preparedKV: kv, guarded: g}
	}
	return kv
}

func (d *preparedQRDBData) Batch() QRDBBatchClient {
	return preparedBatch{inner: d.inner.Batch(), prepare: d.prepare}
}

func (d *preparedQRDBData) Transaction() QRDBTransactionClient {
	return preparedTransaction{inner: d.inner.Transaction(), prepare: d.prepare}
}

func (d *preparedQRDBData) MVCC() QRDBMVCCClient {
	return preparedMVCC{inner: d.inner.MVCC(), prepare: d.prepare}
}

func (d *preparedQRDBData) Collections() QRDBCollectionClient {
	if d.prepareCollections == nil {
		return d.inner.Collections()
	}
	return preparedCollections{inner: d.inner.Collections(), prepare: d.prepareCollections}
}

func (d *preparedQRDBData) Index() QRDBIndexClient {
	return preparedIndex{inner: d.inner.Index(), prepare: d.prepare}
}

type preparedKV struct {
	inner   QRDBKVClient
	prepare func(context.Context) (context.Context, error)
}

func (k preparedKV) Put(ctx context.Context, key string, value []byte, opts ...qrdbtransport.WriteOption) error {
	ctx, err := k.prepare(ctx)
	if err != nil {
		return err
	}
	return k.inner.Put(ctx, key, value, opts...)
}

func (k preparedKV) Get(ctx context.Context, key string, opts ...qrdbtransport.ReadOption) ([]byte, error) {
	ctx, err := k.prepare(ctx)
	if err != nil {
		return nil, err
	}
	return k.inner.Get(ctx, key, opts...)
}

func (k preparedKV) Delete(ctx context.Context, key string, opts ...qrdbtransport.WriteOption) error {
	ctx, err := k.prepare(ctx)
	if err != nil {
		return err
	}
	return k.inner.Delete(ctx, key, opts...)
}

func (k preparedKV) List(ctx context.Context, prefix string, opts ...qrdbtransport.ReadOption) ([]string, error) {
	ctx, err := k.prepare(ctx)
	if err != nil {
		return nil, err
	}
	return k.inner.List(ctx, prefix, opts...)
}

func (k preparedKV) Scan(ctx context.Context, prefix string, opts ...qrdbtransport.ReadOption) (map[string][]byte, error) {
	ctx, err := k.prepare(ctx)
	if err != nil {
		return nil, err
	}
	return k.inner.Scan(ctx, prefix, opts...)
}

func (k preparedKV) Exists(ctx context.Context, key string, opts ...qrdbtransport.ReadOption) (bool, error) {
	ctx, err := k.prepare(ctx)
	if err != nil {
		return false, err
	}
	return k.inner.Exists(ctx, key, opts...)
}

// preparedGuardedKV is preparedKV over an inner client that has go-qrdb's
// guarded writes: it keeps PutIfAbsent, CompareAndDelete and CompareAndSwap,
// each prepared like the plain KV calls. QRDBData's KV returns it only when
// the inner client is a GuardedKVClient, so callers that detect the
// capability by type assertion see exactly what the inner client offers.
type preparedGuardedKV struct {
	preparedKV
	guarded qrdbtransport.GuardedKVClient
}

func (k preparedGuardedKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	ctx, err := k.prepare(ctx)
	if err != nil {
		return false, err
	}
	return k.guarded.PutIfAbsent(ctx, key, value)
}

func (k preparedGuardedKV) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	ctx, err := k.prepare(ctx)
	if err != nil {
		return false, err
	}
	return k.guarded.CompareAndDelete(ctx, key, expected)
}

func (k preparedGuardedKV) CompareAndSwap(ctx context.Context, key string, expected, next []byte) (bool, error) {
	ctx, err := k.prepare(ctx)
	if err != nil {
		return false, err
	}
	return k.guarded.CompareAndSwap(ctx, key, expected, next)
}

type preparedBatch struct {
	inner   QRDBBatchClient
	prepare func(context.Context) (context.Context, error)
}

func (b preparedBatch) ExecuteBatch(ctx context.Context, ops []qrdbtransport.Op, opts ...qrdbtransport.WriteOption) (*qrdbtransport.BatchResponse, error) {
	ctx, err := b.prepare(ctx)
	if err != nil {
		return nil, err
	}
	return b.inner.ExecuteBatch(ctx, ops, opts...)
}

func (b preparedBatch) ImportFile(ctx context.Context, filename string, data io.Reader, opts qrdbtransport.ImportOptions) (*qrdbtransport.ImportResult, error) {
	ctx, err := b.prepare(ctx)
	if err != nil {
		return nil, err
	}
	return b.inner.ImportFile(ctx, filename, data, opts)
}

type preparedTransaction struct {
	inner   QRDBTransactionClient
	prepare func(context.Context) (context.Context, error)
}

func (t preparedTransaction) ExecuteTransaction(ctx context.Context, req *qrdbtransport.TransactionRequest, opts ...qrdbtransport.WriteOption) (*qrdbtransport.TransactionResponse, error) {
	ctx, err := t.prepare(ctx)
	if err != nil {
		return nil, err
	}
	return t.inner.ExecuteTransaction(ctx, req, opts...)
}

type preparedMVCC struct {
	inner   QRDBMVCCClient
	prepare func(context.Context) (context.Context, error)
}

func (m preparedMVCC) PutWithLease(ctx context.Context, key string, value []byte, leaseID int64, opts ...qrdbtransport.WriteOption) (*qrdbtransport.MVCCKeyValue, error) {
	ctx, err := m.prepare(ctx)
	if err != nil {
		return nil, err
	}
	return m.inner.PutWithLease(ctx, key, value, leaseID, opts...)
}

func (m preparedMVCC) GetRevision(ctx context.Context, key string, opts ...qrdbtransport.ReadOption) (*qrdbtransport.MVCCKeyValue, error) {
	ctx, err := m.prepare(ctx)
	if err != nil {
		return nil, err
	}
	return m.inner.GetRevision(ctx, key, opts...)
}

func (m preparedMVCC) GetAtRevision(ctx context.Context, key string, revision int64, opts ...qrdbtransport.ReadOption) (*qrdbtransport.MVCCKeyValue, error) {
	ctx, err := m.prepare(ctx)
	if err != nil {
		return nil, err
	}
	return m.inner.GetAtRevision(ctx, key, revision, opts...)
}

func (m preparedMVCC) Range(ctx context.Context, start, end []byte, rangeOpts qrdbtransport.RangeOptions, opts ...qrdbtransport.ReadOption) (*qrdbtransport.RangeResponse, error) {
	ctx, err := m.prepare(ctx)
	if err != nil {
		return nil, err
	}
	return m.inner.Range(ctx, start, end, rangeOpts, opts...)
}

func (m preparedMVCC) Compact(ctx context.Context, revision int64) error {
	ctx, err := m.prepare(ctx)
	if err != nil {
		return err
	}
	return m.inner.Compact(ctx, revision)
}

func (m preparedMVCC) CurrentRevision(ctx context.Context) (int64, error) {
	ctx, err := m.prepare(ctx)
	if err != nil {
		return 0, err
	}
	return m.inner.CurrentRevision(ctx)
}

func (m preparedMVCC) RestoreToRevision(ctx context.Context, req *qrdbtransport.RestoreRevisionRequest) (*qrdbtransport.RestoreRevisionResult, error) {
	ctx, err := m.prepare(ctx)
	if err != nil {
		return nil, err
	}
	return m.inner.RestoreToRevision(ctx, req)
}

type preparedIndex struct {
	inner   QRDBIndexClient
	prepare func(context.Context) (context.Context, error)
}

func (x preparedIndex) IndexCreate(ctx context.Context, name, fieldName string, config qrdbtransport.IndexConfig) error {
	ctx, err := x.prepare(ctx)
	if err != nil {
		return err
	}
	return x.inner.IndexCreate(ctx, name, fieldName, config)
}

func (x preparedIndex) IndexDrop(ctx context.Context, name string) error {
	ctx, err := x.prepare(ctx)
	if err != nil {
		return err
	}
	return x.inner.IndexDrop(ctx, name)
}

func (x preparedIndex) IndexQuery(ctx context.Context, indexName, value string) (*qrdbtransport.IndexQueryResponse, error) {
	ctx, err := x.prepare(ctx)
	if err != nil {
		return nil, err
	}
	return x.inner.IndexQuery(ctx, indexName, value)
}

func (x preparedIndex) IndexScan(ctx context.Context, indexName string, start, end *string) (*qrdbtransport.IndexScanResponse, error) {
	ctx, err := x.prepare(ctx)
	if err != nil {
		return nil, err
	}
	return x.inner.IndexScan(ctx, indexName, start, end)
}

func (x preparedIndex) IndexList(ctx context.Context) (*qrdbtransport.IndexListResponse, error) {
	ctx, err := x.prepare(ctx)
	if err != nil {
		return nil, err
	}
	return x.inner.IndexList(ctx)
}

type preparedCollections struct {
	inner   QRDBCollectionClient
	prepare func(context.Context) (context.Context, error)
}

func (p preparedCollections) CreateCollection(ctx context.Context, req qrdbtransport.CreateCollectionRequest) (*qrdbtransport.CollectionInfo, error) {
	ctx, err := p.prepare(ctx)
	if err != nil {
		return nil, err
	}
	return p.inner.CreateCollection(ctx, req)
}

func (p preparedCollections) GetCollectionInfo(ctx context.Context, database, name string) (*qrdbtransport.CollectionInfo, error) {
	ctx, err := p.prepare(ctx)
	if err != nil {
		return nil, err
	}
	return p.inner.GetCollectionInfo(ctx, database, name)
}

func (p preparedCollections) ListCollections(ctx context.Context, database string) (*qrdbtransport.ListCollectionsResponse, error) {
	ctx, err := p.prepare(ctx)
	if err != nil {
		return nil, err
	}
	return p.inner.ListCollections(ctx, database)
}

func (p preparedCollections) DropCollection(ctx context.Context, database, name string) error {
	ctx, err := p.prepare(ctx)
	if err != nil {
		return err
	}
	return p.inner.DropCollection(ctx, database, name)
}

func (p preparedCollections) InsertDocument(ctx context.Context, req qrdbtransport.InsertDocumentRequest) (*qrdbtransport.InsertDocumentResponse, error) {
	ctx, err := p.prepare(ctx)
	if err != nil {
		return nil, err
	}
	return p.inner.InsertDocument(ctx, req)
}

func (p preparedCollections) GetDocument(ctx context.Context, database, collection string, id uint64) (*qrdbtransport.DocumentResponse, error) {
	ctx, err := p.prepare(ctx)
	if err != nil {
		return nil, err
	}
	return p.inner.GetDocument(ctx, database, collection, id)
}

func (p preparedCollections) UpdateDocument(ctx context.Context, req qrdbtransport.UpdateDocumentRequest) error {
	ctx, err := p.prepare(ctx)
	if err != nil {
		return err
	}
	return p.inner.UpdateDocument(ctx, req)
}

func (p preparedCollections) DeleteDocument(ctx context.Context, database, collection string, id uint64) error {
	ctx, err := p.prepare(ctx)
	if err != nil {
		return err
	}
	return p.inner.DeleteDocument(ctx, database, collection, id)
}

func (p preparedCollections) ListDocuments(ctx context.Context, req qrdbtransport.ListDocumentsRequest) (*qrdbtransport.ListDocumentsResponse, error) {
	ctx, err := p.prepare(ctx)
	if err != nil {
		return nil, err
	}
	return p.inner.ListDocuments(ctx, req)
}
