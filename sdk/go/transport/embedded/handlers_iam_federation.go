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
	"fmt"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

const federationServiceName = "quicbaas.IAM.Federation"

// federationListLimit is the page size the gRPC server applies when a
// ListProviders request names none; the SDK client never names one.
const federationListLimit = 100

// iamFederationUnaryHandlers routes the SDK IAM client's identity-provider
// verbs onto the iam.FederationManager the REST and gRPC servers call. The
// block is kept apart from iamUnaryHandlers so it merges independently.
var iamFederationUnaryHandlers = map[string]handlerFunc{
	federationServiceName + "/CreateProvider": handleIAMFederationCreateProvider,
	federationServiceName + "/GetProvider":    handleIAMFederationGetProvider,
	federationServiceName + "/UpdateProvider": handleIAMFederationUpdateProvider,
	federationServiceName + "/DeleteProvider": handleIAMFederationDeleteProvider,
	federationServiceName + "/ListProviders":  handleIAMFederationListProviders,
}

func init() {
	for k, v := range iamFederationUnaryHandlers {
		unaryHandlers[k] = v
	}
}

// iamFederationServiceName names the federation verbs in a
// not-configured error.
const iamFederationServiceName = "IAM.Federation"

// federationTarget applies the tenant check the gRPC server makes for every
// federation verb: the request may name only the tenant the call is bound to.
func federationTarget(ctx context.Context, method, requested string) (string, error) {
	return tenantctx.Target(ctx, federationServiceName+"/"+method, requested)
}

// federationRequestField reads a string field from a request the SDK client
// sends as an unexported struct. A request without the field is the wrong type.
func federationRequestField(req interface{}, method, name string) (string, error) {
	value, ok := iamRequestField(req, name)
	if !ok {
		return "", NewInvalidRequestTypeError(federationServiceName, method,
			"struct with "+name, fmt.Sprintf("%T", req))
	}
	return value, nil
}

func handleIAMFederationCreateProvider(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*cli.CreateProviderOptions)
	if !ok || r == nil {
		return nil, NewInvalidRequestTypeError(federationServiceName, "CreateProvider",
			"*cli.CreateProviderOptions", fmt.Sprintf("%T", req))
	}
	tenantID, err := federationTarget(ctx, "CreateProvider", r.TenantID)
	if err != nil {
		return nil, err
	}
	mgr, err := federationManagerFor(ctx, svc, iamFederationServiceName)
	if err != nil {
		return nil, err
	}
	provider := &iam.ExternalProvider{
		ID:            r.ProviderID,
		TenantID:      tenantID,
		Name:          r.Name,
		Type:          iam.ProviderType(r.Type),
		ClientID:      r.ClientID,
		ClientSecret:  r.ClientSecret,
		IssuerURL:     r.IssuerURL,
		Scopes:        r.Scopes,
		AutoProvision: r.AutoProvision,
		AllowLinking:  r.AllowLinking,
		DefaultRoles:  r.DefaultRoles,
	}
	if err := mgr.RegisterProvider(ctx, provider); err != nil {
		return nil, err
	}
	return toCLIProvider(provider), nil
}

func handleIAMFederationGetProvider(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	tenantID, providerID, mgr, err := federationProviderCall(ctx, svc, "GetProvider", req)
	if err != nil {
		return nil, err
	}
	provider, err := mgr.GetProvider(ctx, tenantID, providerID)
	if err != nil {
		return nil, err
	}
	return toCLIProvider(provider), nil
}

// handleIAMFederationUpdateProvider follows the gRPC server's partial update:
// read the stored provider, apply the fields the request sets, store it and
// return the stored result.
func handleIAMFederationUpdateProvider(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*cli.UpdateProviderOptions)
	if !ok || r == nil {
		return nil, NewInvalidRequestTypeError(federationServiceName, "UpdateProvider",
			"*cli.UpdateProviderOptions", fmt.Sprintf("%T", req))
	}
	tenantID, err := federationTarget(ctx, "UpdateProvider", r.TenantID)
	if err != nil {
		return nil, err
	}
	mgr, err := federationManagerFor(ctx, svc, iamFederationServiceName)
	if err != nil {
		return nil, err
	}
	existing, err := mgr.GetProvider(ctx, tenantID, r.ProviderID)
	if err != nil {
		return nil, err
	}
	applyProviderUpdate(existing, r)
	if err := mgr.UpdateProvider(ctx, existing); err != nil {
		return nil, err
	}
	updated, err := mgr.GetProvider(ctx, tenantID, r.ProviderID)
	if err != nil {
		return nil, err
	}
	return toCLIProvider(updated), nil
}

func handleIAMFederationDeleteProvider(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	tenantID, providerID, mgr, err := federationProviderCall(ctx, svc, "DeleteProvider", req)
	if err != nil {
		return nil, err
	}
	if err := mgr.DeleteProvider(ctx, tenantID, providerID); err != nil {
		return nil, err
	}
	return nil, nil
}

func handleIAMFederationListProviders(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	requested, err := federationRequestField(req, "ListProviders", "TenantID")
	if err != nil {
		return nil, err
	}
	tenantID, err := federationTarget(ctx, "ListProviders", requested)
	if err != nil {
		return nil, err
	}
	mgr, err := federationManagerFor(ctx, svc, iamFederationServiceName)
	if err != nil {
		return nil, err
	}
	providers, err := mgr.ListProviders(ctx, tenantID, iam.ListOptions{Limit: federationListLimit})
	if err != nil {
		return nil, err
	}
	out := make([]*cli.Provider, 0, len(providers))
	for _, p := range providers {
		out = append(out, toCLIProvider(p))
	}
	return out, nil
}

// federationProviderCall resolves the tenant, provider id and manager for a
// verb whose request names one provider of one tenant.
func federationProviderCall(ctx context.Context, svc service.QuicBaaS, method string, req interface{}) (string, string, iam.FederationManager, error) {
	requested, err := federationRequestField(req, method, "TenantID")
	if err != nil {
		return "", "", nil, err
	}
	providerID, err := federationRequestField(req, method, "ProviderID")
	if err != nil {
		return "", "", nil, err
	}
	tenantID, err := federationTarget(ctx, method, requested)
	if err != nil {
		return "", "", nil, err
	}
	mgr, err := federationManagerFor(ctx, svc, iamFederationServiceName)
	if err != nil {
		return "", "", nil, err
	}
	return tenantID, providerID, mgr, nil
}

// applyProviderUpdate copies the fields an update request sets onto the
// stored provider.
func applyProviderUpdate(p *iam.ExternalProvider, r *cli.UpdateProviderOptions) {
	if r.Name != nil {
		p.Name = *r.Name
	}
	if r.Enabled != nil {
		p.Enabled = *r.Enabled
	}
	if r.ClientID != nil {
		p.ClientID = *r.ClientID
	}
	if r.ClientSecret != nil {
		p.ClientSecret = *r.ClientSecret
	}
	if r.IssuerURL != nil {
		p.IssuerURL = *r.IssuerURL
	}
	if r.Scopes != nil {
		p.Scopes = r.Scopes
	}
	if r.AutoProvision != nil {
		p.AutoProvision = *r.AutoProvision
	}
	if r.AllowLinking != nil {
		p.AllowLinking = *r.AllowLinking
	}
	if r.DefaultRoles != nil {
		p.DefaultRoles = r.DefaultRoles
	}
}

// toCLIProvider converts a stored provider into the shape the SDK client
// decodes. The client secret is never returned.
func toCLIProvider(p *iam.ExternalProvider) *cli.Provider {
	if p == nil {
		return nil
	}
	return &cli.Provider{
		ID:            p.ID,
		TenantID:      p.TenantID,
		Name:          p.Name,
		Type:          string(p.Type),
		Enabled:       p.Enabled,
		ClientID:      p.ClientID,
		IssuerURL:     p.IssuerURL,
		Scopes:        p.Scopes,
		AutoProvision: p.AutoProvision,
		AllowLinking:  p.AllowLinking,
		DefaultRoles:  p.DefaultRoles,
		Created:       p.Created,
		Updated:       p.Updated,
	}
}
