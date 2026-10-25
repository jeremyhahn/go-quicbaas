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

package quicbaas

import (
	"context"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	qrdbembedded "github.com/jeremyhahn/go-qrdb/sdk/go/transport/embedded"
	"github.com/jeremyhahn/go-xkms/sdk/go/secure"
)

// boundQRDBTransport is the in-process qrdb transport an embedded client's
// qrdb sub-client runs over when the client has an embedded identity. It
// binds every call's context through that identity's binder -- the same one
// the quicbaas embedded transport binds through -- before the call reaches
// qrdb, so a refused or revoked delegation never dispatches, and a valid one
// carries its tenant and principal into the qrdb servicers.
//
// It implements exactly the qrdb capabilities the SDK's qrdb-backed accessors
// reach: KV and Index (DataStore), Barrier, Tenant, Ceremony, Credential,
// SealStore, User and UserAuthFactor. A capability it does not implement is
// one no accessor reaches, so none is served unbound.
type boundQRDBTransport struct {
	next *qrdbembedded.Transport
	bind func(context.Context) (context.Context, error)
}

var (
	_ qrdbtransport.Transport            = (*boundQRDBTransport)(nil)
	_ qrdbtransport.KVClient             = (*boundQRDBTransport)(nil)
	_ qrdbtransport.IndexClient          = (*boundQRDBTransport)(nil)
	_ qrdbtransport.BarrierClient        = (*boundQRDBTransport)(nil)
	_ qrdbtransport.TenantClient         = (*boundQRDBTransport)(nil)
	_ qrdbtransport.CeremonyClient       = (*boundQRDBTransport)(nil)
	_ qrdbtransport.CredentialClient     = (*boundQRDBTransport)(nil)
	_ qrdbtransport.SealStoreClient      = (*boundQRDBTransport)(nil)
	_ qrdbtransport.UserClient           = (*boundQRDBTransport)(nil)
	_ qrdbtransport.UserAuthFactorClient = (*boundQRDBTransport)(nil)
)

// boundCall binds ctx and runs call with the bound context.
func boundCall(b *boundQRDBTransport, ctx context.Context, call func(context.Context) error) error {
	bound, err := b.bind(ctx)
	if err != nil {
		return err
	}
	return call(bound)
}

// boundResult binds ctx and runs call with the bound context, returning its
// result.
func boundResult[R any](b *boundQRDBTransport, ctx context.Context, call func(context.Context) (R, error)) (R, error) {
	bound, err := b.bind(ctx)
	if err != nil {
		var zero R
		return zero, err
	}
	return call(bound)
}

// --- Transport ---

func (b *boundQRDBTransport) Connect(ctx context.Context) error { return b.next.Connect(ctx) }

func (b *boundQRDBTransport) Close() error { return b.next.Close() }

func (b *boundQRDBTransport) Healthy(ctx context.Context) bool { return b.next.Healthy(ctx) }

func (b *boundQRDBTransport) Request(ctx context.Context, method string, req, resp any) error {
	return boundCall(b, ctx, func(ctx context.Context) error { return b.next.Request(ctx, method, req, resp) })
}

// --- KV ---

func (b *boundQRDBTransport) Put(ctx context.Context, key string, value []byte, opts ...qrdbtransport.WriteOption) error {
	return boundCall(b, ctx, func(ctx context.Context) error { return b.next.Put(ctx, key, value, opts...) })
}

func (b *boundQRDBTransport) Get(ctx context.Context, key string, opts ...qrdbtransport.ReadOption) ([]byte, error) {
	return boundResult(b, ctx, func(ctx context.Context) ([]byte, error) { return b.next.Get(ctx, key, opts...) })
}

func (b *boundQRDBTransport) Delete(ctx context.Context, key string, opts ...qrdbtransport.WriteOption) error {
	return boundCall(b, ctx, func(ctx context.Context) error { return b.next.Delete(ctx, key, opts...) })
}

func (b *boundQRDBTransport) List(ctx context.Context, prefix string, opts ...qrdbtransport.ReadOption) ([]string, error) {
	return boundResult(b, ctx, func(ctx context.Context) ([]string, error) { return b.next.List(ctx, prefix, opts...) })
}

func (b *boundQRDBTransport) Scan(ctx context.Context, prefix string, opts ...qrdbtransport.ReadOption) (map[string][]byte, error) {
	return boundResult(b, ctx, func(ctx context.Context) (map[string][]byte, error) { return b.next.Scan(ctx, prefix, opts...) })
}

func (b *boundQRDBTransport) Exists(ctx context.Context, key string, opts ...qrdbtransport.ReadOption) (bool, error) {
	return boundResult(b, ctx, func(ctx context.Context) (bool, error) { return b.next.Exists(ctx, key, opts...) })
}

// --- Index ---

func (b *boundQRDBTransport) IndexCreate(ctx context.Context, name, fieldName string, config qrdbtransport.IndexConfig) error {
	return boundCall(b, ctx, func(ctx context.Context) error { return b.next.IndexCreate(ctx, name, fieldName, config) })
}

func (b *boundQRDBTransport) IndexDrop(ctx context.Context, name string) error {
	return boundCall(b, ctx, func(ctx context.Context) error { return b.next.IndexDrop(ctx, name) })
}

func (b *boundQRDBTransport) IndexQuery(ctx context.Context, indexName, value string) (*qrdbtransport.IndexQueryResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.IndexQueryResponse, error) {
		return b.next.IndexQuery(ctx, indexName, value)
	})
}

func (b *boundQRDBTransport) IndexScan(ctx context.Context, indexName string, start, end *string) (*qrdbtransport.IndexScanResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.IndexScanResponse, error) {
		return b.next.IndexScan(ctx, indexName, start, end)
	})
}

func (b *boundQRDBTransport) IndexList(ctx context.Context) (*qrdbtransport.IndexListResponse, error) {
	return boundResult(b, ctx, b.next.IndexList)
}

// --- Barrier ---

func (b *boundQRDBTransport) BarrierInitialize(ctx context.Context, req *qrdbtransport.BarrierInitializeRequest) error {
	return boundCall(b, ctx, func(ctx context.Context) error { return b.next.BarrierInitialize(ctx, req) })
}

func (b *boundQRDBTransport) BarrierUnseal(ctx context.Context, req *qrdbtransport.BarrierUnsealRequest) error {
	return boundCall(b, ctx, func(ctx context.Context) error { return b.next.BarrierUnseal(ctx, req) })
}

func (b *boundQRDBTransport) BarrierSeal(ctx context.Context) error {
	return boundCall(b, ctx, b.next.BarrierSeal)
}

func (b *boundQRDBTransport) BarrierStatus(ctx context.Context) (*qrdbtransport.BarrierStatusResponse, error) {
	return boundResult(b, ctx, b.next.BarrierStatus)
}

func (b *boundQRDBTransport) BarrierClusterUnseal(ctx context.Context) (*qrdbtransport.ClusterSeal, error) {
	return boundResult(b, ctx, b.next.BarrierClusterUnseal)
}

func (b *boundQRDBTransport) BarrierInitializeShamir(ctx context.Context, req *qrdbtransport.BarrierInitializeShamirRequest) (*qrdbtransport.BarrierInitializeShamirResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.BarrierInitializeShamirResponse, error) {
		return b.next.BarrierInitializeShamir(ctx, req)
	})
}

func (b *boundQRDBTransport) BarrierEnableEncryption(ctx context.Context, req *qrdbtransport.BarrierEnableEncryptionRequest) (*qrdbtransport.BarrierEnableEncryptionResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.BarrierEnableEncryptionResponse, error) {
		return b.next.BarrierEnableEncryption(ctx, req)
	})
}

func (b *boundQRDBTransport) BarrierGenerateRecoveryKeys(ctx context.Context, req *qrdbtransport.BarrierGenerateRecoveryKeysRequest) (*qrdbtransport.BarrierRecoveryKeysResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.BarrierRecoveryKeysResponse, error) {
		return b.next.BarrierGenerateRecoveryKeys(ctx, req)
	})
}

func (b *boundQRDBTransport) BarrierDeleteRecoveryKeys(ctx context.Context) error {
	return boundCall(b, ctx, b.next.BarrierDeleteRecoveryKeys)
}

func (b *boundQRDBTransport) BarrierHasRecoveryKeys(ctx context.Context) (*qrdbtransport.BarrierHasRecoveryKeysResponse, error) {
	return boundResult(b, ctx, b.next.BarrierHasRecoveryKeys)
}

func (b *boundQRDBTransport) OfficerRegister(ctx context.Context, req *qrdbtransport.OfficerRegisterRequest) (*qrdbtransport.OfficerRegisterResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.OfficerRegisterResponse, error) {
		return b.next.OfficerRegister(ctx, req)
	})
}

func (b *boundQRDBTransport) OfficerList(ctx context.Context) ([]*qrdbtransport.OfficerStatus, error) {
	return boundResult(b, ctx, b.next.OfficerList)
}

func (b *boundQRDBTransport) ShareClaimBegin(ctx context.Context, req *qrdbtransport.ShareClaimBeginRequest) (*qrdbtransport.ShareClaimBeginResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.ShareClaimBeginResponse, error) {
		return b.next.ShareClaimBegin(ctx, req)
	})
}

func (b *boundQRDBTransport) ShareClaimComplete(ctx context.Context, req *qrdbtransport.ShareClaimCompleteRequest) (*qrdbtransport.ShareClaimCompleteResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.ShareClaimCompleteResponse, error) {
		return b.next.ShareClaimComplete(ctx, req)
	})
}

func (b *boundQRDBTransport) RecoveryOfficerRegister(ctx context.Context, req *qrdbtransport.OfficerRegisterRequest) (*qrdbtransport.OfficerRegisterResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.OfficerRegisterResponse, error) {
		return b.next.RecoveryOfficerRegister(ctx, req)
	})
}

func (b *boundQRDBTransport) RecoveryShareClaimBegin(ctx context.Context, req *qrdbtransport.ShareClaimBeginRequest) (*qrdbtransport.ShareClaimBeginResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.ShareClaimBeginResponse, error) {
		return b.next.RecoveryShareClaimBegin(ctx, req)
	})
}

func (b *boundQRDBTransport) RecoveryShareClaimComplete(ctx context.Context, req *qrdbtransport.ShareClaimCompleteRequest) (*qrdbtransport.ShareClaimCompleteResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.ShareClaimCompleteResponse, error) {
		return b.next.RecoveryShareClaimComplete(ctx, req)
	})
}

func (b *boundQRDBTransport) BarrierQuorumBegin(ctx context.Context, req *qrdbtransport.QuorumBeginRequest) (*qrdbtransport.QuorumChallenge, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.QuorumChallenge, error) {
		return b.next.BarrierQuorumBegin(ctx, req)
	})
}

func (b *boundQRDBTransport) BarrierQuorumSubmit(ctx context.Context, req *qrdbtransport.QuorumSubmitRequest) (*qrdbtransport.QuorumOutcome, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.QuorumOutcome, error) {
		return b.next.BarrierQuorumSubmit(ctx, req)
	})
}

func (b *boundQRDBTransport) BarrierRekeyStart(ctx context.Context, req *qrdbtransport.RekeyStartRequest) (*qrdbtransport.QuorumRound, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.QuorumRound, error) {
		return b.next.BarrierRekeyStart(ctx, req)
	})
}

func (b *boundQRDBTransport) BarrierOfficerRevoke(ctx context.Context, req *qrdbtransport.OfficerRevokeRequest) (*qrdbtransport.QuorumRound, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.QuorumRound, error) {
		return b.next.BarrierOfficerRevoke(ctx, req)
	})
}

// --- Tenant ---

func (b *boundQRDBTransport) CreateTenant(ctx context.Context, req *qrdbtransport.CreateTenantRequest) (*qrdbtransport.CreateTenantResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.CreateTenantResponse, error) {
		return b.next.CreateTenant(ctx, req)
	})
}

func (b *boundQRDBTransport) GetTenant(ctx context.Context, tenantID string) (*qrdbtransport.GetTenantResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.GetTenantResponse, error) {
		return b.next.GetTenant(ctx, tenantID)
	})
}

func (b *boundQRDBTransport) ListTenants(ctx context.Context) (*qrdbtransport.ListTenantsResponse, error) {
	return boundResult(b, ctx, b.next.ListTenants)
}

func (b *boundQRDBTransport) ListDeletedTenants(ctx context.Context, req *qrdbtransport.ListDeletedTenantsRequest) (*qrdbtransport.ListDeletedTenantsResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.ListDeletedTenantsResponse, error) {
		return b.next.ListDeletedTenants(ctx, req)
	})
}

func (b *boundQRDBTransport) ReconcileDeletedTenants(ctx context.Context) (*qrdbtransport.ReconcileDeletedTenantsResponse, error) {
	return boundResult(b, ctx, b.next.ReconcileDeletedTenants)
}

func (b *boundQRDBTransport) DeleteTenant(ctx context.Context, tenantID string) error {
	return boundCall(b, ctx, func(ctx context.Context) error { return b.next.DeleteTenant(ctx, tenantID) })
}

func (b *boundQRDBTransport) UpdateTenant(ctx context.Context, req *qrdbtransport.UpdateTenantRequest) (*qrdbtransport.UpdateTenantResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.UpdateTenantResponse, error) {
		return b.next.UpdateTenant(ctx, req)
	})
}

func (b *boundQRDBTransport) UpdateTenantQuotas(ctx context.Context, req *qrdbtransport.UpdateTenantQuotasRequest) (*qrdbtransport.UpdateTenantQuotasResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.UpdateTenantQuotasResponse, error) {
		return b.next.UpdateTenantQuotas(ctx, req)
	})
}

func (b *boundQRDBTransport) UpdateTenantEntitlements(ctx context.Context, req *qrdbtransport.UpdateTenantEntitlementsRequest) (*qrdbtransport.UpdateTenantEntitlementsResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.UpdateTenantEntitlementsResponse, error) {
		return b.next.UpdateTenantEntitlements(ctx, req)
	})
}

func (b *boundQRDBTransport) AddTrustAnchors(ctx context.Context, req *qrdbtransport.AddTrustAnchorsRequest) (*qrdbtransport.TrustAnchorList, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.TrustAnchorList, error) {
		return b.next.AddTrustAnchors(ctx, req)
	})
}

func (b *boundQRDBTransport) ListTrustAnchors(ctx context.Context, tenantID string) (*qrdbtransport.TrustAnchorList, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.TrustAnchorList, error) {
		return b.next.ListTrustAnchors(ctx, tenantID)
	})
}

func (b *boundQRDBTransport) RemoveTrustAnchor(ctx context.Context, tenantID, fingerprint string) error {
	return boundCall(b, ctx, func(ctx context.Context) error { return b.next.RemoveTrustAnchor(ctx, tenantID, fingerprint) })
}

func (b *boundQRDBTransport) TenantBarrierInit(ctx context.Context, req *qrdbtransport.TenantBarrierInitRequest) (*qrdbtransport.TenantBarrierInitResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.TenantBarrierInitResponse, error) {
		return b.next.TenantBarrierInit(ctx, req)
	})
}

func (b *boundQRDBTransport) TenantBarrierUnseal(ctx context.Context, req *qrdbtransport.TenantBarrierUnsealRequest) error {
	return boundCall(b, ctx, func(ctx context.Context) error { return b.next.TenantBarrierUnseal(ctx, req) })
}

func (b *boundQRDBTransport) TenantBarrierSeal(ctx context.Context, req *qrdbtransport.TenantBarrierSealRequest) error {
	return boundCall(b, ctx, func(ctx context.Context) error { return b.next.TenantBarrierSeal(ctx, req) })
}

func (b *boundQRDBTransport) TenantBarrierStatus(ctx context.Context, req *qrdbtransport.TenantBarrierStatusRequest) (*qrdbtransport.TenantBarrierStatusResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.TenantBarrierStatusResponse, error) {
		return b.next.TenantBarrierStatus(ctx, req)
	})
}

func (b *boundQRDBTransport) TenantBarrierOfficerRegister(ctx context.Context, req *qrdbtransport.TenantOfficerRegisterRequest) (*qrdbtransport.OfficerRegisterResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.OfficerRegisterResponse, error) {
		return b.next.TenantBarrierOfficerRegister(ctx, req)
	})
}

func (b *boundQRDBTransport) TenantBarrierOfficerList(ctx context.Context, tenantID string) ([]*qrdbtransport.OfficerStatus, error) {
	return boundResult(b, ctx, func(ctx context.Context) ([]*qrdbtransport.OfficerStatus, error) {
		return b.next.TenantBarrierOfficerList(ctx, tenantID)
	})
}

func (b *boundQRDBTransport) TenantBarrierShareClaimBegin(ctx context.Context, req *qrdbtransport.TenantShareClaimBeginRequest) (*qrdbtransport.ShareClaimBeginResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.ShareClaimBeginResponse, error) {
		return b.next.TenantBarrierShareClaimBegin(ctx, req)
	})
}

func (b *boundQRDBTransport) TenantBarrierShareClaimComplete(ctx context.Context, req *qrdbtransport.TenantShareClaimCompleteRequest) (*qrdbtransport.ShareClaimCompleteResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.ShareClaimCompleteResponse, error) {
		return b.next.TenantBarrierShareClaimComplete(ctx, req)
	})
}

func (b *boundQRDBTransport) TenantBarrierQuorumBegin(ctx context.Context, req *qrdbtransport.TenantQuorumBeginRequest) (*qrdbtransport.QuorumChallenge, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.QuorumChallenge, error) {
		return b.next.TenantBarrierQuorumBegin(ctx, req)
	})
}

func (b *boundQRDBTransport) TenantBarrierQuorumSubmit(ctx context.Context, req *qrdbtransport.TenantQuorumSubmitRequest) (*qrdbtransport.QuorumOutcome, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.QuorumOutcome, error) {
		return b.next.TenantBarrierQuorumSubmit(ctx, req)
	})
}

func (b *boundQRDBTransport) TenantBarrierRekeyStart(ctx context.Context, req *qrdbtransport.TenantRekeyStartRequest) (*qrdbtransport.QuorumRound, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.QuorumRound, error) {
		return b.next.TenantBarrierRekeyStart(ctx, req)
	})
}

func (b *boundQRDBTransport) TenantBarrierOfficerRevoke(ctx context.Context, req *qrdbtransport.TenantOfficerRevokeRequest) (*qrdbtransport.QuorumRound, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.QuorumRound, error) {
		return b.next.TenantBarrierOfficerRevoke(ctx, req)
	})
}

// --- Ceremony ---

func (b *boundQRDBTransport) CeremonyInitialize(ctx context.Context, req *qrdbtransport.CeremonyInitializeRequest) (*qrdbtransport.CeremonyInitializeResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.CeremonyInitializeResponse, error) {
		return b.next.CeremonyInitialize(ctx, req)
	})
}

func (b *boundQRDBTransport) CeremonyStatus(ctx context.Context) (*qrdbtransport.CeremonyStatusResponse, error) {
	return boundResult(b, ctx, b.next.CeremonyStatus)
}

func (b *boundQRDBTransport) CeremonySignCSR(ctx context.Context, req *qrdbtransport.SignCSRInitRequest) (*qrdbtransport.SignCSRInitResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.SignCSRInitResponse, error) {
		return b.next.CeremonySignCSR(ctx, req)
	})
}

// --- Credential ---

func (b *boundQRDBTransport) CredentialSubmit(ctx context.Context, req *qrdbtransport.CredentialSubmitRequest) error {
	return boundCall(b, ctx, func(ctx context.Context) error { return b.next.CredentialSubmit(ctx, req) })
}

func (b *boundQRDBTransport) CredentialStrategy(ctx context.Context) (*qrdbtransport.CredentialStrategyResponse, error) {
	return boundResult(b, ctx, b.next.CredentialStrategy)
}

func (b *boundQRDBTransport) CredentialDelete(ctx context.Context, name string) error {
	return boundCall(b, ctx, func(ctx context.Context) error { return b.next.CredentialDelete(ctx, name) })
}

func (b *boundQRDBTransport) CredentialList(ctx context.Context, req *qrdbtransport.CredentialListRequest) (*qrdbtransport.CredentialListResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.CredentialListResponse, error) {
		return b.next.CredentialList(ctx, req)
	})
}

func (b *boundQRDBTransport) CredentialRotate(ctx context.Context, req *qrdbtransport.CredentialRotateRequest) (*qrdbtransport.CredentialRotateResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.CredentialRotateResponse, error) {
		return b.next.CredentialRotate(ctx, req)
	})
}

func (b *boundQRDBTransport) CredentialRetrieveVersionBuffer(ctx context.Context, req *qrdbtransport.CredentialRetrieveVersionRequest) (*secure.Buffer, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*secure.Buffer, error) {
		return b.next.CredentialRetrieveVersionBuffer(ctx, req)
	})
}

func (b *boundQRDBTransport) CredentialListVersions(ctx context.Context, req *qrdbtransport.CredentialListVersionsRequest) (*qrdbtransport.CredentialListVersionsResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.CredentialListVersionsResponse, error) {
		return b.next.CredentialListVersions(ctx, req)
	})
}

func (b *boundQRDBTransport) CredentialGCExpired(ctx context.Context, req *qrdbtransport.CredentialGCRequest) (*qrdbtransport.CredentialGCResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.CredentialGCResponse, error) {
		return b.next.CredentialGCExpired(ctx, req)
	})
}

func (b *boundQRDBTransport) CredentialStore(ctx context.Context, name string, value []byte) error {
	return boundCall(b, ctx, func(ctx context.Context) error { return b.next.CredentialStore(ctx, name, value) })
}

func (b *boundQRDBTransport) CredentialRetrieveBuffer(ctx context.Context, name string) (*secure.Buffer, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*secure.Buffer, error) {
		return b.next.CredentialRetrieveBuffer(ctx, name)
	})
}

// --- SealStore ---

func (b *boundQRDBTransport) SealStorePut(ctx context.Context, req *qrdbtransport.SealStorePutRequest) error {
	return boundCall(b, ctx, func(ctx context.Context) error { return b.next.SealStorePut(ctx, req) })
}

func (b *boundQRDBTransport) SealStoreGet(ctx context.Context, req *qrdbtransport.SealStoreGetRequest) (*qrdbtransport.SealStoreGetResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.SealStoreGetResponse, error) {
		return b.next.SealStoreGet(ctx, req)
	})
}

func (b *boundQRDBTransport) SealStoreDelete(ctx context.Context, req *qrdbtransport.SealStoreDeleteRequest) error {
	return boundCall(b, ctx, func(ctx context.Context) error { return b.next.SealStoreDelete(ctx, req) })
}

func (b *boundQRDBTransport) SealStoreList(ctx context.Context) (*qrdbtransport.SealStoreListResponse, error) {
	return boundResult(b, ctx, b.next.SealStoreList)
}

func (b *boundQRDBTransport) SealStoreReseal(ctx context.Context, req *qrdbtransport.SealStoreResealRequest) error {
	return boundCall(b, ctx, func(ctx context.Context) error { return b.next.SealStoreReseal(ctx, req) })
}

func (b *boundQRDBTransport) SealStoreStatus(ctx context.Context) (*qrdbtransport.SealStoreStatusResponse, error) {
	return boundResult(b, ctx, b.next.SealStoreStatus)
}

// --- User ---

func (b *boundQRDBTransport) CreateUser(ctx context.Context, req *qrdbtransport.CreateUserRequest) (*qrdbtransport.User, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.User, error) { return b.next.CreateUser(ctx, req) })
}

func (b *boundQRDBTransport) GetUser(ctx context.Context, id uint64) (*qrdbtransport.User, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.User, error) { return b.next.GetUser(ctx, id) })
}

func (b *boundQRDBTransport) GetUserByUsername(ctx context.Context, tenantID, username string) (*qrdbtransport.User, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.User, error) {
		return b.next.GetUserByUsername(ctx, tenantID, username)
	})
}

func (b *boundQRDBTransport) GetUserByEmail(ctx context.Context, tenantID, email string) (*qrdbtransport.User, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.User, error) {
		return b.next.GetUserByEmail(ctx, tenantID, email)
	})
}

func (b *boundQRDBTransport) GetUserByCertThumbprint(ctx context.Context, thumbprint string) (*qrdbtransport.User, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.User, error) {
		return b.next.GetUserByCertThumbprint(ctx, thumbprint)
	})
}

func (b *boundQRDBTransport) AddCertBinding(ctx context.Context, userID uint64, thumbprint string, notAfterUnix int64) (*qrdbtransport.User, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.User, error) {
		return b.next.AddCertBinding(ctx, userID, thumbprint, notAfterUnix)
	})
}

func (b *boundQRDBTransport) RemoveCertBinding(ctx context.Context, userID uint64, thumbprint string) (*qrdbtransport.User, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.User, error) {
		return b.next.RemoveCertBinding(ctx, userID, thumbprint)
	})
}

func (b *boundQRDBTransport) UpdateUser(ctx context.Context, req *qrdbtransport.UpdateUserRequest) (*qrdbtransport.User, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.User, error) { return b.next.UpdateUser(ctx, req) })
}

func (b *boundQRDBTransport) DeleteUser(ctx context.Context, id uint64) error {
	return boundCall(b, ctx, func(ctx context.Context) error { return b.next.DeleteUser(ctx, id) })
}

func (b *boundQRDBTransport) ListUsers(ctx context.Context, req *qrdbtransport.ListUsersRequest) (*qrdbtransport.ListUsersResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.ListUsersResponse, error) {
		return b.next.ListUsers(ctx, req)
	})
}

func (b *boundQRDBTransport) EnableUser(ctx context.Context, id uint64) error {
	return boundCall(b, ctx, func(ctx context.Context) error { return b.next.EnableUser(ctx, id) })
}

func (b *boundQRDBTransport) DisableUser(ctx context.Context, id uint64) error {
	return boundCall(b, ctx, func(ctx context.Context) error { return b.next.DisableUser(ctx, id) })
}

func (b *boundQRDBTransport) SuspendUser(ctx context.Context, id uint64) error {
	return boundCall(b, ctx, func(ctx context.Context) error { return b.next.SuspendUser(ctx, id) })
}

func (b *boundQRDBTransport) UnsuspendUser(ctx context.Context, id uint64) error {
	return boundCall(b, ctx, func(ctx context.Context) error { return b.next.UnsuspendUser(ctx, id) })
}

// --- UserAuthFactor ---

func (b *boundQRDBTransport) AddAuthFactor(ctx context.Context, req *qrdbtransport.AuthFactorAddRequest) (*qrdbtransport.AuthFactorAddResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.AuthFactorAddResponse, error) {
		return b.next.AddAuthFactor(ctx, req)
	})
}

func (b *boundQRDBTransport) ListAuthFactors(ctx context.Context, req *qrdbtransport.AuthFactorListRequest) (*qrdbtransport.AuthFactorListResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.AuthFactorListResponse, error) {
		return b.next.ListAuthFactors(ctx, req)
	})
}

func (b *boundQRDBTransport) GetAuthFactor(ctx context.Context, req *qrdbtransport.AuthFactorGetRequest) (*qrdbtransport.AuthFactorGetResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.AuthFactorGetResponse, error) {
		return b.next.GetAuthFactor(ctx, req)
	})
}

func (b *boundQRDBTransport) RemoveAuthFactor(ctx context.Context, req *qrdbtransport.AuthFactorRemoveRequest) (*qrdbtransport.AuthFactorRemoveResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.AuthFactorRemoveResponse, error) {
		return b.next.RemoveAuthFactor(ctx, req)
	})
}

func (b *boundQRDBTransport) DeleteAuthFactor(ctx context.Context, req *qrdbtransport.AuthFactorDeleteRequest) (*qrdbtransport.AuthFactorDeleteResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.AuthFactorDeleteResponse, error) {
		return b.next.DeleteAuthFactor(ctx, req)
	})
}

func (b *boundQRDBTransport) RotateAuthFactor(ctx context.Context, req *qrdbtransport.AuthFactorRotateRequest) (*qrdbtransport.AuthFactorRotateResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.AuthFactorRotateResponse, error) {
		return b.next.RotateAuthFactor(ctx, req)
	})
}

func (b *boundQRDBTransport) RelabelAuthFactor(ctx context.Context, req *qrdbtransport.AuthFactorRelabelRequest) (*qrdbtransport.AuthFactorRelabelResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.AuthFactorRelabelResponse, error) {
		return b.next.RelabelAuthFactor(ctx, req)
	})
}

func (b *boundQRDBTransport) LoginAuthFactor(ctx context.Context, req *qrdbtransport.AuthFactorLoginRequest) (*qrdbtransport.AuthFactorLoginResponse, error) {
	return boundResult(b, ctx, func(ctx context.Context) (*qrdbtransport.AuthFactorLoginResponse, error) {
		return b.next.LoginAuthFactor(ctx, req)
	})
}
