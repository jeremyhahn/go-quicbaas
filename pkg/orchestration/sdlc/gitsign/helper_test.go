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
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha512"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gossh "golang.org/x/crypto/ssh"
)

// errRefused stands in for quicbaas refusing a call.
var errRefused = errors.New("refused")

// remoteKey is the session key as the helper sees it: a signer it can ask for
// signatures and the connection that reached it. It records whether the helper
// opened and closed the connection.
type remoteKey struct {
	signer   gossh.Signer
	openErr  error
	closeErr error
	opened   bool
	closed   bool
}

func (k *remoteKey) open(context.Context) (gossh.Signer, io.Closer, error) {
	k.opened = true
	if k.openErr != nil {
		return nil, nil, k.openErr
	}
	return k.signer, k, nil
}

func (k *remoteKey) Close() error {
	k.closed = true
	return k.closeErr
}

// failingSigner is a key whose every signature request is refused.
type failingSigner struct{ gossh.Signer }

func (s failingSigner) Sign(io.Reader, []byte) (*gossh.Signature, error) { return nil, errRefused }

// lyingSigner claims one key and signs with another.
type lyingSigner struct {
	claimed gossh.PublicKey
	actual  gossh.Signer
}

func (s lyingSigner) PublicKey() gossh.PublicKey { return s.claimed }

func (s lyingSigner) Sign(r io.Reader, data []byte) (*gossh.Signature, error) {
	return s.actual.Sign(r, data)
}

// failingWriter is a stdout that cannot be written.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errRefused }

func newEd25519Signer(t *testing.T) gossh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signer, err := gossh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	return signer
}

// writeFile writes content to name in dir and returns its path.
func writeFile(t *testing.T, dir, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// writeKeyFile writes pub as git does for a literal user.signingkey.
func writeKeyFile(t *testing.T, dir string, pub gossh.PublicKey) string {
	t.Helper()
	return writeFile(t, dir, "key.pub", gossh.MarshalAuthorizedKey(pub))
}

// signArgs is git's invocation for the key file and data file.
func signArgs(keyFile, dataFile string) []string {
	args := []string{"-Y", "sign", "-n", "git", "-f", keyFile, "-U"}
	if dataFile != "" {
		args = append(args, dataFile)
	}
	return args
}

// verifyArmored decodes an armored SSHSIG and checks it is pub's signature
// over message in the git namespace.
func verifyArmored(t *testing.T, armored []byte, pub gossh.PublicKey, message []byte) {
	t.Helper()
	text := strings.TrimSpace(string(armored))
	lines := strings.Split(text, "\n")
	if lines[0] != armorBegin || lines[len(lines)-1] != armorEnd {
		t.Fatalf("not armored:\n%s", text)
	}
	for _, line := range lines[1 : len(lines)-1] {
		if len(line) > armorWidth {
			t.Fatalf("armor line is %d columns, want at most %d", len(line), armorWidth)
		}
	}
	blob, err := base64.StdEncoding.DecodeString(strings.Join(lines[1:len(lines)-1], ""))
	if err != nil {
		t.Fatalf("decode armor: %v", err)
	}
	if !bytes.HasPrefix(blob, []byte(sigMagic)) {
		t.Fatalf("blob lacks the %s magic", sigMagic)
	}
	var fields struct {
		Version       uint32
		PublicKey     []byte
		Namespace     string
		Reserved      string
		HashAlgorithm string
		Signature     []byte
	}
	if err := gossh.Unmarshal(blob[len(sigMagic):], &fields); err != nil {
		t.Fatalf("unmarshal blob: %v", err)
	}
	if fields.Version != sigVersion || fields.Namespace != GitNamespace || fields.HashAlgorithm != hashAlgorithm {
		t.Fatalf("unexpected framing: version %d namespace %q hash %q", fields.Version, fields.Namespace, fields.HashAlgorithm)
	}
	if !bytes.Equal(fields.PublicKey, pub.Marshal()) {
		t.Fatal("blob names a different public key")
	}
	var sig gossh.Signature
	if err := gossh.Unmarshal(fields.Signature, &sig); err != nil {
		t.Fatalf("unmarshal signature: %v", err)
	}
	digest := sha512.Sum512(message)
	if err := pub.Verify(signedData(GitNamespace, digest[:]), &sig); err != nil {
		t.Fatalf("signature does not verify: %v", err)
	}
}

func TestRunSignsDataFile(t *testing.T) {
	dir := t.TempDir()
	key := &remoteKey{signer: newEd25519Signer(t)}
	message := []byte("tree 4b825dc642cb6eb9a060e54bf8d69288fbee4904\n\ncommit message\n")
	keyFile := writeKeyFile(t, dir, key.signer.PublicKey())
	dataFile := writeFile(t, dir, "buffer", message)

	helper := &Helper{Open: key.open}
	if err := helper.Run(context.Background(), signArgs(keyFile, dataFile)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	armored, err := os.ReadFile(dataFile + signatureSuffix)
	if err != nil {
		t.Fatalf("read signature: %v", err)
	}
	verifyArmored(t, armored, key.signer.PublicKey(), message)
	if !key.closed {
		t.Fatal("the quicbaas connection was not closed")
	}
}

func TestRunSignsStdinToStdout(t *testing.T) {
	dir := t.TempDir()
	key := &remoteKey{signer: newEd25519Signer(t)}
	message := bytes.Repeat([]byte("long data that wraps the armor "), 64)
	var stdout bytes.Buffer

	helper := &Helper{Stdin: bytes.NewReader(message), Stdout: &stdout, Open: key.open}
	if err := helper.Run(context.Background(), signArgs(writeKeyFile(t, dir, key.signer.PublicKey()), "")); err != nil {
		t.Fatalf("Run: %v", err)
	}
	verifyArmored(t, stdout.Bytes(), key.signer.PublicKey(), message)
}

// TestRunFailuresWriteNoSignature covers every failure after the invocation is
// parsed: none may leave a .sig for git to pick up.
func TestRunFailuresWriteNoSignature(t *testing.T) {
	signer := newEd25519Signer(t)
	other := newEd25519Signer(t)
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	rsaSigner, err := gossh.NewSignerFromKey(rsaKey)
	if err != nil {
		t.Fatalf("rsa signer: %v", err)
	}

	cases := []struct {
		name       string
		key        *remoteKey
		keyFile    func(t *testing.T, dir string) string
		args       func(keyFile, dataFile string) []string
		wantTarget any
		wantOpened bool
	}{
		{
			name:       "malformed invocation",
			key:        &remoteKey{signer: signer},
			args:       func(k, d string) []string { return []string{"-Y", "verify", "-n", "git", "-f", k, d} },
			wantTarget: new(*InvalidInvocationError),
		},
		{
			name:       "key file missing",
			key:        &remoteKey{signer: signer},
			keyFile:    func(t *testing.T, dir string) string { return filepath.Join(dir, "absent.pub") },
			wantTarget: new(*KeyFileError),
		},
		{
			name:       "key file holds no public key",
			key:        &remoteKey{signer: signer},
			keyFile:    func(t *testing.T, dir string) string { return writeFile(t, dir, "junk.pub", []byte("not a key\n")) },
			wantTarget: new(*KeyFileError),
		},
		{
			name:       "data file missing",
			key:        &remoteKey{signer: signer},
			args:       func(k, d string) []string { return signArgs(k, d+".absent") },
			wantTarget: new(*IOError),
		},
		{
			name:       "quicbaas unreachable",
			key:        &remoteKey{openErr: errRefused},
			wantTarget: new(*SignError),
			wantOpened: true,
		},
		{
			name:       "session key is not the key git named",
			key:        &remoteKey{signer: other},
			wantTarget: new(*KeyMismatchError),
			wantOpened: true,
		},
		{
			name:       "quicbaas refuses the signature",
			key:        &remoteKey{signer: failingSigner{signer}},
			wantTarget: new(*SignError),
			wantOpened: true,
		},
		{
			name:       "signature does not verify",
			key:        &remoteKey{signer: lyingSigner{claimed: signer.PublicKey(), actual: other}},
			wantTarget: new(*SignError),
			wantOpened: true,
		},
		{
			name: "sha-1 rsa signature",
			key:  &remoteKey{signer: rsaSigner},
			keyFile: func(t *testing.T, dir string) string {
				return writeFile(t, dir, "rsa.pub", gossh.MarshalAuthorizedKey(rsaSigner.PublicKey()))
			},
			wantTarget: new(*SignError),
			wantOpened: true,
		},
		{
			name:       "connection fails to close",
			key:        &remoteKey{signer: signer, closeErr: errRefused},
			wantTarget: new(*SignError),
			wantOpened: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			keyFile := writeKeyFile(t, dir, signer.PublicKey())
			if tc.keyFile != nil {
				keyFile = tc.keyFile(t, dir)
			}
			dataFile := writeFile(t, dir, "buffer", []byte("data"))
			args := signArgs(keyFile, dataFile)
			if tc.args != nil {
				args = tc.args(keyFile, dataFile)
			}

			err := (&Helper{Open: tc.key.open}).Run(context.Background(), args)
			if !errors.As(err, tc.wantTarget) {
				t.Fatalf("error %v is not a %T", err, tc.wantTarget)
			}
			if tc.key.opened != tc.wantOpened {
				t.Fatalf("opened = %v, want %v", tc.key.opened, tc.wantOpened)
			}
			if _, statErr := os.Stat(dataFile + signatureSuffix); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("a signature file was left behind: %v", statErr)
			}
		})
	}
}

func TestRunReportsUnwritableSignature(t *testing.T) {
	signer := newEd25519Signer(t)

	t.Run("signature file", func(t *testing.T) {
		dir := t.TempDir()
		dataFile := writeFile(t, dir, "buffer", []byte("data"))
		if err := os.Mkdir(dataFile+signatureSuffix, 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		err := (&Helper{Open: (&remoteKey{signer: signer}).open}).Run(context.Background(), signArgs(writeKeyFile(t, dir, signer.PublicKey()), dataFile))
		var ioErr *IOError
		if !errors.As(err, &ioErr) || ioErr.Op != "write signature" {
			t.Fatalf("error %v is not a write *IOError", err)
		}
	})

	t.Run("stdout", func(t *testing.T) {
		dir := t.TempDir()
		helper := &Helper{Stdin: strings.NewReader("data"), Stdout: failingWriter{}, Open: (&remoteKey{signer: signer}).open}
		err := helper.Run(context.Background(), signArgs(writeKeyFile(t, dir, signer.PublicKey()), ""))
		if !errors.Is(err, ErrIO) {
			t.Fatalf("error %v does not unwrap to ErrIO", err)
		}
	})
}

func TestArmorWrapsAtSeventyColumns(t *testing.T) {
	cases := map[string]int{"empty": 0, "one line": 51, "two lines": 52, "many lines": 400}
	for name, size := range cases {
		t.Run(name, func(t *testing.T) {
			blob := bytes.Repeat([]byte{0xa5}, size)
			lines := strings.Split(strings.TrimSuffix(string(armor(blob)), "\n"), "\n")
			if lines[0] != armorBegin || lines[len(lines)-1] != armorEnd {
				t.Fatalf("bad armor lines %q", lines)
			}
			body := strings.Join(lines[1:len(lines)-1], "")
			for _, line := range lines[1 : len(lines)-1] {
				if len(line) > armorWidth {
					t.Fatalf("line of %d columns", len(line))
				}
			}
			decoded, err := base64.StdEncoding.DecodeString(body)
			if err != nil || !bytes.Equal(decoded, blob) {
				t.Fatalf("armor does not round-trip: %v", err)
			}
		})
	}
}
