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

package certfixture

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// twoNodeTopology is a minimal but realistic declaration: two serving
// identities under their container names and one tenant-scoped login
// credential.
func twoNodeTopology() *Topology {
	return &Topology{
		SPIFFETrustDomain: trustDomain,
		Nodes: []ServerFixture{
			{File: "node1", CommonName: "test-node1", DNSNames: []string{"localhost", "test-node1"}, IPAddresses: []string{"127.0.0.1"}},
			{File: "node2", CommonName: "test-node2", DNSNames: []string{"localhost", "test-node2"}, IPAddresses: []string{"127.0.0.1"}},
		},
		Clients: []ClientFixture{
			{File: "admin", CommonName: "admin@certfixture.test", Email: "admin@certfixture.test", TenantID: "system"},
		},
	}
}

// =============================================================================
// Write / Ensure
// =============================================================================

// Write produces the complete declared set, every leaf chains to the written
// trust anchor, and every pair loads as a TLS keypair.
//
// The tls.LoadX509KeyPair assertion is deliberate. go-xkms encodes an ECDSA key
// as PKCS#8 inside a PEM block labelled "EC PRIVATE KEY", so a consumer that
// dispatches on the label rejects it; the nodes load these files with
// tls.LoadX509KeyPair, and this proves that path works on what the product
// actually emits.
func TestWrite_ProducesACompleteUsableSet(t *testing.T) {
	dir := t.TempDir()
	topo := twoNodeTopology()

	if err := Write(context.Background(), dir, topo); err != nil {
		t.Fatalf("Write: %v", err)
	}

	for _, path := range topo.Files(dir) {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("declared fixture %s was not written: %v", path, err)
		}
	}

	caPEM, err := os.ReadFile(CACertPath(dir))
	if err != nil {
		t.Fatalf("read trust anchor: %v", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		t.Fatal("written ca.crt is not usable as a trust anchor")
	}

	for _, stem := range []string{"node1", "node2", "admin"} {
		certPath := filepath.Join(dir, stem+".crt")
		keyPath := filepath.Join(dir, stem+".key")
		if _, err := tls.LoadX509KeyPair(certPath, keyPath); err != nil {
			t.Errorf("tls.LoadX509KeyPair(%s): %v", stem, err)
			continue
		}
		pemBytes, err := os.ReadFile(certPath)
		if err != nil {
			t.Fatalf("read %s: %v", certPath, err)
		}
		cert, err := decodeCertificate(pemBytes)
		if err != nil {
			t.Fatalf("decode %s: %v", certPath, err)
		}
		if _, err := cert.Verify(x509.VerifyOptions{
			Roots:     pool,
			KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		}); err != nil {
			t.Errorf("%s does not chain to the written trust anchor: %v", stem, err)
		}
	}
}

// Private keys are owner-only and certificates are world-readable, because a
// different container's process reads them than the one that wrote them.
func TestWrite_UsesSeparateModesForKeysAndCertificates(t *testing.T) {
	dir := t.TempDir()
	if err := Write(context.Background(), dir, twoNodeTopology()); err != nil {
		t.Fatalf("Write: %v", err)
	}
	key, err := os.Stat(filepath.Join(dir, "node1.key"))
	if err != nil {
		t.Fatalf("stat node1.key: %v", err)
	}
	if key.Mode().Perm() != 0o600 {
		t.Errorf("node1.key mode = %v, want 0600", key.Mode().Perm())
	}
	cert, err := os.Stat(filepath.Join(dir, "node1.crt"))
	if err != nil {
		t.Fatalf("stat node1.crt: %v", err)
	}
	if cert.Mode().Perm() != 0o644 {
		t.Errorf("node1.crt mode = %v, want 0644", cert.Mode().Perm())
	}
}

// Ensure is a no-op over a complete set: the trust anchor is not rewritten, so a
// node that already loaded it keeps verifying the leaves it was given.
//
// Compose starts the fixture service twice — once for `up`, once for the later
// `run` — so a second generation here would mint a second root under the same
// subject name and the cluster would start reporting "certificate signed by
// unknown authority" against an authority it can name.
func TestEnsure_LeavesACompleteSetAlone(t *testing.T) {
	dir := t.TempDir()
	topo := twoNodeTopology()

	if err := Ensure(context.Background(), dir, topo); err != nil {
		t.Fatalf("first Ensure: %v", err)
	}
	before, err := os.ReadFile(CACertPath(dir))
	if err != nil {
		t.Fatalf("read trust anchor: %v", err)
	}

	if err := Ensure(context.Background(), dir, topo); err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	after, err := os.ReadFile(CACertPath(dir))
	if err != nil {
		t.Fatalf("re-read trust anchor: %v", err)
	}
	if string(before) != string(after) {
		t.Fatal("the second Ensure replaced the trust anchor")
	}
}

// A partial set is redone, so an interrupted generation is never half-used.
func TestEnsure_RedoesAPartialSet(t *testing.T) {
	dir := t.TempDir()
	topo := twoNodeTopology()

	if err := Ensure(context.Background(), dir, topo); err != nil {
		t.Fatalf("first Ensure: %v", err)
	}
	missing := filepath.Join(dir, "node2.crt")
	if err := os.Remove(missing); err != nil {
		t.Fatalf("remove %s: %v", missing, err)
	}

	if err := Ensure(context.Background(), dir, topo); err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	if _, err := os.Stat(missing); err != nil {
		t.Fatalf("%s was not reissued: %v", missing, err)
	}
}

// An invalid topology is refused before anything is written.
func TestWriteAndEnsure_RejectAnInvalidTopology(t *testing.T) {
	dir := t.TempDir()
	bad := &Topology{}
	if err := Write(context.Background(), dir, bad); !errors.Is(err, ErrNoTopologyNodes) {
		t.Fatalf("Write error = %v, want ErrNoTopologyNodes", err)
	}
	if err := Ensure(context.Background(), dir, bad); !errors.Is(err, ErrNoTopologyNodes) {
		t.Fatalf("Ensure error = %v, want ErrNoTopologyNodes", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	if len(entries) != 0 {
		t.Errorf("an invalid topology wrote %d entries", len(entries))
	}
}

// A directory that cannot be created is reported as a FileError naming the path.
func TestWrite_ReportsAnUnusableDirectory(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write %s: %v", blocker, err)
	}
	err := Write(context.Background(), filepath.Join(blocker, "certs"), twoNodeTopology())
	var fileErr *FileError
	if !errors.As(err, &fileErr) {
		t.Fatalf("error = %v, want *FileError", err)
	}
}

// A fixture path that is already a DIRECTORY makes the write fail, and the
// failure is reported as a FileError naming that path rather than leaving a set
// that looks complete but is not.
//
// Both halves are exercised, because they are written by different call sites:
// the trust anchor by Write itself and the credential pair by writePair.
func TestWrite_ReportsAnUnwritableFixturePath(t *testing.T) {
	for _, blocked := range []string{"ca.crt", "node1.key"} {
		t.Run(blocked, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, blocked), 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", blocked, err)
			}

			err := Write(context.Background(), dir, twoNodeTopology())
			var fileErr *FileError
			if !errors.As(err, &fileErr) {
				t.Fatalf("error = %v, want *FileError", err)
			}
			if fileErr.Op != "write" {
				t.Errorf("Op = %q, want write", fileErr.Op)
			}
			if filepath.Base(fileErr.Path) != blocked {
				t.Errorf("Path = %q, want one ending in %q", fileErr.Path, blocked)
			}
		})
	}
}

// A keystore directory that cannot be created is reported as an AuthorityError
// naming it, so "the CA could not be opened" is never mistaken for "the CA
// refused to issue".
func TestOpen_ReportsAnUnusableKeystoreDir(t *testing.T) {
	dir := t.TempDir()
	blocker := KeystoreDir(dir)
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write %s: %v", blocker, err)
	}

	_, err := Open(context.Background(), Config{Dir: dir, SPIFFETrustDomain: trustDomain})
	var authErr *AuthorityError
	if !errors.As(err, &authErr) {
		t.Fatalf("error = %v, want *AuthorityError", err)
	}
	if authErr.Op != "open ca" || authErr.Dir != blocker {
		t.Errorf("error = %+v, want Op=open ca Dir=%s", authErr, blocker)
	}
}

// =============================================================================
// Topology
// =============================================================================

// Validate accepts a complete declaration and names what is wrong with an
// incomplete one.
func TestTopologyValidate(t *testing.T) {
	if err := twoNodeTopology().Validate(); err != nil {
		t.Fatalf("a complete topology was rejected: %v", err)
	}

	cases := []struct {
		name string
		topo Topology
		want error
	}{
		{
			name: "no nodes",
			topo: Topology{SPIFFETrustDomain: trustDomain},
			want: ErrNoTopologyNodes,
		},
		{
			name: "node with no file stem",
			topo: Topology{Nodes: []ServerFixture{{CommonName: "test-node1"}}},
			want: ErrNoFileStem,
		},
		{
			name: "node with no common name",
			topo: Topology{Nodes: []ServerFixture{{File: "node1"}}},
			want: ErrNoCommonName,
		},
		{
			name: "client with no file stem",
			topo: Topology{
				Nodes:   []ServerFixture{{File: "node1", CommonName: "test-node1"}},
				Clients: []ClientFixture{{CommonName: "admin@certfixture.test"}},
			},
			want: ErrNoFileStem,
		},
		{
			name: "client with no common name",
			topo: Topology{
				Nodes:   []ServerFixture{{File: "node1", CommonName: "test-node1"}},
				Clients: []ClientFixture{{File: "admin"}},
			},
			want: ErrNoCommonName,
		},
		{
			name: "tenant-scoped client with no trust domain",
			topo: Topology{
				Nodes:   []ServerFixture{{File: "node1", CommonName: "test-node1"}},
				Clients: []ClientFixture{{File: "admin", CommonName: "admin@certfixture.test", TenantID: "system"}},
			},
			want: ErrNoTrustDomain,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.topo.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("Validate error = %v, want %v", err, tc.want)
			}
		})
	}
}

// Files lists the trust anchor and both halves of every declared credential, in
// declaration order, so the completeness check and the generation cannot
// disagree about what a complete set is.
func TestTopologyFiles(t *testing.T) {
	got := twoNodeTopology().Files("/certs")
	want := []string{
		"/certs/ca.crt",
		"/certs/node1.crt", "/certs/node1.key",
		"/certs/node2.crt", "/certs/node2.key",
		"/certs/admin.crt", "/certs/admin.key",
	}
	if len(got) != len(want) {
		t.Fatalf("Files = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != filepath.FromSlash(want[i]) {
			t.Fatalf("Files = %v, want %v", got, want)
		}
	}
}

// LoadTopology reads a declaration back with every field mapped.
func TestLoadTopology_RoundTripsADeclaration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "topology.json")
	data, err := json.Marshal(twoNodeTopology())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	topo, err := LoadTopology(path)
	if err != nil {
		t.Fatalf("LoadTopology: %v", err)
	}
	if topo.SPIFFETrustDomain != trustDomain {
		t.Errorf("trust domain = %q, want %q", topo.SPIFFETrustDomain, trustDomain)
	}
	if len(topo.Nodes) != 2 || topo.Nodes[0].CommonName != "test-node1" {
		t.Fatalf("nodes = %+v", topo.Nodes)
	}
	if len(topo.Nodes[0].DNSNames) != 2 || len(topo.Nodes[0].IPAddresses) != 1 {
		t.Errorf("node SANs did not round-trip: %+v", topo.Nodes[0])
	}
	if len(topo.Clients) != 1 || topo.Clients[0].TenantID != "system" {
		t.Fatalf("clients = %+v", topo.Clients)
	}
}

// A missing, malformed or invalid declaration is reported as a FileError naming
// the path and the step that failed.
func TestLoadTopology_RejectsBadInput(t *testing.T) {
	dir := t.TempDir()

	var fileErr *FileError
	if _, err := LoadTopology(filepath.Join(dir, "absent.json")); !errors.As(err, &fileErr) {
		t.Fatalf("missing file error = %v, want *FileError", err)
	} else if fileErr.Op != "read" {
		t.Errorf("Op = %q, want read", fileErr.Op)
	}

	malformed := filepath.Join(dir, "malformed.json")
	if err := os.WriteFile(malformed, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write %s: %v", malformed, err)
	}
	if _, err := LoadTopology(malformed); !errors.As(err, &fileErr) {
		t.Fatalf("malformed file error = %v, want *FileError", err)
	} else if fileErr.Op != "parse" {
		t.Errorf("Op = %q, want parse", fileErr.Op)
	}

	empty := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(empty, []byte(`{"nodes":[]}`), 0o600); err != nil {
		t.Fatalf("write %s: %v", empty, err)
	}
	_, err := LoadTopology(empty)
	if !errors.As(err, &fileErr) {
		t.Fatalf("invalid declaration error = %v, want *FileError", err)
	}
	if fileErr.Op != "validate" || !errors.Is(err, ErrNoTopologyNodes) {
		t.Errorf("error = %v (Op %q), want a validate failure wrapping ErrNoTopologyNodes", err, fileErr.Op)
	}
}

// =============================================================================
// the committed declaration
// =============================================================================

// The declaration the harness actually ships is valid, and where a node
// configuration pins a SPIFFE trust domain it is the one the fixtures stamp.
//
// The pin is conditional because it is: go-quicbaas#930 reverted
// iam.spiffe_trust_domain back out of the integration node configs, and with no
// pin the daemon accepts whatever trust domain the presented certificate
// carries. The assertion is written so it holds either way and FAILS the day a
// pin comes back with a different value — which would fail every login closed,
// because login compares the configured domain against the presented one.
func TestCommittedTopology_IsValidAndAgreesWithAnyNodePin(t *testing.T) {
	topo, err := LoadTopology("topology.json")
	if err != nil {
		t.Fatalf("the committed topology is not loadable: %v", err)
	}
	if len(topo.Nodes) == 0 {
		t.Fatal("the committed topology declares no nodes")
	}
	if len(topo.Clients) == 0 {
		t.Fatal("the committed topology declares no client credentials; the suite could not log in")
	}
	if topo.SPIFFETrustDomain == "" {
		t.Fatal("the committed topology declares no trust domain; tenant-scoped clients could not be issued")
	}

	for _, config := range []string{
		filepath.Join("..", "quicbaas.yaml"),
		filepath.Join("..", "quicbaas-tenantbarrier.yaml"),
	} {
		data, err := os.ReadFile(config)
		if err != nil {
			t.Fatalf("read %s: %v", config, err)
		}
		pinned, ok := pinnedTrustDomain(string(data))
		if !ok {
			continue
		}
		if pinned != topo.SPIFFETrustDomain {
			t.Errorf("%s pins trust domain %q, fixtures stamp %q", config, pinned, topo.SPIFFETrustDomain)
		}
	}
}

// pinnedTrustDomain returns the iam.spiffe_trust_domain a node configuration
// pins, and whether it pins one at all. The match is on a line whose only
// leading content is the key, so a commented-out mention is not read as a pin.
func pinnedTrustDomain(config string) (string, bool) {
	const key = "spiffe_trust_domain:"
	for _, line := range splitLines(config) {
		trimmed := trimLeadingSpace(line)
		if !hasPrefix(trimmed, key) {
			continue
		}
		return trimLeadingSpace(trimmed[len(key):]), true
	}
	return "", false
}

// splitLines splits s on newlines without pulling in a dependency for three
// lines of scanning.
func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return out
}

// trimLeadingSpace drops leading spaces and tabs.
func trimLeadingSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	return s
}

// hasPrefix reports whether s starts with prefix.
func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
