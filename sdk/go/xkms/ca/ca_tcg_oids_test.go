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

package ca_test

import (
	"crypto/x509"
	"encoding/asn1"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/ca"
)

// TestTCGOIDs_AreTheUpstreamValues pins every re-exported TCG identifier to
// the go-xkms value it aliases. A re-export that drifted from its source would
// let the platform check an attestation certificate against an arc the CA
// never stamped — silently, because both sides would still "look like" TCG.
func TestTCGOIDs_AreTheUpstreamValues(t *testing.T) {
	arcs := []struct {
		name     string
		got      asn1.ObjectIdentifier
		upstream asn1.ObjectIdentifier
	}{
		{"OIDTCGKpAIKCertificate", ca.OIDTCGKpAIKCertificate, xkmssdk.OIDTCGKpAIKCertificate},
		{"OIDTCGVerifiedTPMResidency", ca.OIDTCGVerifiedTPMResidency, xkmssdk.OIDTCGVerifiedTPMResidency},
		{"OIDTCGVerifiedTPMFixed", ca.OIDTCGVerifiedTPMFixed, xkmssdk.OIDTCGVerifiedTPMFixed},
		{"OIDTCGVerifiedTPMRestricted", ca.OIDTCGVerifiedTPMRestricted, xkmssdk.OIDTCGVerifiedTPMRestricted},
	}
	for _, a := range arcs {
		if !a.got.Equal(a.upstream) {
			t.Errorf("%s = %v, upstream %v", a.name, a.got, a.upstream)
		}
	}

	policies := []struct {
		name     string
		got      x509.OID
		upstream x509.OID
	}{
		{"TCGVerifiedTPMResidencyPolicy", ca.TCGVerifiedTPMResidencyPolicy, xkmssdk.TCGVerifiedTPMResidencyPolicy},
		{"TCGVerifiedTPMFixedPolicy", ca.TCGVerifiedTPMFixedPolicy, xkmssdk.TCGVerifiedTPMFixedPolicy},
		{"TCGVerifiedTPMRestrictedPolicy", ca.TCGVerifiedTPMRestrictedPolicy, xkmssdk.TCGVerifiedTPMRestrictedPolicy},
	}
	for _, p := range policies {
		if !p.got.Equal(p.upstream) {
			t.Errorf("%s = %v, upstream %v", p.name, p.got, p.upstream)
		}
	}
}

// TestTCGOIDs_NameTheTCGArcs checks the aliases against the arcs the TCG
// specification assigns, so a wrong upstream constant is caught here rather
// than at the first rejected attestation. tcg-kp-AIKCertificate is 2.23.133.8.3;
// tcg-cap-verifiedTPM{Residency,Fixed,Restricted} are 2.23.133.11.1.{1,2,3}.
func TestTCGOIDs_NameTheTCGArcs(t *testing.T) {
	want := map[string]struct {
		oid    asn1.ObjectIdentifier
		policy x509.OID
	}{
		"2.23.133.8.3":    {oid: ca.OIDTCGKpAIKCertificate},
		"2.23.133.11.1.1": {oid: ca.OIDTCGVerifiedTPMResidency, policy: ca.TCGVerifiedTPMResidencyPolicy},
		"2.23.133.11.1.2": {oid: ca.OIDTCGVerifiedTPMFixed, policy: ca.TCGVerifiedTPMFixedPolicy},
		"2.23.133.11.1.3": {oid: ca.OIDTCGVerifiedTPMRestricted, policy: ca.TCGVerifiedTPMRestrictedPolicy},
	}
	for dotted, w := range want {
		if got := w.oid.String(); got != dotted {
			t.Errorf("asn1 arc = %s, want %s", got, dotted)
		}
		if w.policy.String() == "" {
			continue // the AIK EKU has no certificate-policy form
		}
		if got := w.policy.String(); got != dotted {
			t.Errorf("x509 policy arc = %s, want %s", got, dotted)
		}
		if !w.policy.EqualASN1OID(w.oid) {
			t.Errorf("x509 policy %s and asn1 arc %s disagree", w.policy, w.oid)
		}
	}
}
