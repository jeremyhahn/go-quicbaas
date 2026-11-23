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

package mcp

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"reflect"
	"testing"
	"time"

	trustclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/trust"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

const trustServiceName = "quicbaas.Trust"

// trustTestCertDER returns the DER of a fresh self-signed P-256 certificate.
func trustTestCertDER(t *testing.T, cn string) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	return der
}

// TestTrustMCP_AutoImportAIA_SendsPEMBundle checks the DER chain travels as
// the one PEM bundle the trust tool takes, and the result decodes into the
// client type.
func TestTrustMCP_AutoImportAIA_SendsPEMBundle(t *testing.T) {
	der1 := trustTestCertDER(t, "leaf-issuer")
	der2 := trustTestCertDER(t, "root")
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_trust_auto_import_aia",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			bundle, ok := args["chain_pem"].(string)
			if !ok {
				t.Fatalf("chain_pem = %#v, want a string", args["chain_pem"])
			}
			var got [][]byte
			rest := []byte(bundle)
			for {
				var block *pem.Block
				block, rest = pem.Decode(rest)
				if block == nil {
					break
				}
				if block.Type != "CERTIFICATE" {
					t.Errorf("block type = %q, want CERTIFICATE", block.Type)
				}
				got = append(got, block.Bytes)
			}
			if len(got) != 2 || !bytes.Equal(got[0], der1) || !bytes.Equal(got[1], der2) {
				t.Errorf("chain_pem decoded to %d blocks, want the two DERs sent in order", len(got))
			}
		},
		result: map[string]interface{}{"imported": 2},
	})
	defer cleanup()

	resp, err := tr.Request(context.Background(), trustServiceName, "AutoImportAIA",
		&trustclient.AutoImportAIARequest{ChainDER: [][]byte{der1, der2}})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	want := &trustclient.AutoImportAIAResponse{Imported: 2}
	if !reflect.DeepEqual(resp, want) {
		t.Errorf("resp = %#v, want %#v", resp, want)
	}
}

// TestTrustMCP_CrossSignedRootPool_DecodesRoots checks the tool's PEM
// strings decode into the client type, and parse as the SDK client does.
func TestTrustMCP_CrossSignedRootPool_DecodesRoots(t *testing.T) {
	pemString := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: trustTestCertDER(t, "root")}))
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_trust_cross_signed_root_pool",
		result:   map[string]interface{}{"root_pems": []interface{}{pemString}, "count": 1},
	})
	defer cleanup()

	resp, err := tr.Request(context.Background(), trustServiceName, "CrossSignedRootPool",
		&trustclient.CrossSignedRootPoolRequest{})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	pool, ok := resp.(*trustclient.CrossSignedRootPoolResponse)
	if !ok {
		t.Fatalf("resp = %T, want *trustclient.CrossSignedRootPoolResponse", resp)
	}
	if len(pool.RootPEMs) != 1 || string(pool.RootPEMs[0]) != pemString {
		t.Fatalf("RootPEMs = %q, want [%q]", pool.RootPEMs, pemString)
	}
	block, _ := pem.Decode(pool.RootPEMs[0])
	if block == nil {
		t.Fatal("root PEM does not decode")
	}
	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		t.Errorf("ParseCertificate: %v", err)
	}
}

// TestTrustMCP_PublishDANE_SendsBase64DER checks PublishDANE sends its
// certificate as standard base64 and decodes the published count.
func TestTrustMCP_PublishDANE_SendsBase64DER(t *testing.T) {
	der := trustTestCertDER(t, "example.test")
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_trust_publish_dane",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			want := map[string]interface{}{
				"name":     "example.test",
				"port":     float64(443),
				"cert_der": base64.StdEncoding.EncodeToString(der),
			}
			if !reflect.DeepEqual(args, want) {
				t.Errorf("args = %#v, want %#v", args, want)
			}
		},
		result: map[string]interface{}{"published": 1},
	})
	defer cleanup()

	resp, err := tr.Request(context.Background(), trustServiceName, "PublishDANE",
		&trustclient.PublishDANERequest{Name: "example.test", Port: 443, CertDER: der})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	want := &trustclient.PublishDANEResponse{Published: 1}
	if !reflect.DeepEqual(resp, want) {
		t.Errorf("resp = %#v, want %#v", resp, want)
	}
}

// TestTrustMCP_RefusesBadRequests checks each malformed request fails as
// InvalidInput before any tool call.
func TestTrustMCP_RefusesBadRequests(t *testing.T) {
	der := []byte{0x30, 0x01, 0x00}
	cases := []struct {
		name   string
		tool   string
		method string
		req    interface{}
	}{
		{"aia nil chain", "quicbaas_trust_auto_import_aia", "AutoImportAIA", &trustclient.AutoImportAIARequest{}},
		{"aia empty chain", "quicbaas_trust_auto_import_aia", "AutoImportAIA", &trustclient.AutoImportAIARequest{ChainDER: [][]byte{}}},
		{"aia wrong type", "quicbaas_trust_auto_import_aia", "AutoImportAIA", &trustclient.PublishDANERequest{}},
		{"dane empty name", "quicbaas_trust_publish_dane", "PublishDANE", &trustclient.PublishDANERequest{Port: 443, CertDER: der}},
		{"dane port 0", "quicbaas_trust_publish_dane", "PublishDANE", &trustclient.PublishDANERequest{Name: "example.test", CertDER: der}},
		{"dane empty cert", "quicbaas_trust_publish_dane", "PublishDANE", &trustclient.PublishDANERequest{Name: "example.test", Port: 443}},
		{"dane wrong type", "quicbaas_trust_publish_dane", "PublishDANE", &trustclient.AutoImportAIARequest{}},
		{"pool wrong type", "quicbaas_trust_cross_signed_root_pool", "CrossSignedRootPool", &trustclient.AutoImportAIARequest{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
				wantTool: tc.tool,
				assertArgs: func(t *testing.T, _ map[string]interface{}) {
					t.Error("a refused request must not reach the tool")
				},
			})
			defer cleanup()

			_, err := tr.Request(context.Background(), trustServiceName, tc.method, tc.req)
			if !sdkerrors.IsInvalidInput(err) {
				t.Errorf("err = %v, want InvalidInput", err)
			}
		})
	}
}

// TestTrustMCP_DoesNotCaptureTrustedCA checks the Trust dispatch leaves the
// TrustedCA service to its own handler.
func TestTrustMCP_DoesNotCaptureTrustedCA(t *testing.T) {
	if isMCPTrustService("quicbaas.TrustedCA") {
		t.Error("isMCPTrustService(quicbaas.TrustedCA) = true, want false")
	}
	if !isMCPTrustService("quicbaas.Trust") {
		t.Error("isMCPTrustService(quicbaas.Trust) = false, want true")
	}
}
