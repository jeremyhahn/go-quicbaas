package server

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane/serviceplanetest"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/xkmsca"
)

// grantsTestServer is a bare server with the trust domain set and every
// node dependency the grants need left nil.
func grantsTestServer(t *testing.T) *Server {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.IAM.SPIFFETrustDomain = "svc.example.test"
	return &Server{logger: log, config: cfg}
}

// requireGrantsNotReady asserts err is the typed not-ready error naming
// missing.
func requireGrantsNotReady(t *testing.T, err error, missing string) {
	t.Helper()
	if !errors.Is(err, errServicePlaneGrants) {
		t.Fatalf("err = %v, want errServicePlaneGrants", err)
	}
	var notReady *ServicePlaneGrantsError
	if !errors.As(err, &notReady) {
		t.Fatalf("err = %T, want *ServicePlaneGrantsError", err)
	}
	if notReady.Missing != missing {
		t.Fatalf("Missing = %q, want %q", notReady.Missing, missing)
	}
}

func TestServicePlaneGrants_AppSuppliedGrantsAreKept(t *testing.T) {
	srv := grantsTestServer(t)
	supplied := serviceplanetest.Grants()
	got, err := srv.servicePlaneGrants(context.Background(), service.ServicePlaneRuntime{Grants: supplied}, nil)
	if err != nil {
		t.Fatalf("servicePlaneGrants: %v", err)
	}
	if len(got) != len(supplied) {
		t.Fatalf("got %d grants, want %d", len(got), len(supplied))
	}
	for i := range supplied {
		if got[i] != supplied[i] {
			t.Fatalf("grant %d = %v, want the supplied %v", i, got[i], supplied[i])
		}
	}
}

func TestServicePlaneGrants_NoCAIsNotReady(t *testing.T) {
	srv := grantsTestServer(t)
	_, err := srv.servicePlaneGrants(context.Background(), service.ServicePlaneRuntime{}, nil)
	requireGrantsNotReady(t, err, "certificate authority")
}

func TestServicePlaneGrants_NoSecretsIsNotReady(t *testing.T) {
	srv := grantsTestServer(t)
	srv.xkmsCA = xkmsca.New(t, "service plane grants "+t.Name())
	_, err := srv.servicePlaneGrants(context.Background(), service.ServicePlaneRuntime{}, nil)
	requireGrantsNotReady(t, err, "secrets manager")
}

func TestServicePlaneGrants_NoIdentityStoreIsNotReady(t *testing.T) {
	srv := grantsTestServer(t)
	srv.xkmsCA = xkmsca.New(t, "service plane grants "+t.Name())
	srv.secretsManager = &secrets.Manager{}

	_, err := srv.servicePlaneGrants(context.Background(), service.ServicePlaneRuntime{}, nil)
	requireGrantsNotReady(t, err, "IAM identity store")

	srv.iamComponents = &iamComponents{}
	_, err = srv.servicePlaneGrants(context.Background(), service.ServicePlaneRuntime{}, nil)
	requireGrantsNotReady(t, err, "IAM identity store")
}

func TestServicePlaneGrantsError_Message(t *testing.T) {
	err := &ServicePlaneGrantsError{Missing: "secrets manager"}
	want := "service plane: the node cannot build the deploy grants: no secrets manager"
	if err.Error() != want {
		t.Fatalf("Error() = %q, want %q", err.Error(), want)
	}
}
