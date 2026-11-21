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

// Package gitsign is git's gpg.ssh.program for a quicbaas signing session.
//
// git asks the program for an SSHSIG over each commit or tag it signs. The
// helper hashes the data, asks quicbaas to sign the SSHSIG preamble with the
// session's key over the session's own MCP connection, checks the signature
// against the public key git named, and writes the armored SSHSIG where git
// expects it. The private key stays in quicbaas: the helper only ever holds the
// public key and the signature, and quicbaas authorizes and audits each call
// against the session's principal.
package gitsign

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha512"
	"io"
	"os"

	gossh "golang.org/x/crypto/ssh"
)

// signatureSuffix is appended to the data file name to name the signature
// file, as ssh-keygen does.
const signatureSuffix = ".sig"

// signatureFileMode is the signature file's mode. git reads it as the same user
// and removes it once read.
const signatureFileMode = 0o600

// OpenSigner returns the session's remote signer and the connection behind it.
type OpenSigner func(ctx context.Context) (gossh.Signer, io.Closer, error)

// Helper runs one gpg.ssh.program invocation.
type Helper struct {
	// Stdin is read when git passes no data file, Stdout written then.
	Stdin  io.Reader
	Stdout io.Writer

	// Open reaches quicbaas. It is called only once the invocation and the key
	// file are valid, so a malformed call never reaches quicbaas.
	Open OpenSigner
}

// Run validates args, signs the data and writes the armored signature. No
// signature is written unless the whole call succeeds.
func (h *Helper) Run(ctx context.Context, args []string) error {
	inv, err := ParseInvocation(args)
	if err != nil {
		return err
	}
	pub, err := readPublicKey(inv.KeyFile)
	if err != nil {
		return err
	}
	digest, err := h.hashData(inv.DataFile)
	if err != nil {
		return err
	}
	toSign := signedData(inv.Namespace, digest)
	sig, err := h.sign(ctx, pub, toSign)
	if err != nil {
		return err
	}
	return h.writeSignature(inv.DataFile, armor(encodeSignature(pub, inv.Namespace, sig)))
}

// sign asks the session's signer for a signature over toSign and checks it is
// one git can verify against pub.
func (h *Helper) sign(ctx context.Context, pub gossh.PublicKey, toSign []byte) (*gossh.Signature, error) {
	signer, conn, err := h.Open(ctx)
	if err != nil {
		return nil, &SignError{Message: "reach quicbaas", Cause: err}
	}
	sig, signErr := signRemote(signer, pub, toSign)
	if closeErr := conn.Close(); closeErr != nil && signErr == nil {
		return nil, &SignError{Message: "close the quicbaas connection", Cause: closeErr}
	}
	return sig, signErr
}

// signRemote signs toSign with signer, which must hold pub.
func signRemote(signer gossh.Signer, pub gossh.PublicKey, toSign []byte) (*gossh.Signature, error) {
	if got := signer.PublicKey(); !bytes.Equal(got.Marshal(), pub.Marshal()) {
		return nil, &KeyMismatchError{Want: gossh.FingerprintSHA256(pub), Got: gossh.FingerprintSHA256(got)}
	}
	sig, err := signer.Sign(rand.Reader, toSign)
	if err != nil {
		return nil, &SignError{Message: "quicbaas refused or failed the signature", Cause: err}
	}
	if !signatureFormats[sig.Format] {
		return nil, &SignError{Message: "unsupported signature format " + sig.Format}
	}
	if err := pub.Verify(toSign, sig); err != nil {
		return nil, &SignError{Message: "the signature does not verify against " + gossh.FingerprintSHA256(pub), Cause: err}
	}
	return sig, nil
}

// readPublicKey parses the public key git wrote to path.
func readPublicKey(path string) (gossh.PublicKey, error) {
	line, err := os.ReadFile(path) // #nosec G304 -- git names the key file; it holds a public key.
	if err != nil {
		return nil, &KeyFileError{Path: path, Cause: err}
	}
	pub, _, _, _, err := gossh.ParseAuthorizedKey(line)
	if err != nil {
		return nil, &KeyFileError{Path: path, Cause: err}
	}
	return pub, nil
}

// hashData is the sha512 of the data file, or of standard input when path is
// empty. The data is streamed, never held whole.
func (h *Helper) hashData(path string) ([]byte, error) {
	source, name, closeSource, err := h.openData(path)
	if err != nil {
		return nil, err
	}
	hash := sha512.New()
	_, copyErr := io.Copy(hash, source)
	closeErr := closeSource()
	if copyErr != nil {
		return nil, &IOError{Op: "read data", Path: name, Cause: copyErr}
	}
	if closeErr != nil {
		return nil, &IOError{Op: "read data", Path: name, Cause: closeErr}
	}
	return hash.Sum(nil), nil
}

// openData opens the data to sign and returns its display name and a func
// that releases it.
func (h *Helper) openData(path string) (io.Reader, string, func() error, error) {
	if path == "" {
		return h.Stdin, "(stdin)", func() error { return nil }, nil
	}
	file, err := os.Open(path) // #nosec G304 -- git names the data file it wants signed.
	if err != nil {
		return nil, path, nil, &IOError{Op: "read data", Path: path, Cause: err}
	}
	return file, path, file.Close, nil
}

// writeSignature writes the armored signature beside the data file, or to
// standard output when the data came from standard input.
func (h *Helper) writeSignature(dataFile string, armored []byte) error {
	if dataFile == "" {
		if _, err := h.Stdout.Write(armored); err != nil {
			return &IOError{Op: "write signature", Path: "(stdout)", Cause: err}
		}
		return nil
	}
	path := dataFile + signatureSuffix
	if err := os.WriteFile(path, armored, signatureFileMode); err != nil {
		return &IOError{Op: "write signature", Path: path, Cause: err}
	}
	return nil
}
