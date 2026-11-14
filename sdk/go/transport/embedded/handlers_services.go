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
	"encoding/json"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane"
	servicesclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/services"
)

// The service plane on the in-process embedded transport: the handlers call
// the same ServicesService every wire transport serves.

const servicesService = servicesclient.ServiceName

func servicesOf(svc service.QuicBaaS) (service.ServicesService, error) {
	s := svc.Services()
	if s == nil {
		return nil, NewServiceNotConfiguredError("Services")
	}
	return s, nil
}

// servicesManifestHandler serves Deploy or Redeploy.
func servicesManifestHandler(method string, verb func(service.ServicesService, context.Context, *service.DeployServiceRequest) (*service.ServiceRecord, error)) handlerFunc {
	return func(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
		s, err := servicesOf(svc)
		if err != nil {
			return nil, err
		}
		r, ok := req.(*servicesclient.DeployRequest)
		if !ok || r == nil {
			return nil, NewInvalidRequestTypeError(servicesService, method, "*services.DeployRequest", typeNameOf(req))
		}
		var signed *serviceplane.SignedManifest
		if r.Manifest != nil {
			signed = &serviceplane.SignedManifest{Payload: r.Manifest.Payload, KeyID: r.Manifest.KeyID, Signature: r.Manifest.Signature}
		}
		rec, err := verb(s, ctx, &service.DeployServiceRequest{Manifest: signed})
		if err != nil {
			return nil, err
		}
		return servicesRecord(rec)
	}
}

// servicesNameHandler serves a verb or read that addresses a service by name.
func servicesNameHandler(method string, verb func(service.ServicesService, context.Context, *service.ServiceNameRequest) (*service.ServiceRecord, error)) handlerFunc {
	return func(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
		s, err := servicesOf(svc)
		if err != nil {
			return nil, err
		}
		r, ok := req.(*servicesclient.NameRequest)
		if !ok || r == nil {
			return nil, NewInvalidRequestTypeError(servicesService, method, "*services.NameRequest", typeNameOf(req))
		}
		rec, err := verb(s, ctx, &service.ServiceNameRequest{Name: r.Name})
		if err != nil {
			return nil, err
		}
		return servicesRecord(rec)
	}
}

func handleServicesUndeploy(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	s, err := servicesOf(svc)
	if err != nil {
		return nil, err
	}
	r, ok := req.(*servicesclient.NameRequest)
	if !ok || r == nil {
		return nil, NewInvalidRequestTypeError(servicesService, "Undeploy", "*services.NameRequest", typeNameOf(req))
	}
	return nil, s.Undeploy(ctx, &service.ServiceNameRequest{Name: r.Name})
}

func handleServicesList(ctx context.Context, svc service.QuicBaaS, _ interface{}) (interface{}, error) {
	s, err := servicesOf(svc)
	if err != nil {
		return nil, err
	}
	resp, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	out := &servicesclient.ListResponse{Services: make([]servicesclient.Record, 0, len(resp.Services))}
	for _, rec := range resp.Services {
		r, err := servicesRecord(rec)
		if err != nil {
			return nil, err
		}
		out.Services = append(out.Services, *r)
	}
	return out, nil
}

// servicesRecord converts a service record to the SDK's.
func servicesRecord(rec *service.ServiceRecord) (*servicesclient.Record, error) {
	out := &servicesclient.Record{
		Tenant: rec.Tenant, Name: rec.Name, State: rec.State, Verb: rec.Verb,
		Revision: rec.Revision, Restarts: rec.Restarts, Grants: rec.Grants,
		Reason: rec.Reason, UpdatedAt: rec.UpdatedAt, KeyID: rec.KeyID,
	}
	if rec.Manifest != nil {
		raw, err := json.Marshal(rec.Manifest)
		if err != nil {
			return nil, err
		}
		out.Manifest = raw
	}
	return out, nil
}
