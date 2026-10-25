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

package server

import (
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// NewCAService exposes the CA issuance adapter the daemon serves every
// certificate operation through — REST, gRPC, QUIC, MCP, Unix and embedded all
// reach the CA via this one type — so a caller outside the server can issue
// through the SAME path instead of assembling its own.
//
// Why this is exported. go-quicbaas#943 found a 444-line parallel certificate
// authority under test/integration minting the whole integration cluster's TLS
// material with raw crypto/ecdsa and crypto/x509. The suite therefore proved
// the system worked against certificates no part of the product issues, and any
// divergence in extensions, SPIFFE URI shape, key usage or chain construction
// was invisible to every test. The fixtures now come from here
// (test/integration/certfixture), which means the material the cluster serves
// is built by the adapter that ships.
//
// spiffeTrustDomain is the operator-configured SPIFFE trust domain. When it is
// non-empty AND a request carries a TenantID, the adapter has the CA stamp a
// tenant-scoped SPIFFE URI SAN (spiffe://<id>/<kind>/<cn>)
// onto the leaf, which is what lets mTLS/PIV login derive the tenant from the
// verified peer certificate. Empty disables SPIFFE synthesis.
//
// Returns ErrCAServiceAdapterNilCA when ca is nil.
func NewCAService(ca xkmssdk.TrustedCA, spiffeTrustDomain string) (xkmstransport.CAService, error) {
	adapter, err := newCAServiceAdapter(ca)
	if err != nil {
		return nil, err
	}
	adapter.SetSPIFFETrustDomain(spiffeTrustDomain)
	return adapter, nil
}
