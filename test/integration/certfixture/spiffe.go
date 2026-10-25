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
	"strings"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// SPIFFEName derives the name segment of a fixture's SPIFFE ID from its
// subject common name: the local part before the first "@", with every
// character outside [A-Za-z0-9._-] replaced by "-". "admin@integration.test"
// becomes "admin" and "test-node1" stays "test-node1".
//
// The common name and any email SAN keep their full value, because the suite
// looks principals up by them. Only the SPIFFE ID uses this name: go-qrdb
// v0.51 builds a principal as spiffe://<tenant>/<kind>/<name> and refuses a
// name segment holding any other character.
func SPIFFEName(commonName string) string {
	local, _, _ := strings.Cut(commonName, "@")
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			return r
		}
		return '-'
	}, local)
}

// spiffeURISAN returns the transport SAN entry ("URI:spiffe://...") for the
// SPIFFE ID spiffe://<tenantID>/<kind>/<SPIFFEName(commonName)>. The tenant
// is the trust domain.
func spiffeURISAN(tenantID, kind, commonName string) (string, error) {
	id, err := qrdbsdk.SpiffeID{TrustDomain: tenantID, Kind: kind, Name: SPIFFEName(commonName)}.URI()
	if err != nil {
		return "", err
	}
	return "URI:" + id.String(), nil
}

// spiffeNames records the SPIFFE ID each fixture of a topology will carry, so
// two fixtures that derive the same ID are refused before anything is issued.
type spiffeNames map[string]string

// claim records commonName under spiffe://<tenantID>/<kind>/<name>, or
// returns a *SPIFFENameCollisionError naming the fixture that already holds
// that ID.
func (n spiffeNames) claim(tenantID, kind, commonName string) error {
	name := SPIFFEName(commonName)
	key := tenantID + "/" + kind + "/" + name
	if first, taken := n[key]; taken {
		return &SPIFFENameCollisionError{
			TenantID: tenantID, Kind: kind, Name: name,
			First: first, Second: commonName,
		}
	}
	n[key] = commonName
	return nil
}
