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

package gitsign

import (
	"bytes"
	"encoding/base64"

	gossh "golang.org/x/crypto/ssh"
)

// SSHSIG framing, from OpenSSH's PROTOCOL.sshsig.
const (
	sigMagic      = "SSHSIG"
	sigVersion    = 1
	hashAlgorithm = "sha512"
	armorBegin    = "-----BEGIN SSH SIGNATURE-----"
	armorEnd      = "-----END SSH SIGNATURE-----"
	armorWidth    = 70
)

// signatureFormats are the signature algorithms the helper writes. ssh-rsa is
// absent on purpose: it is RSA over SHA-1, which ssh-keygen and git refuse to
// verify by default.
var signatureFormats = map[string]bool{
	gossh.KeyAlgoED25519:   true,
	gossh.KeyAlgoECDSA256:  true,
	gossh.KeyAlgoECDSA384:  true,
	gossh.KeyAlgoECDSA521:  true,
	gossh.KeyAlgoRSASHA256: true,
	gossh.KeyAlgoRSASHA512: true,
}

// signedData is the blob the key signs for a message whose sha512 is digest:
// the magic preamble, the namespace, an empty reserved field, the hash
// algorithm and the digest.
func signedData(namespace string, digest []byte) []byte {
	return append([]byte(sigMagic), gossh.Marshal(struct {
		Namespace     string
		Reserved      string
		HashAlgorithm string
		Hash          []byte
	}{namespace, "", hashAlgorithm, digest})...)
}

// encodeSignature is the SSHSIG blob carrying sig, made by pub.
func encodeSignature(pub gossh.PublicKey, namespace string, sig *gossh.Signature) []byte {
	return append([]byte(sigMagic), gossh.Marshal(struct {
		Version       uint32
		PublicKey     []byte
		Namespace     string
		Reserved      string
		HashAlgorithm string
		Signature     []byte
	}{sigVersion, pub.Marshal(), namespace, "", hashAlgorithm, gossh.Marshal(sig)})...)
}

// armor wraps an SSHSIG blob in the PEM-like armor ssh-keygen writes: base64
// broken at 70 columns between BEGIN and END lines.
func armor(blob []byte) []byte {
	encoded := base64.StdEncoding.EncodeToString(blob)
	var out bytes.Buffer
	out.WriteString(armorBegin + "\n")
	for len(encoded) > armorWidth {
		out.WriteString(encoded[:armorWidth] + "\n")
		encoded = encoded[armorWidth:]
	}
	out.WriteString(encoded + "\n")
	out.WriteString(armorEnd + "\n")
	return out.Bytes()
}
