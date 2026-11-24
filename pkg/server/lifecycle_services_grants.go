package server

import (
	"context"
	"errors"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/registry"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane/grants"
)

// errServicePlaneGrants is the sentinel a ServicePlaneGrantsError unwraps
// to: a node dependency the deploy grants need is not up yet. The lazy
// resolver retries on the next call, as it does a platform shard that is
// not up.
var errServicePlaneGrants = errors.New("service plane: the node cannot build the deploy grants yet")

// ServicePlaneGrantsError reports the node dependency the deploy grants
// are missing.
type ServicePlaneGrantsError struct {
	Missing string
}

// Error implements error.
func (e *ServicePlaneGrantsError) Error() string {
	return "service plane: the node cannot build the deploy grants: no " + e.Missing
}

// Unwrap returns errServicePlaneGrants.
func (e *ServicePlaneGrantsError) Unwrap() error {
	return errServicePlaneGrants
}

// servicePlaneGrants returns the grants the plane runs. An application
// that supplies its own keeps them; otherwise the node builds the five
// (grants.All) over its go-xkms CA, secrets manager, the platform shard's
// registry and iam.WorkloadRoleBinder over its IAM identity store, with
// names and certificates under rt.Domain (default: the SPIFFE trust domain).
func (s *Server) servicePlaneGrants(ctx context.Context, rt service.ServicePlaneRuntime, client *qrdbsdk.Client) ([]serviceplane.Grant, error) {
	if len(rt.Grants) > 0 {
		return rt.Grants, nil
	}
	// IAM attaches after the plane's recovery loop starts; iamReady is
	// stored after s.iamComponents, so loading it first orders the reads.
	if !s.iamReady.Load() {
		return nil, &ServicePlaneGrantsError{Missing: "IAM identity store"}
	}
	if s.xkmsCA == nil {
		return nil, &ServicePlaneGrantsError{Missing: "certificate authority"}
	}
	if s.secretsManager == nil {
		return nil, &ServicePlaneGrantsError{Missing: "secrets manager"}
	}
	if s.iamComponents == nil || s.iamComponents.GetIdentityStore() == nil {
		return nil, &ServicePlaneGrantsError{Missing: "IAM identity store"}
	}
	store := s.iamComponents.GetIdentityStore()
	reg, err := registry.NewFromClient(ctx, client)
	if err != nil {
		return nil, err
	}
	trustDomain := s.config.IAM.SPIFFETrustDomain
	domain := rt.Domain
	if domain == "" {
		domain = trustDomain
	}
	return grants.All(grants.Config{
		CA:          s.xkmsCA,
		Secrets:     s.secretsManager,
		Registry:    reg,
		Roles:       &iam.WorkloadRoleBinder{Store: store},
		Domain:      domain,
		TrustDomain: trustDomain,
	})
}
