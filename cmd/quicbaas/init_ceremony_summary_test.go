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

package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// ceremonyInitializeSecretFields names the JSON fields of
// CeremonyInitializeResponse that carry secret material and so must never
// reach the --output json summary (Codex 7595 P1 1, #1927).
var ceremonyInitializeSecretFields = map[string]bool{
	"admin_key_pem": true,
}

// jsonFieldNames returns the JSON names of a struct type's exported fields.
func jsonFieldNames(t *testing.T, typ reflect.Type) []string {
	t.Helper()
	var names []string
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if !f.IsExported() {
			continue
		}
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// TestFormatCeremonyInitializeResponse_JSONIsThePublicSummary verifies the JSON
// output carries exactly the public summary fields and their values.
func TestFormatCeremonyInitializeResponse_JSONIsThePublicSummary(t *testing.T) {
	resp := &qrdbtransport.CeremonyInitializeResponse{
		SPKIPin:    "pin",
		CACertPEM:  "-----BEGIN CERTIFICATE-----\nca\n-----END CERTIFICATE-----\n",
		TLSCertPEM: "-----BEGIN CERTIFICATE-----\ntls\n-----END CERTIFICATE-----\n",
		State:      "enrolling",
		ShareCount: 3,
	}
	var out bytes.Buffer
	require.NoError(t, formatCeremonyInitializeResponse(resp, "json", &out))

	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(out.Bytes(), &got))

	keys := make([]string, 0, len(got))
	for k := range got {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	assert.Equal(t, jsonFieldNames(t, reflect.TypeOf(ceremonyInitializeSummary{})), keys)
	assert.Equal(t, "pin", got["spki_pin"])
	assert.Equal(t, resp.CACertPEM, got["ca_cert_pem"])
	assert.Equal(t, resp.TLSCertPEM, got["tls_cert_pem"])
	assert.Equal(t, "enrolling", got["state"])
	assert.EqualValues(t, 3, got["share_count"])
	assert.NotContains(t, out.String(), "PRIVATE KEY")
}

// TestCeremonyInitializeSummary_ClassifiesEveryResponseField is the guard for
// the go-qrdb pin bump: every JSON field of the pinned response is either in
// the public summary or on the secret deny-list, and no secret field is in the
// summary. A new upstream field fails here until it is classified, so the JSON
// output cannot silently start carrying key material.
func TestCeremonyInitializeSummary_ClassifiesEveryResponseField(t *testing.T) {
	public := map[string]bool{}
	for _, name := range jsonFieldNames(t, reflect.TypeOf(ceremonyInitializeSummary{})) {
		require.False(t, ceremonyInitializeSecretFields[name],
			"secret field %q must not be in the JSON summary", name)
		public[name] = true
	}
	for _, name := range jsonFieldNames(t, reflect.TypeOf(qrdbtransport.CeremonyInitializeResponse{})) {
		assert.True(t, public[name] || ceremonyInitializeSecretFields[name],
			"CeremonyInitializeResponse field %q is unclassified: add it to ceremonyInitializeSummary "+
				"if it is public, or to ceremonyInitializeSecretFields if it carries secret material", name)
	}
}
