//go:build rest

package rest

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// unreachableCeremonyClient satisfies transport.CeremonyClient for a real
// CeremonyService whose request validation must refuse before any client
// call; reaching a method panics on the nil embedded interface.
type unreachableCeremonyClient struct {
	transport.CeremonyClient
}

// TestHandleCeremonyInitialize_ForwardsAdminSelector proves every admin
// credential selector field in the REST body reaches the service request
// unchanged.
func TestHandleCeremonyInitialize_ForwardsAdminSelector(t *testing.T) {
	var gotReq *service.CeremonyInitRequest
	var gotSecret []byte
	svc := &fakeCeremonyService{
		initFn: func(ctx context.Context, req *service.CeremonyInitRequest) (*service.CeremonyInitInfo, error) {
			gotReq = req
			// Copy the plaintext while the sealed secret is still alive.
			if err := req.Secret.WithBytes(ctx, func(b []byte) error {
				gotSecret = append([]byte(nil), b...)
				return nil
			}); err != nil {
				return nil, err
			}
			return &service.CeremonyInitInfo{State: "initialized"}, nil
		},
	}
	// "c2VjcmV0" is base64 for "secret": encoding/json carries []byte as base64.
	body := `{"so_pin":"1234","admin_csr_pem":"csr","admin_cert_pem":"cert",` +
		`"admin_cert_pool_pem":"pool","admin_generate":true,"secret":"c2VjcmV0"}`
	w := doCeremonyReq(t, ceremonyServer(t, svc), http.MethodPost, "/api/v1/init/initialize", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if gotReq == nil {
		t.Fatal("service was not called")
	}
	if gotReq.AdminCSRPEM != "csr" || gotReq.AdminCertPEM != "cert" ||
		gotReq.AdminCertPoolPEM != "pool" || !gotReq.AdminGenerate {
		t.Fatalf("admin selector not forwarded: %+v", gotReq)
	}
	if !bytes.Equal(gotSecret, []byte("secret")) {
		t.Fatalf("secret not forwarded: %q", gotSecret)
	}
}

// TestHandleCeremonyInitialize_NoAdminSelector proves a body without an
// admin credential selector comes back as 400 from the real service's
// validation, before any go-qrdb client call.
func TestHandleCeremonyInitialize_NoAdminSelector(t *testing.T) {
	svc := service.NewCeremonyService(&unreachableCeremonyClient{})
	w := doCeremonyReq(t, ceremonyServer(t, svc), http.MethodPost, "/api/v1/init/initialize", `{"so_pin":"1234"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}
