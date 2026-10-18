// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package embedded

import (
	"context"
	"strconv"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// iamIdentityServiceName is the service name the SDK IAM client sends for
// the identity-link verbs.
const iamIdentityServiceName = "quicbaas.IAM.Identity"

func init() {
	unaryHandlers[iamIdentityServiceName+"/LinkIdentity"] = handleIAMIdentityLink
	unaryHandlers[iamIdentityServiceName+"/UnlinkIdentity"] = handleIAMIdentityUnlink
	unaryHandlers[iamIdentityServiceName+"/ListUserIdentities"] = handleIAMIdentityListUserIdentities
}

// federationManagerProvider is satisfied by the QuicBaaS service; the REST,
// gRPC and MCP transports reach the same federation manager through it.
type federationManagerProvider interface {
	GetFederationManager(ctx context.Context) (iam.FederationManager, error)
}

func federationManagerFor(ctx context.Context, svc service.QuicBaaS) (iam.FederationManager, error) {
	p, ok := svc.(federationManagerProvider)
	if !ok {
		return nil, NewServiceNotConfiguredError(iamIdentityServiceName)
	}
	mgr, err := p.GetFederationManager(ctx)
	if err != nil {
		return nil, err
	}
	if mgr == nil {
		return nil, NewServiceNotConfiguredError(iamIdentityServiceName)
	}
	return mgr, nil
}

// identityTarget applies the tenant check the gRPC federation server makes
// (requireCallerTenant) and parses the user id the request names.
func identityTarget(ctx context.Context, method, tenantID, userID string) (string, uint64, error) {
	tenant, err := tenantctx.Target(ctx, iamIdentityServiceName+"/"+method, tenantID)
	if err != nil {
		return "", 0, err
	}
	id, err := parseIAMUserID(iamIdentityServiceName, method, userID)
	if err != nil {
		return "", 0, err
	}
	return tenant, id, nil
}

// identityRequestFields reads the string fields of an unexported SDK request
// type; a nil, non-struct or field-less request is the wrong request type.
func identityRequestFields(method, expected string, req interface{}, names ...string) ([]string, error) {
	values := make([]string, 0, len(names))
	for _, name := range names {
		v, ok := iamRequestField(req, name)
		if !ok {
			return nil, NewInvalidRequestTypeError(iamIdentityServiceName, method, expected, typeNameOf(req))
		}
		values = append(values, v)
	}
	return values, nil
}

func handleIAMIdentityLink(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*cli.LinkIdentityOptions)
	if !ok || r == nil {
		return nil, NewInvalidRequestTypeError(iamIdentityServiceName, "LinkIdentity", "*cli.LinkIdentityOptions", typeNameOf(req))
	}
	mgr, err := federationManagerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	tenant, userID, err := identityTarget(ctx, "LinkIdentity", r.TenantID, r.UserID)
	if err != nil {
		return nil, err
	}
	// The client's auth code is the external token gRPC and MCP pass on.
	if err := mgr.LinkAccount(ctx, userID, tenant, r.ProviderID, r.AuthCode); err != nil {
		return nil, err
	}
	// LinkAccount returns no link record; like REST, answer with the link
	// the request established.
	return &cli.FederatedIdentity{
		UserID:     strconv.FormatUint(userID, 10),
		TenantID:   tenant,
		ProviderID: r.ProviderID,
		Created:    time.Now(),
	}, nil
}

func handleIAMIdentityUnlink(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	fields, err := identityRequestFields("UnlinkIdentity", "struct{TenantID, UserID, ProviderID string}", req,
		"TenantID", "UserID", "ProviderID")
	if err != nil {
		return nil, err
	}
	mgr, err := federationManagerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	tenant, userID, err := identityTarget(ctx, "UnlinkIdentity", fields[0], fields[1])
	if err != nil {
		return nil, err
	}
	if err := mgr.UnlinkAccount(ctx, userID, tenant, fields[2]); err != nil {
		return nil, err
	}
	return map[string]bool{"success": true}, nil
}

func handleIAMIdentityListUserIdentities(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	fields, err := identityRequestFields("ListUserIdentities", "struct{TenantID, UserID string}", req,
		"TenantID", "UserID")
	if err != nil {
		return nil, err
	}
	mgr, err := federationManagerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	tenant, userID, err := identityTarget(ctx, "ListUserIdentities", fields[0], fields[1])
	if err != nil {
		return nil, err
	}
	links, err := mgr.GetUserLinks(ctx, userID, tenant)
	if err != nil {
		return nil, err
	}
	out := make([]*cli.FederatedIdentity, 0, len(links))
	for _, link := range links {
		out = append(out, federatedIdentityToSDK(link))
	}
	return out, nil
}

func federatedIdentityToSDK(link *iam.FederatedIdentity) *cli.FederatedIdentity {
	return &cli.FederatedIdentity{
		ID:            link.ID,
		UserID:        strconv.FormatUint(link.UserID, 10),
		TenantID:      link.TenantID,
		ProviderID:    link.ProviderID,
		ExternalID:    link.ExternalID,
		ExternalEmail: link.ExternalEmail,
		ExternalName:  link.ExternalName,
		Created:       link.Created,
		LastLogin:     link.LastLogin,
	}
}
