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

package quicbaas

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/quic-go/quic-go/http3"
	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	pkgiam "github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	pkgservice "github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/xkmsca"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/embedded"
	sdkgrpc "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/grpc"
	sdkmcp "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
	sdkquic "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/quic"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/rest"
	sdkunix "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/unix"
)

// adminFactorServer is the server side of an admin factor enrollment: the
// quicbaas REST, gRPC and MCP handlers over one FIDO2 and one certificate
// service. Each transport under test serves these real handlers.
type adminFactorServer struct {
	facade *mocks.MockQuicBaaS
	log    *logger.Logger
	tls    adminFactorTLS
	calls  atomic.Int64
}

func newAdminFactorServer(t *testing.T, fido2 service.FIDO2Service, cert service.CertificateService) *adminFactorServer {
	t.Helper()
	facade := mocks.NewMockQuicBaaS()
	facade.FIDO2ServiceFunc = func() service.FIDO2Service { return fido2 }
	facade.CertificateServiceFunc = func() service.CertificateService { return cert }
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	return &adminFactorServer{facade: facade, log: log, tls: newAdminFactorTLS(t)}
}

// caller stands in for the server's authentication middleware: it binds the
// authenticated admin and their tenant to the request, and counts requests
// so a test can prove none reached the server.
func (s *adminFactorServer) caller(ctx context.Context) context.Context {
	s.calls.Add(1)
	return tenantctx.WithTenant(passkeyCaller(ctx), passkeyTenant)
}

// adminFactorTransport opens one SDK transport against the server.
type adminFactorTransport func(t *testing.T, s *adminFactorServer) transport.Transport

// adminFactorTransports are every transport an admin factor enrollment can
// run over.
var adminFactorTransports = map[string]adminFactorTransport{
	"embedded": embeddedAdminFactors,
	"rest":     restAdminFactors,
	"quic":     quicAdminFactors,
	"grpc":     grpcAdminFactors,
	"unix":     unixAdminFactors,
	"mcp":      mcpAdminFactors,
}

func embeddedAdminFactors(t *testing.T, s *adminFactorServer) transport.Transport {
	t.Helper()
	tr, err := embedded.New(s.facade)
	if err != nil {
		t.Fatalf("embedded.New: %v", err)
	}
	t.Cleanup(func() { closeFIDO2Transport(t, tr) })
	return tr
}

// httpHandler answers the routes the SDK REST transport calls the way the
// server's REST handlers do: decode the JSON request, call the service as the
// authenticated caller, and answer with the service's response as JSON. The
// server's handlers build only under the rest tag, which the SDK tests do not
// set.
func (s *adminFactorServer) httpHandler(t *testing.T) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/xkms/fido2/credentials/begin", func(w http.ResponseWriter, r *http.Request) {
		tenantID, _ := tenantctx.Lookup(r.Context()) // comma-ok: the caller's own tenant, as the server handler reads it
		resp, err := s.facade.FIDO2().BeginAddCredential(r.Context(), tenantID)
		writeAdminFactorJSON(t, w, resp, err)
	})
	mux.HandleFunc("POST /api/v1/xkms/fido2/register/finish", func(w http.ResponseWriter, r *http.Request) {
		var req xkmstransport.FinishRegistrationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp, err := s.facade.FIDO2().FinishRegistration(r.Context(), r.URL.Query().Get("tenant_id"), &req)
		writeAdminFactorJSON(t, w, resp, err)
	})
	mux.HandleFunc("POST /api/v1/certificate/sign-csr", func(w http.ResponseWriter, r *http.Request) {
		var req pkgservice.SignCSRRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp, err := s.facade.Certificate().SignCSR(r.Context(), &req)
		if err != nil {
			writeAdminFactorJSON(t, w, nil, err)
			return
		}
		writeAdminFactorJSON(t, w, map[string]any{
			"certificate_pem":    resp.CertificatePEM,
			"ca_certificate_pem": resp.CACertificatePEM,
			"chain_pem":          resp.ChainPEM,
			"serial_number":      resp.SerialNumber,
			"not_before":         resp.NotBefore.Unix(),
			"not_after":          resp.NotAfter.Unix(),
		}, nil)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r.WithContext(s.caller(r.Context())))
	})
}

func writeAdminFactorJSON(t *testing.T, w http.ResponseWriter, resp any, err error) {
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

func restAdminFactors(t *testing.T, s *adminFactorServer) transport.Transport {
	t.Helper()
	server := httptest.NewServer(s.httpHandler(t))
	t.Cleanup(server.Close)

	tr, err := rest.New(&transport.Config{Address: server.URL, Timeout: 5 * time.Second}, rest.WithDefaultTenant(passkeyTenant))
	if err != nil {
		t.Fatalf("rest.New: %v", err)
	}
	t.Cleanup(func() { closeFIDO2Transport(t, tr) })
	return tr
}

func quicAdminFactors(t *testing.T, s *adminFactorServer) transport.Transport {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	server := &http3.Server{TLSConfig: s.tls.server([]string{"h3"}), Handler: s.httpHandler(t)}
	served := make(chan error, 1)
	go func() { served <- server.Serve(conn) }()
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Errorf("close http3 server: %v", err)
		}
		if err := conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Errorf("close udp listener: %v", err)
		}
		<-served
	})

	tr, err := sdkquic.New(&transport.Config{
		Address:   conn.LocalAddr().String(),
		Timeout:   5 * time.Second,
		TLSConfig: s.tls.client(),
	})
	if err != nil {
		t.Fatalf("quic.New: %v", err)
	}
	t.Cleanup(func() { closeFIDO2Transport(t, tr) })
	return tr
}

// serveGRPC serves the gRPC FIDO2 and certificate services on listener.
func (s *adminFactorServer) serveGRPC(t *testing.T, listener net.Listener) {
	t.Helper()
	api, err := grpcapi.NewServer(s.facade, s.log, 1, "test")
	if err != nil {
		t.Fatalf("grpcapi.NewServer: %v", err)
	}
	server := grpclib.NewServer(
		grpclib.Creds(credentials.NewTLS(s.tls.server(nil))),
		grpclib.UnaryInterceptor(func(ctx context.Context, req any, _ *grpclib.UnaryServerInfo, handler grpclib.UnaryHandler) (any, error) {
			return handler(s.caller(ctx), req)
		}),
	)
	grpcapi.RegisterFIDO2ServiceServer(server, api)
	grpcapi.RegisterCertificateServiceServer(server, api)
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		if err := <-served; err != nil {
			t.Errorf("grpc serve: %v", err)
		}
	})
}

func grpcAdminFactors(t *testing.T, s *adminFactorServer) transport.Transport {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	s.serveGRPC(t, listener)

	tr, err := sdkgrpc.New(&transport.Config{
		Address:   listener.Addr().String(),
		Timeout:   5 * time.Second,
		TLSConfig: s.tls.client(),
	})
	if err != nil {
		t.Fatalf("grpc.New: %v", err)
	}
	t.Cleanup(func() { closeFIDO2Transport(t, tr) })
	return tr
}

func unixAdminFactors(t *testing.T, s *adminFactorServer) transport.Transport {
	t.Helper()
	// t.TempDir paths can exceed the 107-byte sun_path limit.
	dir, err := os.MkdirTemp("", "af")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("remove socket dir: %v", err)
		}
	})
	socket := filepath.Join(dir, "qb.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	s.serveGRPC(t, listener)

	tr, err := sdkunix.New(&transport.Config{
		Address:   socket,
		Timeout:   5 * time.Second,
		TLSConfig: s.tls.client(),
	})
	if err != nil {
		t.Fatalf("unix.New: %v", err)
	}
	t.Cleanup(func() { closeFIDO2Transport(t, tr) })
	return tr
}

// mcpTools answers the MCP tools an admin factor enrollment calls the way the
// server's tool registry does. The registry builds only under the mcp tag,
// which the SDK tests do not set.
func (s *adminFactorServer) mcpTools() map[string]func(context.Context, map[string]any) (any, error) {
	return map[string]func(context.Context, map[string]any) (any, error){
		"xkms.fido2.beginAddCredential": func(ctx context.Context, _ map[string]any) (any, error) {
			tenantID, _ := tenantctx.Lookup(ctx) // comma-ok: the caller's own tenant, as the server tool reads it
			return s.facade.FIDO2().BeginAddCredential(ctx, tenantID)
		},
		"xkms.fido2.finishRegistration": func(ctx context.Context, args map[string]any) (any, error) {
			tenantID, ok := args["tenant_id"].(string)
			if !ok || tenantID == "" {
				tenantID, _ = tenantctx.Lookup(ctx) // comma-ok: an unnamed tenant is the caller's own
			}
			sessionID, _ := args["session_id"].(string)             // comma-ok: the service refuses an empty session
			attestationJSON, _ := args["attestation_json"].(string) // comma-ok: the service refuses an empty attestation
			return s.facade.FIDO2().FinishRegistration(ctx, tenantID, &xkmstransport.FinishRegistrationRequest{
				SessionID:       sessionID,
				AttestationJSON: attestationJSON,
			})
		},
		"quicbaas_certificate_sign_csr": func(ctx context.Context, args map[string]any) (any, error) {
			csrPEM, _ := args["csr_pem"].(string) // comma-ok: the CA refuses an empty CSR
			usage, _ := args["usage"].(string)    // comma-ok: an unnamed usage is the CA's default
			resp, err := s.facade.Certificate().SignCSR(ctx, &pkgservice.SignCSRRequest{CSRPEM: csrPEM, Usage: usage})
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"certificate_pem":    resp.CertificatePEM,
				"ca_certificate_pem": resp.CACertificatePEM,
				"chain_pem":          resp.ChainPEM,
				"serial_number":      resp.SerialNumber,
				"not_before":         resp.NotBefore.Format(time.RFC3339),
				"not_after":          resp.NotAfter.Format(time.RFC3339),
			}, nil
		},
	}
}

func mcpAdminFactors(t *testing.T, s *adminFactorServer) transport.Transport {
	t.Helper()
	server := mcpserver.NewMCPServer("admin-factors", "1.0.0")
	for name, call := range s.mcpTools() {
		server.AddTool(mcpgo.NewTool(name), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			result, err := call(ctx, req.GetArguments())
			if err != nil {
				return mcpgo.NewToolResultError(err.Error()), nil
			}
			out, err := json.Marshal(result)
			if err != nil {
				return nil, err
			}
			return mcpgo.NewToolResultText(string(out)), nil
		})
	}
	httpServer := mcpserver.NewTestStreamableHTTPServer(server,
		mcpserver.WithHTTPContextFunc(func(ctx context.Context, _ *http.Request) context.Context {
			return s.caller(ctx)
		}),
	)
	t.Cleanup(httpServer.Close)

	tr, err := sdkmcp.New(&transport.Config{Address: httpServer.URL, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("mcp.New: %v", err)
	}
	t.Cleanup(func() { closeFIDO2Transport(t, tr) })
	return tr
}

// adminFactorTLS is a self-signed server certificate for 127.0.0.1 and the
// pool a client trusts it through.
type adminFactorTLS struct {
	cert tls.Certificate
	pool *x509.CertPool
}

func newAdminFactorTLS(t *testing.T) adminFactorTLS {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return adminFactorTLS{cert: tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, pool: pool}
}

func (c adminFactorTLS) server(protos []string) *tls.Config {
	return &tls.Config{Certificates: []tls.Certificate{c.cert}, MinVersion: tls.VersionTLS13, NextProtos: protos}
}

func (c adminFactorTLS) client() *tls.Config {
	return &tls.Config{RootCAs: c.pool, MinVersion: tls.VersionTLS13}
}

// pivServer is an admin whose certificate client reaches the server's CA
// over the transport under test.
type pivServer struct {
	enroller AdminFactorEnroller
	factors  AuthFactorsClient
	store    qrdbsdk.UserAuthFactorServicer
	ca       *xkmsca.CAService
}

func newPIVServer(t *testing.T, open adminFactorTransport) *pivServer {
	t.Helper()
	ca := xkmsca.NewCAService(t, t.Name())
	cert := pkgservice.NewCertificateService(&caSigningClient{ca: ca})
	c, store := buildLoginFactorRoundtrip(t, newDirectory(testUser{id: passkeyUserID, tenantID: adminTenant, username: "root"}))
	c.certificateClient = newCertificateClient(open(t, newAdminFactorServer(t, nil, cert)))
	auth := c.Security().Auth()
	return &pivServer{enroller: auth.AdminFactors(), factors: auth.Factors(), store: store, ca: ca}
}

func pivRequest(t *testing.T) *AdminFactorEnrollRequest {
	t.Helper()
	return &AdminFactorEnrollRequest{
		Factors:      []string{"piv"},
		Subject:      "root",
		TenantID:     adminTenant,
		PIVGenerator: xkmssdk.NewSoftwarePIVKeyGenerator(),
		PIVCertStore: newPIVCertStore(t),
	}
}

func TestAdminFactors_PIVCertificateOnEveryTransport(t *testing.T) {
	for name, open := range adminFactorTransports {
		t.Run(name, func(t *testing.T) {
			s := newPIVServer(t, open)

			results, err := s.enroller.Enroll(context.Background(), pivRequest(t))
			if err != nil {
				t.Fatalf("Enroll: %v", err)
			}
			if len(results) != 1 || results[0].Factor != qrdbsdk.FactorPIV {
				t.Fatalf("results = %+v, want one piv enrollment", results)
			}
			cert, err := parseAdminFactorCertificate(results[0].Enrollment.PublicKey)
			if err != nil {
				t.Fatalf("enrollment key material is not a certificate: %v", err)
			}
			if err := s.ca.VerifyAsClientCert(cert); err != nil {
				t.Fatalf("certificate %s was not issued by the server CA: %v", cert.Subject, err)
			}
			for key, credential := range map[string][]byte{
				"cert digest": pkgiam.MTLSCredentialKey(cert),
				"spki digest": pkgiam.MTLSSPKICredentialKey(cert),
			} {
				factor, err := s.store.LookupByCredentialKey(context.Background(), qrdbsdk.AuthFactorPIV, credential)
				if err != nil {
					t.Fatalf("LookupByCredentialKey(%s): %v", key, err)
				}
				if factor.UserID != passkeyUserID {
					t.Errorf("%s resolved user %d, want %d", key, factor.UserID, passkeyUserID)
				}
			}
		})
	}
}

func TestAdminFactors_PIVRefusedByCAOnEveryTransport(t *testing.T) {
	for name, open := range adminFactorTransports {
		t.Run(name, func(t *testing.T) {
			s := newPIVServer(t, open)
			s.ca.SignCSRErr = errors.New("ca offline")

			if _, err := s.enroller.Enroll(context.Background(), pivRequest(t)); err == nil {
				t.Fatal("Enroll succeeded although the CA refused the certificate")
			}
			listed, err := s.factors.List(context.Background(), "root")
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if listed.Total != 0 {
				t.Errorf("a factor was stored without an issued certificate: %d factors", listed.Total)
			}
		})
	}
}
