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

// set.go turns a declared cluster topology into the fixture files a harness
// mounts.
//
// The topology is DATA, read from a JSON declaration, and never a constant in
// this package. The old generator hardcoded five node hostnames, three name
// aliases per node and two loopback addresses inside the function that signed
// the certificate, which made it unusable for any cluster shaped differently
// and made the repository carry deployment names in a cryptography tool. The
// names a cluster is reached by belong to the file that defines the cluster.

package certfixture

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// Topology declares the identities a harness needs certificates for.
type Topology struct {
	// CACommonName names the root. Empty means DefaultCACommonName.
	CACommonName string `json:"ca_common_name,omitempty"`

	// Organization is the subject organization of the root and its leaves.
	// Empty means DefaultOrganization.
	Organization string `json:"organization,omitempty"`

	// SPIFFETrustDomain is the trust domain stamped into tenant-scoped client
	// certificates. It MUST equal iam.spiffe_trust_domain in the node
	// configuration the same harness starts; login pins one against the other.
	SPIFFETrustDomain string `json:"spiffe_trust_domain"`

	// Nodes are the cluster's serving identities. At least one is required.
	Nodes []ServerFixture `json:"nodes"`

	// Clients are the client credentials the suite authenticates with.
	Clients []ClientFixture `json:"clients,omitempty"`
}

// LoadTopology reads a topology declaration from path.
func LoadTopology(path string) (*Topology, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is a harness-supplied fixture declaration
	if err != nil {
		return nil, &FileError{Op: "read", Path: path, Cause: err}
	}
	var topo Topology
	if err := json.Unmarshal(data, &topo); err != nil {
		return nil, &FileError{Op: "parse", Path: path, Cause: err}
	}
	if err := topo.Validate(); err != nil {
		return nil, &FileError{Op: "validate", Path: path, Cause: err}
	}
	return &topo, nil
}

// Validate reports whether the topology can produce a usable fixture set.
func (t *Topology) Validate() error {
	if len(t.Nodes) == 0 {
		return ErrNoTopologyNodes
	}
	hasTrustDomain := strings.TrimSpace(t.SPIFFETrustDomain) != ""
	ids := spiffeNames{}
	for _, node := range t.Nodes {
		if strings.TrimSpace(node.File) == "" {
			return ErrNoFileStem
		}
		if strings.TrimSpace(node.CommonName) == "" {
			return ErrNoCommonName
		}
		if hasTrustDomain {
			if err := ids.claim(tenantctx.SystemTenantID, qrdbsdk.SpiffeKindService, node.CommonName); err != nil {
				return err
			}
		}
	}
	for _, client := range t.Clients {
		if strings.TrimSpace(client.File) == "" {
			return ErrNoFileStem
		}
		if strings.TrimSpace(client.CommonName) == "" {
			return ErrNoCommonName
		}
		if client.TenantID == "" {
			continue
		}
		if !hasTrustDomain {
			return ErrNoTrustDomain
		}
		if err := ids.claim(client.TenantID, qrdbsdk.SpiffeKindUser, client.CommonName); err != nil {
			return err
		}
	}
	return nil
}

// Files returns every path a complete generation writes into dir, in a stable
// order.
func (t *Topology) Files(dir string) []string {
	files := make([]string, 0, 1+2*(len(t.Nodes)+len(t.Clients)))
	files = append(files, CACertPath(dir))
	for _, node := range t.Nodes {
		files = append(files, filepath.Join(dir, node.File+".crt"), filepath.Join(dir, node.File+".key"))
	}
	for _, client := range t.Clients {
		files = append(files, filepath.Join(dir, client.File+".crt"), filepath.Join(dir, client.File+".key"))
	}
	return files
}

// CACertPath returns the trust anchor's path inside a fixture directory. Every
// node's tls_ca_file and every client's RootCAs points here.
func CACertPath(dir string) string { return filepath.Join(dir, "ca.crt") }

// Ensure writes the complete fixture set for topo into dir, and is a NO-OP when
// every file is already there.
//
// The no-op matters and is not an optimization. Docker Compose starts the
// fixture service once for `up` and again for the later `run`, and a second
// generation over a complete set would mint a second root under the same
// subject name: nodes that already loaded the first one would present leaves
// the new root cannot verify, which reads as "certificate signed by unknown
// authority" against an authority the client can name. Material stays per-run
// because the volume is destroyed before each run, not because this function
// rewrites it.
//
// A partial set is treated as absent, so an interrupted generation is redone
// rather than half-used.
func Ensure(ctx context.Context, dir string, topo *Topology) error {
	if err := topo.Validate(); err != nil {
		return err
	}
	if allPresent(topo.Files(dir)) {
		return nil
	}
	return Write(ctx, dir, topo)
}

// Write issues and writes the complete fixture set for topo into dir,
// unconditionally.
func Write(ctx context.Context, dir string, topo *Topology) error {
	if err := topo.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // G301: a fixture directory read by other containers, so world-readable on purpose
		return &FileError{Op: "create directory", Path: dir, Cause: err}
	}

	authority, err := Open(ctx, Config{
		Dir:               dir,
		CommonName:        topo.CACommonName,
		Organization:      topo.Organization,
		SPIFFETrustDomain: topo.SPIFFETrustDomain,
	})
	if err != nil {
		return err
	}

	caPEM, err := authority.CACertificatePEM()
	if err != nil {
		return err
	}
	if err := writeFile(CACertPath(dir), caPEM, 0o644); err != nil {
		return err
	}

	for _, node := range topo.Nodes {
		issued, issueErr := authority.IssueServer(ctx, node)
		if issueErr != nil {
			return issueErr
		}
		if err := writePair(dir, node.File, issued); err != nil {
			return err
		}
	}

	for _, client := range topo.Clients {
		issued, issueErr := authority.IssueClient(ctx, client)
		if issueErr != nil {
			return issueErr
		}
		if err := writePair(dir, client.File, issued); err != nil {
			return err
		}
	}

	return nil
}

// writePair writes one credential's certificate and private key under the
// fixture's file stem.
func writePair(dir, stem string, issued *Issued) error {
	if err := writeFile(filepath.Join(dir, stem+".crt"), issued.CertificatePEM, 0o644); err != nil {
		return err
	}
	return writeFile(filepath.Join(dir, stem+".key"), issued.PrivateKeyPEM, 0o600)
}

// writeFile writes data to path with the given mode.
//
// Certificates are world-readable and private keys are owner-only, and both are
// read by a different container's process than the one that wrote them — which
// is why the certificate mode is 0644 rather than something tighter.
func writeFile(path string, data []byte, mode os.FileMode) error {
	if err := os.WriteFile(path, data, mode); err != nil {
		return &FileError{Op: "write", Path: path, Cause: err}
	}
	return nil
}

// allPresent reports whether every path exists.
func allPresent(files []string) bool {
	for _, f := range files {
		if _, err := os.Stat(f); err != nil {
			return false
		}
	}
	return true
}
