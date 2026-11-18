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

package service

import (
	"context"
	"crypto/x509"
	"errors"
	"strings"
	"testing"

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/fleet/deploy"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/xkmsca"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/test/sshfixture"
)

// TestXKMSAgentCertIssuer_AgentCABundleVerifiesWhatItIssues: the bundle
// preflight verifies the control plane with is the one the issued daemon
// trusts — so it must anchor the certificates this CA issues.
func TestXKMSAgentCertIssuer_AgentCABundleVerifiesWhatItIssues(t *testing.T) {
	ca := xkmsca.New(t, "QuicBaaS #1022 Preflight CA")
	issuer, err := NewXKMSFleetNodeCertIssuer(ca)
	if err != nil {
		t.Fatalf("NewXKMSFleetNodeCertIssuer: %v", err)
	}
	bundle, err := issuer.FleetCABundle(context.Background(), testAgentTenant)
	if err != nil {
		t.Fatalf("FleetCABundle: %v", err)
	}
	issued, err := issuer.IssueFleetNodeCertificate(context.Background(), &FleetNodeCertificateRequest{TenantID: testAgentTenant, Hostname: "agent-01"})
	if err != nil {
		t.Fatalf("IssueFleetNodeCertificate: %v", err)
	}
	defer issued.PrivateKey.Destroy()
	if string(bundle) != string(issued.CAPEM) {
		t.Fatal("preflight's CA bundle differs from the one the deploy installs")
	}
}

func TestSDKAgentCertIssuer_AgentCABundle(t *testing.T) {
	ca := xkmsca.NewCAService(t, "QuicBaaS #1022 SDK Preflight CA")
	issuer, err := NewSDKFleetNodeCertIssuer(ca)
	if err != nil {
		t.Fatalf("NewSDKFleetNodeCertIssuer: %v", err)
	}
	bundle, err := issuer.FleetCABundle(context.Background(), sdkIssuerTenant)
	if err != nil {
		t.Fatalf("FleetCABundle: %v", err)
	}
	if !x509.NewCertPool().AppendCertsFromPEM(bundle) {
		t.Fatal("bundle is not a usable trust anchor")
	}

	failing := xkmsca.NewCAService(t, "QuicBaaS #1022 SDK Preflight CA failing")
	failing.SetBundleErr(errors.New("no bundle"))
	issuer, err = NewSDKFleetNodeCertIssuer(failing)
	if err != nil {
		t.Fatalf("NewSDKFleetNodeCertIssuer: %v", err)
	}
	var issueErr *FleetNodeCertIssueError
	if _, err := issuer.FleetCABundle(context.Background(), sdkIssuerTenant); !errors.As(err, &issueErr) {
		t.Fatalf("want FleetNodeCertIssueError, got %v", err)
	}
}

// TestXKMSSSHHostKeyResolver_UnpinnedAlgorithmIsNamed is the remote fleet node's host-key
// failure: pinned by its ed25519 key, the host negotiated another algorithm.
// The error says so, still matches as a host-key mismatch, and its fix names
// the algorithm to pin.
func TestXKMSSSHHostKeyResolver_UnpinnedAlgorithmIsNamed(t *testing.T) {
	host := sshfixture.Host(t)
	store := &fakeKnownHosts{pins: []*xkmsssh.Pin{pin("agent-01", "SHA256:someotherkey", "ecdsa-sha2-nistp256")}}
	resolver := &XKMSSSHHostKeyResolver{KnownHosts: store, Owner: "op"}

	policy, err := resolver.HostKeyPolicy(context.Background(), SSHHostRef{Host: "agent-01"})
	if err != nil {
		t.Fatalf("HostKeyPolicy: %v", err)
	}
	err = policy.Callback("agent-01:22", nil, host.Public)

	var algorithm *SSHHostKeyAlgorithmMismatchError
	var mismatch *SSHHostKeyMismatchError
	if !errors.As(err, &algorithm) || !errors.As(err, &mismatch) {
		t.Fatalf("want an algorithm mismatch that is also a mismatch, got %T: %v", err, err)
	}
	if algorithm.Presented != "ssh-ed25519" || !strings.Contains(err.Error(), "ecdsa-sha2-nistp256") {
		t.Fatalf("error does not name both algorithms: %v", err)
	}
	fix := deploy.FixFor(err)
	if !strings.Contains(fix, "--algorithm ssh-ed25519 --fingerprint "+host.Fingerprint) {
		t.Fatalf("fix = %q", fix)
	}
	if (&SSHHostKeyAlgorithmMismatchError{}).Unwrap() != nil {
		t.Fatal("an algorithm mismatch without a mismatch should unwrap to nil")
	}

	// A pin with no algorithm covers every key, so a wrong fingerprint under
	// it is a plain mismatch.
	store.pins = []*xkmsssh.Pin{pin("agent-01", "SHA256:someotherkey", "")}
	policy, err = resolver.HostKeyPolicy(context.Background(), SSHHostRef{Host: "agent-01"})
	if err != nil {
		t.Fatalf("HostKeyPolicy: %v", err)
	}
	if err := policy.Callback("agent-01:22", nil, host.Public); errors.As(err, &algorithm) || !errors.As(err, &mismatch) {
		t.Fatalf("want a plain mismatch, got %T: %v", err, err)
	}
}

func TestHostKeyErrors_Fixes(t *testing.T) {
	notPinned := &SSHHostKeyNotPinnedError{Host: "agent-01", Scope: "user", Owner: "op"}
	if !strings.Contains(notPinned.Fix(), "quicbaas ssh known-hosts add --host agent-01") {
		t.Fatalf("fix = %q", notPinned.Fix())
	}
	mismatch := &SSHHostKeyMismatchError{Host: "agent-01", Algorithm: "ssh-ed25519", Presented: "SHA256:new"}
	if !strings.Contains(mismatch.Fix(), "Confirm on the node") || !strings.Contains(mismatch.Fix(), "--fingerprint SHA256:new") {
		t.Fatalf("fix = %q", mismatch.Fix())
	}
}
