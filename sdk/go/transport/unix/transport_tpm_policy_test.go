package unix

import (
	"context"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	xkmspb "github.com/jeremyhahn/go-xkms/pkg/api/grpc/proto/xkmsv1"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// tpmPolicyListServer records each xkms.v1 TPMPolicyList call.
type tpmPolicyListServer struct {
	xkmspb.UnimplementedKeystoreServiceServer

	mu      sync.Mutex
	tenants []string
}

func (s *tpmPolicyListServer) TPMPolicyList(_ context.Context, req *xkmspb.TPMPolicyListRequest) (*xkmspb.TPMPolicyListResponse, error) {
	s.mu.Lock()
	s.tenants = append(s.tenants, req.GetTenantId())
	s.mu.Unlock()
	return &xkmspb.TPMPolicyListResponse{Policies: []*xkmspb.TPMPolicy{{Name: "policy-a"}}}, nil
}

// TestTPMPolicy_Unix_ListReachesKeystoreService proves the Unix transport
// delegates TPM-policy verbs to the gRPC bridge: TPMPolicyList reaches the
// xkms.v1 KeystoreService RPC once and decodes into the typed response.
func TestTPMPolicy_Unix_ListReachesKeystoreService(t *testing.T) {
	serverTLS, clientTLS := unixTestTLS(t)
	socket := filepath.Join(t.TempDir(), "quicbaas.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	fake := &tpmPolicyListServer{}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	xkmspb.RegisterKeystoreServiceServer(server, fake)
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	defer func() {
		server.Stop()
		if err := <-served; err != nil {
			t.Errorf("serve: %v", err)
		}
	}()

	tr, err := New(&transport.Config{Address: socket, Timeout: 5 * time.Second, TLSConfig: clientTLS})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := tr.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	}()

	result, err := tr.Request(context.Background(), "quicbaas.TPMPolicy", "TPMPolicyList",
		&xkmstransport.TPMPolicyListRequest{TenantID: "tenant-a"})
	if err != nil {
		t.Fatalf("TPMPolicyList: %v", err)
	}
	resp, ok := result.(*xkmstransport.TPMPolicyListResponse)
	if !ok || len(resp.Policies) != 1 || resp.Policies[0].Name != "policy-a" {
		t.Fatalf("TPMPolicyList result %#v", result)
	}

	fake.mu.Lock()
	tenants := append([]string(nil), fake.tenants...)
	fake.mu.Unlock()
	if len(tenants) != 1 || tenants[0] != "tenant-a" {
		t.Fatalf("TPMPolicyList calls saw tenants %v, want [tenant-a]", tenants)
	}
}
