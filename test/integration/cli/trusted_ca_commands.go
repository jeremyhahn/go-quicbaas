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

//go:build integration

package cli

import "time"

// trustedCATimeout bounds every `quicbaas trusted-ca` invocation.
const trustedCATimeout = 15 * time.Second

// trustedCACommand builds a successful trusted-ca CommandDef with JSON output.
func trustedCACommand(name string, verbArgs ...string) CommandDef {
	args := append([]string{"trusted-ca", "--output", "json"}, verbArgs...)
	return CommandDef{
		Name:           name,
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        trustedCATimeout,
		RequiresUnseal: true,
	}
}

// TrustedCABundleCommand fetches the CA bundle.
func TrustedCABundleCommand() CommandDef {
	return trustedCACommand("trusted-ca bundle", "bundle")
}

// TrustedCACertificateCommand fetches the CA certificate.
func TrustedCACertificateCommand() CommandDef {
	return trustedCACommand("trusted-ca certificate", "certificate")
}

// TrustedCASignCSRCommand signs the PEM CSR at csrPath.
func TrustedCASignCSRCommand(csrPath string) CommandDef {
	return trustedCACommand("trusted-ca sign-csr", "sign-csr", "--csr", csrPath)
}

// TrustedCAIssueCommand issues a certificate for commonName.
func TrustedCAIssueCommand(commonName string) CommandDef {
	return trustedCACommand("trusted-ca issue", "issue", "--common-name", commonName)
}

// TrustedCARevokeCommand revokes serial.
func TrustedCARevokeCommand(serial string) CommandDef {
	return trustedCACommand("trusted-ca revoke", "revoke", serial)
}

// TrustedCAIsRevokedCommand reports whether serial is revoked.
func TrustedCAIsRevokedCommand(serial string) CommandDef {
	return trustedCACommand("trusted-ca is-revoked", "is-revoked", serial)
}

// TCGCAIssueEKCommand issues an EK certificate for the PEM public key at
// publicKeyPath.
func TCGCAIssueEKCommand(commonName, publicKeyPath string) CommandDef {
	return trustedCACommand("trusted-ca tcg ek", "tcg", "ek", "--common-name", commonName, "--public-key", publicKeyPath)
}

// trustedCAFailingCommand builds a trusted-ca CommandDef expected to fail in
// the CLI, before any request reaches the server.
func trustedCAFailingCommand(name string, verbArgs ...string) CommandDef {
	return CommandDef{
		Name:          name,
		Args:          append([]string{"trusted-ca"}, verbArgs...),
		ExpectSuccess: false,
		Timeout:       trustedCATimeout,
	}
}

// TrustedCASignCSRMissingCSRCommand omits sign-csr's --csr.
func TrustedCASignCSRMissingCSRCommand() CommandDef {
	return trustedCAFailingCommand("trusted-ca sign-csr (missing --csr)", "sign-csr")
}

// TrustedCAIssueMissingCommonNameCommand omits issue's --common-name.
func TrustedCAIssueMissingCommonNameCommand() CommandDef {
	return trustedCAFailingCommand("trusted-ca issue (missing --common-name)", "issue")
}

// TrustedCARevokeMissingSerialCommand omits revoke's <serial>.
func TrustedCARevokeMissingSerialCommand() CommandDef {
	return trustedCAFailingCommand("trusted-ca revoke (missing serial)", "revoke")
}

// TCGCAIssueEKMissingPublicKeyCommand omits tcg ek's --public-key.
func TCGCAIssueEKMissingPublicKeyCommand() CommandDef {
	return trustedCAFailingCommand("trusted-ca tcg ek (missing --public-key)", "tcg", "ek", "--common-name", "e2e-ek")
}
