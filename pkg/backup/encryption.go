// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package backup

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"

	xkms "github.com/jeremyhahn/go-xkms/sdk/go"
)

// Encryptor provides encryption functionality.
type Encryptor interface {
	// Encrypt wraps a writer with encryption.
	Encrypt(w io.Writer) (io.WriteCloser, error)

	// Decrypt wraps a reader with decryption.
	Decrypt(r io.Reader) (io.ReadCloser, error)
}

// NewEncryptor creates an encryptor for the given type.
func NewEncryptor(etype EncryptionType, key []byte) (Encryptor, error) {
	switch etype {
	case EncryptionNone:
		return &noneEncryptor{}, nil
	case EncryptionAES256GCM:
		if len(key) != xkms.AESGCMKeySize {
			return nil, api.NewKeySizeError("backup.NewEncryptor", "AES-256", xkms.AESGCMKeySize, len(key))
		}
		return NewAES256GCMEncryptor(key)
	default:
		return nil, api.NewUnsupportedValueError("backup.NewEncryptor", "encryption type", string(etype))
	}
}

// noneEncryptor is a no-op encryptor.
type noneEncryptor struct{}

func (e *noneEncryptor) Encrypt(w io.Writer) (io.WriteCloser, error) {
	return &nopWriteCloser{w}, nil
}

func (e *noneEncryptor) Decrypt(r io.Reader) (io.ReadCloser, error) {
	if rc, ok := r.(io.ReadCloser); ok {
		return rc, nil
	}
	return io.NopCloser(r), nil
}

// aes256gcmEncryptor implements AES-256-GCM encryption using xkms AES-256-GCM.
//
// Wire format: [nonce:12][ciphertext+tag]
//
// On Encrypt, the nonce is generated and written eagerly to the underlying writer.
// Plaintext is buffered in Write() and encrypted on Close() using the pre-generated nonce.
// Decryption eagerly reads the entire stream, validates the nonce size, and decrypts on Read().
type aes256gcmEncryptor struct {
	key []byte
}

// NewAES256GCMEncryptor creates a new AES-256-GCM encryptor.
func NewAES256GCMEncryptor(key []byte) (*aes256gcmEncryptor, error) {
	if len(key) != xkms.AESGCMKeySize {
		return nil, api.NewKeySizeError("backup.NewAES256GCMEncryptor", "AES-256", xkms.AESGCMKeySize, len(key))
	}

	return &aes256gcmEncryptor{
		key: key,
	}, nil
}

// Encrypt creates an encrypting writer. The nonce is generated and written eagerly
// to the underlying writer. If the nonce write fails, an error is returned immediately.
func (e *aes256gcmEncryptor) Encrypt(w io.Writer) (io.WriteCloser, error) {
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return nil, api.NewCipherError("backup.aes256gcmEncryptor.Encrypt", "create cipher", 0, err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, api.NewCipherError("backup.aes256gcmEncryptor.Encrypt", "create GCM", 0, err)
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, &NonceGenerateError{Cause: err}
	}

	if _, err := w.Write(nonce); err != nil {
		return nil, &NonceWriteError{Cause: err}
	}

	return &gcmWriter{
		writer: w,
		key:    e.key,
		nonce:  nonce,
		buf:    make([]byte, 0, 4096),
	}, nil
}

// Decrypt creates a decrypting reader. The input data is read eagerly to validate
// that it contains at least enough bytes for the nonce. If the data is too short,
// an error is returned immediately.
func (e *aes256gcmEncryptor) Decrypt(r io.Reader) (io.ReadCloser, error) {
	sealed, readErr := io.ReadAll(r)

	if len(sealed) < xkms.AESGCMNonceSize {
		return nil, &NonceTooShortError{
			Expected: xkms.AESGCMNonceSize,
			Actual:   len(sealed),
		}
	}

	return &gcmReader{
		key:     e.key,
		sealed:  sealed,
		readErr: readErr,
	}, nil
}

// gcmWriter buffers data and encrypts on close using the pre-generated nonce.
type gcmWriter struct {
	writer io.Writer
	key    []byte
	nonce  []byte
	buf    []byte
	closed bool
}

func (w *gcmWriter) Write(p []byte) (n int, err error) {
	if w.closed {
		return 0, api.NewWriterClosedError("backup.gcmWriter.Write")
	}
	w.buf = append(w.buf, p...)
	return len(p), nil
}

func (w *gcmWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true

	block, err := aes.NewCipher(w.key)
	if err != nil {
		return api.NewCipherError("backup.gcmWriter.Close", "create cipher", 0, err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return api.NewCipherError("backup.gcmWriter.Close", "create GCM", 0, err)
	}

	// Encrypt using the pre-generated nonce. Seal appends ciphertext+tag.
	ciphertext := aead.Seal(nil, w.nonce, w.buf, nil)

	// Zero the plaintext buffer.
	xkms.SecureMemZero(w.buf)

	// Write the ciphertext+tag (nonce was already written eagerly in Encrypt).
	if _, err := w.writer.Write(ciphertext); err != nil {
		return api.NewWriteError("backup.gcmWriter.Close", "ciphertext", err)
	}

	return nil
}

// gcmReader decrypts data that was eagerly read from the input stream.
type gcmReader struct {
	key       []byte
	sealed    []byte // The complete sealed data: [nonce][ciphertext+tag]
	readErr   error  // Any error from the initial read
	plaintext []byte // Decrypted plaintext, populated on first Read
	offset    int
	decrypted bool
}

func (r *gcmReader) Read(p []byte) (n int, err error) {
	if !r.decrypted {
		r.decrypted = true

		// If there was a read error during eager read, surface it now.
		if r.readErr != nil {
			return 0, api.NewReadError("backup.gcmReader.Read", "ciphertext", r.readErr)
		}

		// Decrypt using xkms AES-256-GCM (expects [nonce][ciphertext+tag]).
		plaintext, err := xkms.AESGCMDecrypt(r.key, r.sealed)
		if err != nil {
			return 0, api.NewCipherError("backup.gcmReader.Read", "decrypt", 0, err)
		}

		r.plaintext = plaintext
	}

	if r.offset >= len(r.plaintext) {
		return 0, io.EOF
	}

	// Copy from decrypted buffer.
	n = copy(p, r.plaintext[r.offset:])
	r.offset += n

	if r.offset >= len(r.plaintext) {
		return n, io.EOF
	}

	return n, nil
}

func (r *gcmReader) Close() error {
	// Zero the decrypted buffer when closing.
	if r.plaintext != nil {
		xkms.SecureMemZero(r.plaintext)
	}
	return nil
}

// ComputeChecksum computes SHA-256 checksum of data.
func ComputeChecksum(data []byte) string {
	hash := sha256.Sum256(data)
	return fmt.Sprintf("%x", hash)
}

// VerifyChecksum verifies SHA-256 checksum of data.
func VerifyChecksum(data []byte, expectedChecksum string) bool {
	actualChecksum := ComputeChecksum(data)
	return actualChecksum == expectedChecksum
}
