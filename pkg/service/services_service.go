// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package service

import (
	"context"
	stderrors "errors"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// ServicesService serves the service plane's seven lifecycle verbs (deploy,
// undeploy, redeploy, start, stop, resume, restart) and its two reads to
// every transport. Every verb acts in the caller's tenant: a manifest that
// names another tenant is refused, and the name verbs address the caller's
// own services only.
type ServicesService interface {
	Deploy(ctx context.Context, req *DeployServiceRequest) (*ServiceRecord, error)
	Redeploy(ctx context.Context, req *DeployServiceRequest) (*ServiceRecord, error)
	Undeploy(ctx context.Context, req *ServiceNameRequest) error
	Start(ctx context.Context, req *ServiceNameRequest) (*ServiceRecord, error)
	Stop(ctx context.Context, req *ServiceNameRequest) (*ServiceRecord, error)
	Resume(ctx context.Context, req *ServiceNameRequest) (*ServiceRecord, error)
	Restart(ctx context.Context, req *ServiceNameRequest) (*ServiceRecord, error)
	Get(ctx context.Context, req *ServiceNameRequest) (*ServiceRecord, error)
	List(ctx context.Context) (*ListServicesResponse, error)
}

// DeployServiceRequest carries a signed manifest to Deploy or Redeploy.
type DeployServiceRequest struct {
	Manifest *serviceplane.SignedManifest `json:"manifest"`
}

// ServiceNameRequest names one of the caller tenant's services.
type ServiceNameRequest struct {
	Name string `json:"name"`
}

// ServiceRecord is a service as every transport answers it: its lifecycle
// state, the manifest it runs (decoded from the signed payload) and the
// references its grants returned.
type ServiceRecord struct {
	Tenant    string                 `json:"tenant"`
	Name      string                 `json:"name"`
	State     string                 `json:"state"`
	Verb      string                 `json:"verb,omitempty"`
	Revision  int64                  `json:"revision"`
	Restarts  int64                  `json:"restarts"`
	Grants    map[string]string      `json:"grants,omitempty"`
	Reason    string                 `json:"reason,omitempty"`
	UpdatedAt time.Time              `json:"updated_at"`
	KeyID     string                 `json:"key_id"`
	Manifest  *serviceplane.Manifest `json:"manifest"`
}

// ListServicesResponse lists the caller tenant's services by name.
type ListServicesResponse struct {
	Services []*ServiceRecord `json:"services"`
}

// ServicePlaneRuntime is what an application supplies for the service plane
// to act: the launcher that runs a verified manifest, one grant per grant
// kind, and the verifier that holds the manifest signing keys. The store
// comes from the node (go-qrdb through cas.Writer).
type ServicePlaneRuntime struct {
	Launcher serviceplane.Launcher
	Grants   []serviceplane.Grant
	Verifier serviceplane.Verifier
}

// ServicePlaneResolver answers the node's plane, built on first use, or why
// it cannot be built yet.
type ServicePlaneResolver func(ctx context.Context) (*serviceplane.Plane, error)

// ErrServicePlaneNotConfigured reports a node with no ServicePlaneRuntime.
var ErrServicePlaneNotConfigured = errors.New(errors.ErrPreconditionFailed, "ServicesService",
	"the service plane is not configured on this node: it needs a launcher, the grants and a manifest keyring")

// ErrServicesNilRequest reports a verb called without its request.
var ErrServicesNilRequest = errors.New(errors.ErrInvalidInput, "ServicesService", "request is required")

// ErrServicesTenant reports a manifest that names a tenant other than the
// caller's, or a caller with no tenant of its own.
var ErrServicesTenant = errors.New(errors.ErrForbidden, "ServicesService",
	"a service is deployed and addressed in the caller's own tenant only")

type servicesService struct {
	resolve ServicePlaneResolver
}

// NewServicesService serves the verbs over resolve. A nil resolve answers
// ErrServicePlaneNotConfigured on every call.
func NewServicesService(resolve ServicePlaneResolver) ServicesService {
	return &servicesService{resolve: resolve}
}

func (s *servicesService) plane(ctx context.Context) (*serviceplane.Plane, string, error) {
	if s.resolve == nil {
		return nil, "", ErrServicePlaneNotConfigured
	}
	tenant, err := tenantctx.FromContext(ctx, "services")
	if err != nil {
		return nil, "", err
	}
	if tenant == "" || tenantctx.IsSystem(tenant) {
		return nil, "", ErrServicesTenant
	}
	p, err := s.resolve(ctx)
	if err != nil {
		return nil, "", servicesError("ServicesService.resolve", err)
	}
	return p, tenant, nil
}

func (s *servicesService) Deploy(ctx context.Context, req *DeployServiceRequest) (*ServiceRecord, error) {
	return s.deploy(ctx, req, serviceplane.VerbDeploy)
}

func (s *servicesService) Redeploy(ctx context.Context, req *DeployServiceRequest) (*ServiceRecord, error) {
	return s.deploy(ctx, req, serviceplane.VerbRedeploy)
}

func (s *servicesService) deploy(ctx context.Context, req *DeployServiceRequest, verb serviceplane.Verb) (*ServiceRecord, error) {
	if req == nil || req.Manifest == nil {
		return nil, ErrServicesNilRequest
	}
	p, tenant, err := s.plane(ctx)
	if err != nil {
		return nil, err
	}
	// The plane checks the caller's tenant against the verified payload.
	act := p.Deploy
	if verb == serviceplane.VerbRedeploy {
		act = p.Redeploy
	}
	svc, err := act(ctx, tenant, req.Manifest)
	if err != nil {
		return nil, servicesError("ServicesService."+string(verb), err)
	}
	return serviceRecord(svc), nil
}

func (s *servicesService) Undeploy(ctx context.Context, req *ServiceNameRequest) error {
	if req == nil {
		return ErrServicesNilRequest
	}
	p, tenant, err := s.plane(ctx)
	if err != nil {
		return err
	}
	if err := p.Undeploy(ctx, tenant, req.Name); err != nil {
		return servicesError("ServicesService.undeploy", err)
	}
	return nil
}

func (s *servicesService) Start(ctx context.Context, req *ServiceNameRequest) (*ServiceRecord, error) {
	return s.byName(ctx, req, "start", func(p *serviceplane.Plane) nameVerb { return p.Start })
}

func (s *servicesService) Stop(ctx context.Context, req *ServiceNameRequest) (*ServiceRecord, error) {
	return s.byName(ctx, req, "stop", func(p *serviceplane.Plane) nameVerb { return p.Stop })
}

func (s *servicesService) Resume(ctx context.Context, req *ServiceNameRequest) (*ServiceRecord, error) {
	return s.byName(ctx, req, "resume", func(p *serviceplane.Plane) nameVerb { return p.Resume })
}

func (s *servicesService) Restart(ctx context.Context, req *ServiceNameRequest) (*ServiceRecord, error) {
	return s.byName(ctx, req, "restart", func(p *serviceplane.Plane) nameVerb { return p.Restart })
}

func (s *servicesService) Get(ctx context.Context, req *ServiceNameRequest) (*ServiceRecord, error) {
	return s.byName(ctx, req, "get", func(p *serviceplane.Plane) nameVerb { return p.Get })
}

type nameVerb func(ctx context.Context, tenant, name string) (*serviceplane.Service, error)

func (s *servicesService) byName(ctx context.Context, req *ServiceNameRequest, op string, pick func(*serviceplane.Plane) nameVerb) (*ServiceRecord, error) {
	if req == nil {
		return nil, ErrServicesNilRequest
	}
	p, tenant, err := s.plane(ctx)
	if err != nil {
		return nil, err
	}
	svc, err := pick(p)(ctx, tenant, req.Name)
	if err != nil {
		return nil, servicesError("ServicesService."+op, err)
	}
	return serviceRecord(svc), nil
}

func (s *servicesService) List(ctx context.Context) (*ListServicesResponse, error) {
	p, tenant, err := s.plane(ctx)
	if err != nil {
		return nil, err
	}
	svcs, err := p.List(ctx, tenant)
	if err != nil {
		return nil, servicesError("ServicesService.list", err)
	}
	out := &ListServicesResponse{Services: make([]*ServiceRecord, 0, len(svcs))}
	for _, svc := range svcs {
		out.Services = append(out.Services, serviceRecord(svc))
	}
	return out, nil
}

func serviceRecord(svc *serviceplane.Service) *ServiceRecord {
	if svc == nil {
		return nil
	}
	m := svc.Manifest
	rec := &ServiceRecord{
		Tenant:    svc.Tenant,
		Name:      svc.Name,
		State:     string(svc.State),
		Verb:      string(svc.Verb),
		Revision:  svc.Revision,
		Restarts:  svc.Restarts,
		Reason:    svc.Reason,
		UpdatedAt: svc.UpdatedAt,
		KeyID:     svc.Signed.KeyID,
		Manifest:  &m,
	}
	if len(svc.Grants) > 0 {
		rec.Grants = make(map[string]string, len(svc.Grants))
		for k, v := range svc.Grants {
			rec.Grants[string(k)] = v
		}
	}
	return rec
}

// servicesErrorCode maps the service plane's sentinels onto the status code
// every transport answers.
func servicesErrorCode(err error) errors.ErrorCode {
	switch {
	case stderrors.Is(err, serviceplane.ErrInvalidManifest):
		return errors.ErrInvalidInput
	case stderrors.Is(err, serviceplane.ErrSignature), stderrors.Is(err, serviceplane.ErrTenant):
		return errors.ErrForbidden
	case stderrors.Is(err, serviceplane.ErrNotFound):
		return errors.ErrNotFound
	case stderrors.Is(err, serviceplane.ErrExists):
		return errors.ErrAlreadyExists
	case stderrors.Is(err, serviceplane.ErrConflict):
		return errors.ErrConflict
	case stderrors.Is(err, serviceplane.ErrTransition):
		return errors.ErrPreconditionFailed
	case stderrors.Is(err, serviceplane.ErrConfig):
		return errors.ErrPreconditionFailed
	default:
		// ErrGrant, ErrLaunch and ErrStore: a dependency of the plane failed.
		return errors.ErrUnavailable
	}
}

// servicesError codes a plane failure. A failure that is already coded and
// did not come from the plane (a resolver's typed error) keeps its code.
func servicesError(op string, err error) error {
	var plane *serviceplane.ServiceError
	var qb *errors.QBError
	if !stderrors.As(err, &plane) && stderrors.As(err, &qb) {
		return err
	}
	return &errors.QBError{Code: servicesErrorCode(err), Op: op, Err: err}
}
