package grpc

import (
	"bytes"
	"context"
	"testing"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"google.golang.org/grpc/codes"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// unreachableCeremonyClient satisfies transport.CeremonyClient for a real
// CeremonyService whose request validation must refuse before any client
// call; reaching a method panics on the nil embedded interface.
type unreachableCeremonyClient struct {
	transport.CeremonyClient
}

// TestCeremonyInitialize_ForwardsAdminSelector proves every admin credential
// selector field (proto fields 8-12) reaches the service request unchanged.
func TestCeremonyInitialize_ForwardsAdminSelector(t *testing.T) {
	var gotReq *service.CeremonyInitRequest
	var gotSecret []byte
	srv := newTestCeremonyServer(&mockCeremonyService{
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
	})

	_, err := srv.CeremonyInitialize(context.Background(), &pb.CeremonyInitializeRequest{
		SoPin:            "1234",
		AdminCsrPem:      "csr",
		AdminCertPem:     "cert",
		AdminCertPoolPem: "pool",
		AdminGenerate:    true,
		Secret:           []byte("secret"),
	})
	if err != nil {
		t.Fatalf("CeremonyInitialize: %v", err)
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

// TestCeremonyInitialize_NoAdminSelector proves a request without an admin
// credential selector comes back as InvalidArgument from the real service's
// validation, before any go-qrdb client call.
func TestCeremonyInitialize_NoAdminSelector(t *testing.T) {
	srv := newTestCeremonyServer(service.NewCeremonyService(&unreachableCeremonyClient{}))
	_, err := srv.CeremonyInitialize(context.Background(), &pb.CeremonyInitializeRequest{SoPin: "1234"})
	assertGRPCCode(t, err, codes.InvalidArgument)
}
