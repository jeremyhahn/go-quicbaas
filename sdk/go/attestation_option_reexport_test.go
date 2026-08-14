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
	"crypto/x509"
	"testing"

	sdkservice "github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// recordingContainerAttestor implements the container attestation seam using
// only SDK names. It exists to prove an embedding consumer — go-trusted-platform
// is the intended one — can fill the seam without importing pkg/service.
type recordingContainerAttestor struct {
	requests []*sdkservice.AttestContainerRequest
}

func (a *recordingContainerAttestor) AttestContainer(
	_ context.Context,
	req *sdkservice.AttestContainerRequest,
) (*sdkservice.AttestContainerResponse, error) {
	a.requests = append(a.requests, req)
	return &sdkservice.AttestContainerResponse{Attested: true}, nil
}

// The SDK ContainerAttestor alias must be the interface WithContainerAttestor
// accepts, otherwise the seam is unreachable from the SDK surface.
func TestContainerAttestorAlias_IsServiceAttestor(t *testing.T) {
	var _ sdkservice.ContainerAttestor = (*recordingContainerAttestor)(nil)
}

// WithContainerAttestor is the composer re-export of
// server.WithContainerAttestor. It must return a usable ServerOption and that
// option must be accepted by NewServer, so an embedding consumer can register
// a fleet attestor at server construction time.
func TestWithContainerAttestorReExport_ReachableAndApplies(t *testing.T) {
	opt := WithContainerAttestor(&recordingContainerAttestor{})
	if opt == nil {
		t.Fatal("WithContainerAttestor returned a nil ServerOption")
	}

	srv, err := NewServer(TestServerConfig(), opt)
	if err != nil {
		t.Fatalf("NewServer with WithContainerAttestor: %v", err)
	}
	if srv == nil {
		t.Fatal("NewServer returned a nil server")
	}
}

// The seam is optional: a nil attestor is the documented "not configured"
// state and must not fail construction.
func TestWithContainerAttestorReExport_NilAttestor(t *testing.T) {
	srv, err := NewServer(TestServerConfig(), WithContainerAttestor(nil))
	if err != nil {
		t.Fatalf("NewServer with a nil container attestor: %v", err)
	}
	if srv == nil {
		t.Fatal("NewServer returned a nil server")
	}
}

// Verification anchors are the other half of the attestation seam: without
// this re-export a consumer can register an attestor but cannot anchor the
// statements it produces.
func TestWithAttestationTrustAnchorsReExport_ReachableAndApplies(t *testing.T) {
	tests := []struct {
		name    string
		anchors []*x509.Certificate
	}{
		{"no anchors", nil},
		{"empty anchor set", []*x509.Certificate{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opt := WithAttestationTrustAnchors(tt.anchors)
			if opt == nil {
				t.Fatal("WithAttestationTrustAnchors returned a nil ServerOption")
			}

			srv, err := NewServer(TestServerConfig(), opt)
			if err != nil {
				t.Fatalf("NewServer with WithAttestationTrustAnchors: %v", err)
			}
			if srv == nil {
				t.Fatal("NewServer returned a nil server")
			}
		})
	}
}
