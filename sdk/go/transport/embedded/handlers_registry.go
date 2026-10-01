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
	stderrors "errors"

	"github.com/jeremyhahn/go-quicbaas/pkg/registry"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	registryclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/registry"
)

// The service registry on the in-process embedded transport. The handlers
// call the service layer directly — the same RegistryService every wire
// transport serves — so an embedded caller gets identical tenant scoping
// (from the context it passes) and identical errors.

const registryService = registryclient.ServiceName

// registryOf returns the registry service, or the not-configured error.
func registryOf(svc service.QuicBaaS) (service.RegistryService, error) {
	reg := svc.Registry()
	if reg == nil {
		return nil, NewServiceNotConfiguredError("Registry")
	}
	return reg, nil
}

func handleRegistryRegister(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	reg, err := registryOf(svc)
	if err != nil {
		return nil, err
	}
	r, ok := req.(*registryclient.RegisterRequest)
	if !ok || r == nil {
		return nil, NewInvalidRequestTypeError(registryService, "Register", "*registry.RegisterRequest", typeNameOf(req))
	}
	resp, err := reg.Register(ctx, &service.RegisterServiceInstanceRequest{
		Instance:   service.RegistryServiceInstance(r.Instance),
		TTLSeconds: r.TTLSeconds,
	})
	if err != nil {
		return nil, err
	}
	return &registryclient.RegisterResponse{LeaseID: resp.LeaseID, TTLSeconds: resp.TTLSeconds}, nil
}

func handleRegistryRenew(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	reg, err := registryOf(svc)
	if err != nil {
		return nil, err
	}
	r, ok := req.(*registryclient.RenewRequest)
	if !ok || r == nil {
		return nil, NewInvalidRequestTypeError(registryService, "Renew", "*registry.RenewRequest", typeNameOf(req))
	}
	resp, err := reg.Renew(ctx, &service.RenewServiceLeaseRequest{LeaseID: r.LeaseID})
	if err != nil {
		return nil, err
	}
	return &registryclient.RenewResponse{LeaseID: resp.LeaseID, TTLSeconds: resp.TTLSeconds}, nil
}

func handleRegistryDeregister(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	reg, err := registryOf(svc)
	if err != nil {
		return nil, err
	}
	r, ok := req.(*registryclient.DeregisterRequest)
	if !ok || r == nil {
		return nil, NewInvalidRequestTypeError(registryService, "Deregister", "*registry.DeregisterRequest", typeNameOf(req))
	}
	return nil, reg.Deregister(ctx, &service.DeregisterServiceInstanceRequest{LeaseID: r.LeaseID})
}

func handleRegistryResolve(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	reg, err := registryOf(svc)
	if err != nil {
		return nil, err
	}
	r, ok := req.(*registryclient.ResolveRequest)
	if !ok || r == nil {
		return nil, NewInvalidRequestTypeError(registryService, "Resolve", "*registry.ResolveRequest", typeNameOf(req))
	}
	resp, err := reg.Resolve(ctx, &service.ResolveServiceRequest{Service: r.Service})
	if err != nil {
		return nil, err
	}
	out := &registryclient.ResolveResponse{
		Instances: make([]registryclient.ServiceInstance, 0, len(resp.Instances)),
		Revision:  resp.Revision,
	}
	for _, inst := range resp.Instances {
		out.Instances = append(out.Instances, registryclient.ServiceInstance(inst))
	}
	return out, nil
}

// registryEventTypes maps service event types to SDK event types.
var registryEventTypes = map[service.RegistryEventType]registryclient.EventType{
	service.RegistryEventPut:    registryclient.EventPut,
	service.RegistryEventDelete: registryclient.EventDelete,
}

// handleRegistryWatchStream forwards a service-layer watch onto the embedded
// stream as SDK events. A store-ended watch is returned as the SDK's typed
// error, which the transport hands to the client as the stream's last
// message.
func handleRegistryWatchStream(ctx context.Context, svc service.QuicBaaS, req interface{}, s *stream) error {
	reg, err := registryOf(svc)
	if err != nil {
		return err
	}
	r, ok := req.(*registryclient.WatchRequest)
	if !ok || r == nil {
		return NewInvalidRequestTypeError(registryService, "Watch", "*registry.WatchRequest", typeNameOf(req))
	}
	events, err := reg.Watch(ctx, &service.WatchServiceRequest{Service: r.Service, FromRevision: r.FromRevision})
	if err != nil {
		return registryWatchError(r, err)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.context().Done():
			return s.context().Err()
		case ev, open := <-events:
			if !open {
				return nil
			}
			if ev.Err != nil {
				return registryWatchError(r, ev.Err)
			}
			out := &registryclient.Event{
				Type:     registryEventTypes[ev.Type],
				Instance: registryclient.ServiceInstance(ev.Instance),
				Revision: ev.Revision,
			}
			select {
			case s.recvChan() <- out:
			case <-ctx.Done():
				return ctx.Err()
			case <-s.context().Done():
				return s.context().Err()
			}
		}
	}
}

// registryWatchError maps the registry's watch endings to the SDK's typed
// errors; any other failure passes through.
func registryWatchError(req *registryclient.WatchRequest, err error) error {
	var compacted *registry.WatchCompactedError
	if stderrors.As(err, &compacted) {
		return &registryclient.WatchCompactedError{Service: req.Service, FromRevision: req.FromRevision, Message: compacted.Error()}
	}
	var ended *registry.WatchEndedError
	if stderrors.As(err, &ended) {
		return &registryclient.WatchEndedError{Service: req.Service, Message: ended.Error()}
	}
	return err
}
