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
	"encoding/base64"
	"time"

	"github.com/jeremyhahn/go-qrdb/pkg/ceremony"
	qrdbservice "github.com/jeremyhahn/go-qrdb/pkg/service"
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// ---------------------------------------------------------------------------
// transport.CeremonyClient — delegates to BasCeremonyService when available
// ---------------------------------------------------------------------------

// CeremonyInitialize starts the initialization ceremony by delegating to the
// injected BasCeremonyService. Returns BaasTransportNotSupportedError when no
// service has been configured.
func (t *baasTransport) CeremonyInitialize(ctx context.Context, _ *transport.CeremonyInitializeRequest) (*transport.CeremonyInitializeResponse, error) {
	if t.ceremonySvc == nil {
		return nil, &BaasTransportNotSupportedError{Operation: "CeremonyInitialize"}
	}
	if t.ceremonyCA == nil {
		return nil, &BaasTransportNotSupportedError{Operation: "CeremonyInitialize (no CA)"}
	}
	result, err := t.ceremonySvc.CeremonyInitialize(ctx, t.ceremonyCA)
	if err != nil {
		return nil, err
	}
	return &transport.CeremonyInitializeResponse{
		SPKIPin:    result.SPKIPin,
		CACertPEM:  string(result.CACertPEM),
		TLSCertPEM: string(result.TLSCertPEM),
		SOCertPEM:  string(result.SOCertPEM),
		SOKeyPEM:   string(result.SOKeyPEM),
		State:      result.State,
		ShareCount: result.ShareCount,
	}, nil
}

// CeremonyStatus returns the current ceremony state by delegating to the
// injected BasCeremonyService. Returns BaasTransportNotSupportedError when no
// service has been configured.
func (t *baasTransport) CeremonyStatus(ctx context.Context) (*transport.CeremonyStatusResponse, error) {
	if t.ceremonySvc == nil {
		return nil, &BaasTransportNotSupportedError{Operation: "CeremonyStatus"}
	}
	info, err := t.ceremonySvc.CeremonyStatus(ctx)
	if err != nil {
		return nil, err
	}
	return &transport.CeremonyStatusResponse{
		State:   info.State,
		SPKIPin: info.SPKIPin,
	}, nil
}

// CeremonyClaimCertBegin starts a certificate claim challenge for the named
// officer by delegating to the injected BasCeremonyService. Returns
// BaasTransportNotSupportedError when no service has been configured.
func (t *baasTransport) CeremonyClaimCertBegin(ctx context.Context, req *transport.ClaimCertBeginRequest) (*transport.ClaimCertBeginResponse, error) {
	if t.ceremonySvc == nil {
		return nil, &BaasTransportNotSupportedError{Operation: "CeremonyClaimCertBegin"}
	}
	challenge, err := t.ceremonySvc.CeremonyBeginClaimCert(ctx, req.Username)
	if err != nil {
		return nil, err
	}
	return &transport.ClaimCertBeginResponse{
		NonceHex:  challenge.NonceHex,
		ExpiresAt: challenge.ExpiresAt.Format(time.RFC3339),
	}, nil
}

// CeremonyClaimCertComplete completes a certificate claim by submitting the
// signed nonce. The base64-encoded signature from the request is decoded before
// passing to the service layer. Returns BaasTransportNotSupportedError when no
// service has been configured.
func (t *baasTransport) CeremonyClaimCertComplete(ctx context.Context, req *transport.ClaimCertCompleteRequest) (*transport.ClaimCertCompleteResponse, error) {
	if t.ceremonySvc == nil {
		return nil, &BaasTransportNotSupportedError{Operation: "CeremonyClaimCertComplete"}
	}
	sigBytes, err := base64.StdEncoding.DecodeString(req.Signature)
	if err != nil {
		return nil, &transport.RequestError{Method: "CeremonyClaimCertComplete", Err: err}
	}
	result, err := t.ceremonySvc.CeremonyCompleteClaimCert(ctx, req.Username, req.NonceHex, sigBytes)
	if err != nil {
		return nil, err
	}
	return &transport.ClaimCertCompleteResponse{
		CertPEM:   string(result.CertPEM),
		CACertPEM: string(result.CACertPEM),
	}, nil
}

// CeremonyClaimShare retrieves the Shamir share for an officer who has already
// claimed their certificate by delegating to the injected BasCeremonyService.
// Returns BaasTransportNotSupportedError when no service has been configured.
func (t *baasTransport) CeremonyClaimShare(ctx context.Context, req *transport.ClaimShareRequest) (*transport.ClaimShareResponse, error) {
	if t.ceremonySvc == nil {
		return nil, &BaasTransportNotSupportedError{Operation: "CeremonyClaimShare"}
	}
	shareBytes, err := t.ceremonySvc.CeremonyClaimShare(ctx, req.Username)
	if err != nil {
		return nil, err
	}
	return &transport.ClaimShareResponse{
		ShareJSON: string(shareBytes),
	}, nil
}

// CeremonySignCSR submits a CSR for signing during or after the ceremony by
// delegating to the injected BasCeremonyService. Returns
// BaasTransportNotSupportedError when no service has been configured.
func (t *baasTransport) CeremonySignCSR(ctx context.Context, req *transport.SignCSRInitRequest) (*transport.SignCSRInitResponse, error) {
	if t.ceremonySvc == nil {
		return nil, &BaasTransportNotSupportedError{Operation: "CeremonySignCSR"}
	}
	result, err := t.ceremonySvc.CeremonySignCSR(ctx, req.Username, req.SOPin, req.CSRPEM, req.Role)
	if err != nil {
		return nil, err
	}
	return &transport.SignCSRInitResponse{
		CertPEM: string(result.CertPEM),
	}, nil
}

// ---------------------------------------------------------------------------
// transport.CredentialClient — delegates to BaasCredentialService when available
// ---------------------------------------------------------------------------

// CredentialSubmit submits a named credential value by delegating to the
// injected BaasCredentialService. Returns BaasTransportNotSupportedError when
// no service has been configured.
func (t *baasTransport) CredentialSubmit(ctx context.Context, req *transport.CredentialSubmitRequest) error {
	if t.credentialSvc == nil {
		return &BaasTransportNotSupportedError{Operation: "CredentialSubmit"}
	}
	return t.credentialSvc.CredentialSubmit(ctx, req.Name, []byte(req.Value))
}

// CredentialStrategy returns the active credential management strategy by
// delegating to the injected BaasCredentialService. Returns
// BaasTransportNotSupportedError when no service has been configured.
func (t *baasTransport) CredentialStrategy(_ context.Context) (*transport.CredentialStrategyResponse, error) {
	if t.credentialSvc == nil {
		return nil, &BaasTransportNotSupportedError{Operation: "CredentialStrategy"}
	}
	return &transport.CredentialStrategyResponse{
		Strategy: t.credentialSvc.CredentialStrategy(),
	}, nil
}

// CredentialDelete removes a named credential by delegating to the injected
// BaasCredentialService. Returns BaasTransportNotSupportedError when no
// service has been configured.
func (t *baasTransport) CredentialDelete(ctx context.Context, name string) error {
	if t.credentialSvc == nil {
		return &BaasTransportNotSupportedError{Operation: "CredentialDelete"}
	}
	return t.credentialSvc.CredentialDelete(ctx, name)
}

// ---------------------------------------------------------------------------
// transport.CustodianGroupClient — delegates to BaasCustodianGroupService when available
// ---------------------------------------------------------------------------

// CreateCustodianGroup creates a new custodian group by delegating to the
// injected BaasCustodianGroupService. Returns BaasTransportNotSupportedError
// when no service has been configured.
func (t *baasTransport) CreateCustodianGroup(ctx context.Context, req *transport.CreateCustodianGroupRequest) (*transport.CreateCustodianGroupResponse, error) {
	if t.custodianGroupSvc == nil {
		return nil, &BaasTransportNotSupportedError{Operation: "CreateCustodianGroup"}
	}
	group, err := t.custodianGroupSvc.CreateGroup(
		ctx, req.ID, req.TenantID, req.Name, req.Purpose, req.Threshold, req.Total,
	)
	if err != nil {
		return nil, err
	}
	return &transport.CreateCustodianGroupResponse{Group: *group}, nil
}

// GetCustodianGroup retrieves a custodian group by ID by delegating to the
// injected BaasCustodianGroupService. Returns BaasTransportNotSupportedError
// when no service has been configured.
func (t *baasTransport) GetCustodianGroup(ctx context.Context, groupID string) (*transport.GetCustodianGroupResponse, error) {
	if t.custodianGroupSvc == nil {
		return nil, &BaasTransportNotSupportedError{Operation: "GetCustodianGroup"}
	}
	group, err := t.custodianGroupSvc.GetGroup(ctx, groupID)
	if err != nil {
		return nil, err
	}
	return &transport.GetCustodianGroupResponse{Group: *group}, nil
}

// ListCustodianGroups lists all custodian groups by delegating to the
// injected BaasCustodianGroupService. Returns BaasTransportNotSupportedError
// when no service has been configured.
func (t *baasTransport) ListCustodianGroups(ctx context.Context) (*transport.ListCustodianGroupsResponse, error) {
	if t.custodianGroupSvc == nil {
		return nil, &BaasTransportNotSupportedError{Operation: "ListCustodianGroups"}
	}
	groups, err := t.custodianGroupSvc.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	return &transport.ListCustodianGroupsResponse{Groups: groups}, nil
}

// DeleteCustodianGroup deletes a custodian group by delegating to the
// injected BaasCustodianGroupService. Returns BaasTransportNotSupportedError
// when no service has been configured.
func (t *baasTransport) DeleteCustodianGroup(ctx context.Context, groupID string) error {
	if t.custodianGroupSvc == nil {
		return &BaasTransportNotSupportedError{Operation: "DeleteCustodianGroup"}
	}
	return t.custodianGroupSvc.DeleteGroup(ctx, groupID)
}

// AddCustodianMember adds a member to a custodian group by delegating to the
// injected BaasCustodianGroupService. Returns BaasTransportNotSupportedError
// when no service has been configured.
func (t *baasTransport) AddCustodianMember(ctx context.Context, req *transport.AddCustodianMemberRequest) (*transport.AddCustodianMemberResponse, error) {
	if t.custodianGroupSvc == nil {
		return nil, &BaasTransportNotSupportedError{Operation: "AddCustodianMember"}
	}
	member, err := t.custodianGroupSvc.AddMember(ctx, req.GroupID, req.UserID, req.Username, req.Method)
	if err != nil {
		return nil, err
	}
	return &transport.AddCustodianMemberResponse{Member: *member}, nil
}

// RemoveCustodianMember removes a member from a custodian group by delegating
// to the injected BaasCustodianGroupService. Returns BaasTransportNotSupportedError
// when no service has been configured.
func (t *baasTransport) RemoveCustodianMember(ctx context.Context, req *transport.RemoveCustodianMemberRequest) error {
	if t.custodianGroupSvc == nil {
		return &BaasTransportNotSupportedError{Operation: "RemoveCustodianMember"}
	}
	return t.custodianGroupSvc.RemoveMember(ctx, req.GroupID, req.UserID)
}

// DistributeShares distributes Shamir shares to custodian group members by
// delegating to the injected BaasCustodianGroupService. Returns
// BaasTransportNotSupportedError when no service has been configured.
func (t *baasTransport) DistributeShares(ctx context.Context, req *transport.DistributeSharesRequest) (*transport.DistributeSharesResponse, error) {
	if t.custodianGroupSvc == nil {
		return nil, &BaasTransportNotSupportedError{Operation: "DistributeShares"}
	}
	count, err := t.custodianGroupSvc.DistributeShares(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}
	return &transport.DistributeSharesResponse{Distributed: count}, nil
}

// ---------------------------------------------------------------------------
// transport.ShareClient — delegates to BaasShareService when available
// ---------------------------------------------------------------------------

// SubmitShare submits a received Shamir share back to the server by delegating
// to the injected BaasShareService. Returns BaasTransportNotSupportedError when
// no service has been configured.
func (t *baasTransport) SubmitShare(ctx context.Context, req *transport.SubmitShareRequest) (*transport.SubmitShareResponse, error) {
	if t.shareSvc == nil {
		return nil, &BaasTransportNotSupportedError{Operation: "SubmitShare"}
	}
	return t.shareSvc.SubmitShare(ctx, req)
}

// ListShares lists shares available for the authenticated user by delegating
// to the injected BaasShareService. Returns BaasTransportNotSupportedError when
// no service has been configured.
func (t *baasTransport) ListShares(ctx context.Context) (*transport.ListSharesResponse, error) {
	if t.shareSvc == nil {
		return nil, &BaasTransportNotSupportedError{Operation: "ListShares"}
	}
	shares, err := t.shareSvc.ListShares(ctx)
	if err != nil {
		return nil, err
	}
	return &transport.ListSharesResponse{Shares: shares}, nil
}

// GetShareCollectionStatus returns the collection status for a group by
// delegating to the injected BaasShareService. Returns
// BaasTransportNotSupportedError when no service has been configured.
func (t *baasTransport) GetShareCollectionStatus(ctx context.Context, groupID string) (*transport.ShareCollectionStatus, error) {
	if t.shareSvc == nil {
		return nil, &BaasTransportNotSupportedError{Operation: "GetShareCollectionStatus"}
	}
	return t.shareSvc.GetShareCollectionStatus(ctx, groupID)
}

// ---------------------------------------------------------------------------
// Type aliases for injection into baasTransport and baasTransportStore.
// These reuse the go-qrdb service interfaces directly.
// ---------------------------------------------------------------------------

// BasCeremonyService is the ceremony lifecycle service interface required by
// the BaaS embedded transport. It is satisfied by qrdb's service.CeremonyServicer.
type BasCeremonyService = qrdbservice.CeremonyServicer

// BaasCredentialService is the credential management service interface required
// by the BaaS embedded transport. It is satisfied by qrdb's service.CredentialServicer.
type BaasCredentialService = qrdbservice.CredentialServicer

// BaasCustodianGroupService is the custodian group service interface required
// by the BaaS embedded transport. It is satisfied by qrdb's ceremony.CustodianGroupServicer.
type BaasCustodianGroupService = ceremony.CustodianGroupServicer

// BaasShareService is the Shamir share service interface required by the BaaS
// embedded transport. It is satisfied by qrdb's ceremony.ShareServicer.
type BaasShareService = ceremony.ShareServicer
