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
	qbtransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
	qbmcp "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
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

// =============================================================================
// QRDB Share Request/Response Re-exports
// =============================================================================

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

// QRDBBarrierStatusResponse is a re-export of the qrdb BarrierStatusResponse type.
type QRDBBarrierStatusResponse = qrdbtransport.BarrierStatusResponse

// QRDBBarrierGenerateRecoveryKeysRequest is a re-export of the qrdb BarrierGenerateRecoveryKeysRequest type.
type QRDBBarrierGenerateRecoveryKeysRequest = qrdbtransport.BarrierGenerateRecoveryKeysRequest

// QRDBBarrierRecoveryKeysResponse is a re-export of the qrdb BarrierRecoveryKeysResponse type.
type QRDBBarrierRecoveryKeysResponse = qrdbtransport.BarrierRecoveryKeysResponse

// QRDBBarrierHasRecoveryKeysResponse is a re-export of the qrdb BarrierHasRecoveryKeysResponse type.
type QRDBBarrierHasRecoveryKeysResponse = qrdbtransport.BarrierHasRecoveryKeysResponse

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
	ErrNotFound    = qrdbsdk.ErrNotFound
	ErrInternal    = qrdbsdk.ErrInternal
	ErrConflict    = qrdbsdk.ErrConflict
	ErrTimeout     = qrdbsdk.ErrTimeout
	ErrUnavailable = qrdbsdk.ErrUnavailable
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
	case TransportREST:
		return qrdbrest.New(buildOpts(address)...)
	case TransportQUIC:
		return qrdbquic.New(buildOpts(address)...)
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
// gRPC and QUIC dial a bare "host:port" and must not be handed a scheme.
// REST needs a scheme-qualified origin because go-qrdb's REST transport
// concatenates its request path onto the address; MCP needs the full
// Streamable HTTP endpoint URL, path included. Unix is resolved separately by
// buildQRDBUnixTransport, which chooses between the gRPC and unix transports
// on TLS posture, so it keeps the socket path verbatim here.
func qrdbSubClientAddress(transportType TransportType, address string, tlsConfigured bool) string {
	switch transportType {
	case TransportREST:
		return qbtransport.HTTPBaseURL(address, tlsConfigured)
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
	if s.AuthFactor != nil {
		embOpts = append(embOpts, qrdbembedded.WithAuthFactorService(s.AuthFactor))
	}
	if s.UserAuthFactor != nil {
		embOpts = append(embOpts, qrdbembedded.WithUserAuthFactorService(s.UserAuthFactor))
	}
	if s.User != nil {
		embOpts = append(embOpts, qrdbembedded.WithUserService(s.User))
	}
	if s.Index != nil {
		embOpts = append(embOpts, qrdbembedded.WithIndexService(s.Index))
	}
	// The identity's grants, the service principal's or a delegation's, are
	// enforced by the node's own AuthMiddleware, the one its network
	// transports authorize with; a delegation without it is refused rather
	// than run unbounded.
	mw, err := c.opts.embeddedIdentity.authMiddleware()
	if err != nil {
		return nil, err
	}
	if mw != nil {
		embOpts = append(embOpts, qrdbembedded.WithAuthMiddleware(mw))
	}

	t, err := qrdbembedded.New(s.KV, s.Admin, embOpts...)
	if err != nil {
		return nil, err
	}
	// Connect is a no-op for the qrdb embedded transport; skip it. The
	// network-transport Connect-with-health-check workaround does not
	// apply here because the embedded transport never dials anything.
	//
	// An embedded identity binds the qrdb sub-client's calls through the
	// same binder the quicbaas embedded transport binds through, so
	// DataStore, TenantMgmt and the qrdb-backed Security() verbs never
	// reach a servicer unbound.
	if bind := c.opts.embeddedIdentity.bind; bind != nil {
		return qrdbsdk.NewClient(&boundQRDBTransport{next: t, bind: bind})
	}
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
