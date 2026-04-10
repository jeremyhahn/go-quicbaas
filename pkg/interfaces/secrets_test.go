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

package interfaces

import (
	"testing"
	"time"
)

// TestEncryptedBlobValidate tests the Validate method on EncryptedBlob.
func TestEncryptedBlobValidate(t *testing.T) {
	tests := []struct {
		name    string
		blob    *EncryptedBlob
		wantErr error
	}{
		{
			name: "valid AES-256-GCM blob",
			blob: &EncryptedBlob{
				KeyIdentifier: "dek:global",
				KeyVersion:    1,
				Algorithm:     AlgorithmAES256GCM,
				Nonce:         make([]byte, 12),
				Ciphertext:    []byte("encrypted data"),
				Tag:           make([]byte, 16),
				AAD:           []byte("/secrets/myapp/api-key"),
				EncryptedAt:   time.Now(),
			},
			wantErr: nil,
		},
		{
			name: "valid AES-128-GCM blob",
			blob: &EncryptedBlob{
				KeyIdentifier: "dek:tenant:acme",
				KeyVersion:    2,
				Algorithm:     AlgorithmAES128GCM,
				Nonce:         make([]byte, 12),
				Ciphertext:    []byte("data"),
				Tag:           make([]byte, 16),
				AAD:           []byte("/path"),
				EncryptedAt:   time.Now(),
			},
			wantErr: nil,
		},
		{
			name: "valid ChaCha20-Poly1305 blob with 12-byte nonce",
			blob: &EncryptedBlob{
				KeyIdentifier: "xkms:aws:kms:alias/mykey",
				KeyVersion:    1,
				Algorithm:     AlgorithmChaCha20Poly1305,
				Nonce:         make([]byte, 12),
				Ciphertext:    []byte("test"),
				Tag:           make([]byte, 16),
				AAD:           nil,
				EncryptedAt:   time.Now(),
			},
			wantErr: nil,
		},
		{
			name: "valid ChaCha20-Poly1305 blob with 24-byte nonce",
			blob: &EncryptedBlob{
				KeyIdentifier: "dek:global",
				KeyVersion:    1,
				Algorithm:     AlgorithmChaCha20Poly1305,
				Nonce:         make([]byte, 24),
				Ciphertext:    []byte("test"),
				Tag:           make([]byte, 16),
				AAD:           []byte("/path"),
				EncryptedAt:   time.Now(),
			},
			wantErr: nil,
		},
		{
			name:    "nil blob",
			blob:    nil,
			wantErr: ErrInvalidBlob,
		},
		{
			name: "missing key identifier",
			blob: &EncryptedBlob{
				KeyIdentifier: "",
				Algorithm:     AlgorithmAES256GCM,
				Nonce:         make([]byte, 12),
				Ciphertext:    []byte("data"),
				Tag:           make([]byte, 16),
				EncryptedAt:   time.Now(),
			},
			wantErr: ErrInvalidKeyIdentifier,
		},
		{
			name: "missing algorithm",
			blob: &EncryptedBlob{
				KeyIdentifier: "dek:global",
				Algorithm:     "",
				Nonce:         make([]byte, 12),
				Ciphertext:    []byte("data"),
				Tag:           make([]byte, 16),
				EncryptedAt:   time.Now(),
			},
			wantErr: ErrUnsupportedAlgorithm,
		},
		{
			name: "unsupported algorithm",
			blob: &EncryptedBlob{
				KeyIdentifier: "dek:global",
				Algorithm:     "DES",
				Nonce:         make([]byte, 12),
				Ciphertext:    []byte("data"),
				Tag:           make([]byte, 16),
				EncryptedAt:   time.Now(),
			},
			wantErr: ErrUnsupportedAlgorithm,
		},
		{
			name: "missing nonce",
			blob: &EncryptedBlob{
				KeyIdentifier: "dek:global",
				Algorithm:     AlgorithmAES256GCM,
				Nonce:         nil,
				Ciphertext:    []byte("data"),
				Tag:           make([]byte, 16),
				EncryptedAt:   time.Now(),
			},
			wantErr: ErrInvalidBlob,
		},
		{
			name: "invalid nonce size for AES-GCM",
			blob: &EncryptedBlob{
				KeyIdentifier: "dek:global",
				Algorithm:     AlgorithmAES256GCM,
				Nonce:         make([]byte, 16), // Should be 12
				Ciphertext:    []byte("data"),
				Tag:           make([]byte, 16),
				EncryptedAt:   time.Now(),
			},
			wantErr: ErrInvalidBlob,
		},
		{
			name: "invalid nonce size for ChaCha20-Poly1305",
			blob: &EncryptedBlob{
				KeyIdentifier: "dek:global",
				Algorithm:     AlgorithmChaCha20Poly1305,
				Nonce:         make([]byte, 16), // Should be 12 or 24
				Ciphertext:    []byte("data"),
				Tag:           make([]byte, 16),
				EncryptedAt:   time.Now(),
			},
			wantErr: ErrInvalidBlob,
		},
		{
			name: "missing ciphertext",
			blob: &EncryptedBlob{
				KeyIdentifier: "dek:global",
				Algorithm:     AlgorithmAES256GCM,
				Nonce:         make([]byte, 12),
				Ciphertext:    nil,
				Tag:           make([]byte, 16),
				EncryptedAt:   time.Now(),
			},
			wantErr: ErrInvalidBlob,
		},
		{
			name: "empty ciphertext",
			blob: &EncryptedBlob{
				KeyIdentifier: "dek:global",
				Algorithm:     AlgorithmAES256GCM,
				Nonce:         make([]byte, 12),
				Ciphertext:    []byte{},
				Tag:           make([]byte, 16),
				EncryptedAt:   time.Now(),
			},
			wantErr: ErrInvalidBlob,
		},
		{
			name: "missing tag",
			blob: &EncryptedBlob{
				KeyIdentifier: "dek:global",
				Algorithm:     AlgorithmAES256GCM,
				Nonce:         make([]byte, 12),
				Ciphertext:    []byte("data"),
				Tag:           nil,
				EncryptedAt:   time.Now(),
			},
			wantErr: ErrInvalidBlob,
		},
		{
			name: "invalid tag size",
			blob: &EncryptedBlob{
				KeyIdentifier: "dek:global",
				Algorithm:     AlgorithmAES256GCM,
				Nonce:         make([]byte, 12),
				Ciphertext:    []byte("data"),
				Tag:           make([]byte, 8), // Should be 16
				EncryptedAt:   time.Now(),
			},
			wantErr: ErrInvalidBlob,
		},
		{
			name: "zero timestamp",
			blob: &EncryptedBlob{
				KeyIdentifier: "dek:global",
				Algorithm:     AlgorithmAES256GCM,
				Nonce:         make([]byte, 12),
				Ciphertext:    []byte("data"),
				Tag:           make([]byte, 16),
				EncryptedAt:   time.Time{},
			},
			wantErr: ErrInvalidBlob,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.blob.Validate()
			if err != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestEncryptedBlobIsDEKEncrypted tests the IsDEKEncrypted method.
func TestEncryptedBlobIsDEKEncrypted(t *testing.T) {
	tests := []struct {
		name          string
		keyIdentifier string
		want          bool
	}{
		{
			name:          "global DEK",
			keyIdentifier: "dek:global",
			want:          true,
		},
		{
			name:          "tenant DEK",
			keyIdentifier: "dek:tenant:acme-corp",
			want:          true,
		},
		{
			name:          "tenant DEK with UUID",
			keyIdentifier: "dek:tenant:123e4567-e89b-12d3-a456-426614174000",
			want:          true,
		},
		{
			name:          "xkms AWS KMS key",
			keyIdentifier: "xkms:aws:kms:alias/mykey",
			want:          false,
		},
		{
			name:          "xkms Vault key",
			keyIdentifier: "xkms:vault:transit/keys/mykey",
			want:          false,
		},
		{
			name:          "simple key ID",
			keyIdentifier: "my-encryption-key",
			want:          false,
		},
		{
			name:          "empty identifier",
			keyIdentifier: "",
			want:          false,
		},
		{
			name:          "DEK prefix but not valid",
			keyIdentifier: "dek",
			want:          false,
		},
		{
			name:          "contains dek but not prefix",
			keyIdentifier: "my-dek-key",
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blob := &EncryptedBlob{
				KeyIdentifier: tt.keyIdentifier,
			}
			if got := blob.IsDEKEncrypted(); got != tt.want {
				t.Errorf("IsDEKEncrypted() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestEncryptOptions tests the EncryptOptions struct.
func TestEncryptOptions(t *testing.T) {
	t.Run("nil options", func(t *testing.T) {
		var opts *EncryptOptions
		if opts != nil {
			t.Error("nil EncryptOptions should be nil")
		}
	})

	t.Run("options with algorithm", func(t *testing.T) {
		opts := &EncryptOptions{
			Algorithm: AlgorithmChaCha20Poly1305,
		}
		if opts.Algorithm != AlgorithmChaCha20Poly1305 {
			t.Errorf("Algorithm = %v, want %v", opts.Algorithm, AlgorithmChaCha20Poly1305)
		}
	})

	t.Run("options with AAD", func(t *testing.T) {
		aad := []byte("/secrets/path")
		opts := &EncryptOptions{
			AAD: aad,
		}
		if string(opts.AAD) != string(aad) {
			t.Errorf("AAD = %v, want %v", opts.AAD, aad)
		}
	})

	t.Run("options with both", func(t *testing.T) {
		aad := []byte("/secrets/myapp/key")
		opts := &EncryptOptions{
			Algorithm: AlgorithmAES128GCM,
			AAD:       aad,
		}
		if opts.Algorithm != AlgorithmAES128GCM {
			t.Errorf("Algorithm = %v, want %v", opts.Algorithm, AlgorithmAES128GCM)
		}
		if string(opts.AAD) != string(aad) {
			t.Errorf("AAD = %v, want %v", opts.AAD, aad)
		}
	})
}

// TestAlgorithmConstants tests that algorithm constants are defined correctly.
func TestAlgorithmConstants(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{
			name:  "AES-256-GCM",
			value: AlgorithmAES256GCM,
			want:  "AES-256-GCM",
		},
		{
			name:  "AES-128-GCM",
			value: AlgorithmAES128GCM,
			want:  "AES-128-GCM",
		},
		{
			name:  "CHACHA20-POLY1305",
			value: AlgorithmChaCha20Poly1305,
			want:  "CHACHA20-POLY1305",
		},
		{
			name:  "Default algorithm is AES-256-GCM",
			value: DefaultAlgorithm,
			want:  AlgorithmAES256GCM,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value != tt.want {
				t.Errorf("constant = %v, want %v", tt.value, tt.want)
			}
		})
	}
}

// TestErrorConstants tests that error constants are defined and unique.
func TestErrorConstants(t *testing.T) {
	errors := []struct {
		name string
		err  error
		msg  string
	}{
		{
			name: "ErrInvalidKeyIdentifier",
			err:  ErrInvalidKeyIdentifier,
			msg:  "invalid key identifier",
		},
		{
			name: "ErrKeyNotFound",
			err:  ErrKeyNotFound,
			msg:  "key not found",
		},
		{
			name: "ErrEncryptionFailed",
			err:  ErrEncryptionFailed,
			msg:  "encryption failed",
		},
		{
			name: "ErrDecryptionFailed",
			err:  ErrDecryptionFailed,
			msg:  "decryption failed",
		},
		{
			name: "ErrUnsupportedAlgorithm",
			err:  ErrUnsupportedAlgorithm,
			msg:  "unsupported algorithm",
		},
		{
			name: "ErrInvalidBlob",
			err:  ErrInvalidBlob,
			msg:  "invalid encrypted blob",
		},
	}

	// Test each error is non-nil and has correct message
	for _, tt := range errors {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err == nil {
				t.Error("error should not be nil")
			}
			if tt.err.Error() != tt.msg {
				t.Errorf("error message = %v, want %v", tt.err.Error(), tt.msg)
			}
		})
	}

	// Test errors are unique
	seen := make(map[error]string)
	for _, tt := range errors {
		if prev, exists := seen[tt.err]; exists {
			t.Errorf("duplicate error: %v and %v are the same", tt.name, prev)
		}
		seen[tt.err] = tt.name
	}
}

// TestEncryptedBlobEdgeCases tests edge cases for EncryptedBlob.
func TestEncryptedBlobEdgeCases(t *testing.T) {
	t.Run("blob with very large ciphertext", func(t *testing.T) {
		blob := &EncryptedBlob{
			KeyIdentifier: "dek:global",
			KeyVersion:    1,
			Algorithm:     AlgorithmAES256GCM,
			Nonce:         make([]byte, 12),
			Ciphertext:    make([]byte, 1024*1024), // 1MB
			Tag:           make([]byte, 16),
			AAD:           []byte("/path"),
			EncryptedAt:   time.Now(),
		}
		if err := blob.Validate(); err != nil {
			t.Errorf("Validate() failed for large ciphertext: %v", err)
		}
	})

	t.Run("blob with very long key identifier", func(t *testing.T) {
		longID := "dek:tenant:" + string(make([]byte, 1000))
		blob := &EncryptedBlob{
			KeyIdentifier: longID,
			KeyVersion:    1,
			Algorithm:     AlgorithmAES256GCM,
			Nonce:         make([]byte, 12),
			Ciphertext:    []byte("data"),
			Tag:           make([]byte, 16),
			AAD:           []byte("/path"),
			EncryptedAt:   time.Now(),
		}
		if err := blob.Validate(); err != nil {
			t.Errorf("Validate() failed for long key identifier: %v", err)
		}
	})

	t.Run("blob with very long AAD", func(t *testing.T) {
		blob := &EncryptedBlob{
			KeyIdentifier: "dek:global",
			KeyVersion:    1,
			Algorithm:     AlgorithmAES256GCM,
			Nonce:         make([]byte, 12),
			Ciphertext:    []byte("data"),
			Tag:           make([]byte, 16),
			AAD:           make([]byte, 10000),
			EncryptedAt:   time.Now(),
		}
		if err := blob.Validate(); err != nil {
			t.Errorf("Validate() failed for long AAD: %v", err)
		}
	})

	t.Run("blob with max uint64 key version", func(t *testing.T) {
		blob := &EncryptedBlob{
			KeyIdentifier: "dek:global",
			KeyVersion:    ^uint64(0), // Max uint64
			Algorithm:     AlgorithmAES256GCM,
			Nonce:         make([]byte, 12),
			Ciphertext:    []byte("data"),
			Tag:           make([]byte, 16),
			AAD:           []byte("/path"),
			EncryptedAt:   time.Now(),
		}
		if err := blob.Validate(); err != nil {
			t.Errorf("Validate() failed for max key version: %v", err)
		}
	})

	t.Run("blob with future timestamp", func(t *testing.T) {
		blob := &EncryptedBlob{
			KeyIdentifier: "dek:global",
			KeyVersion:    1,
			Algorithm:     AlgorithmAES256GCM,
			Nonce:         make([]byte, 12),
			Ciphertext:    []byte("data"),
			Tag:           make([]byte, 16),
			AAD:           []byte("/path"),
			EncryptedAt:   time.Now().Add(24 * time.Hour),
		}
		// Should still be valid - timestamp validation is just for zero check
		if err := blob.Validate(); err != nil {
			t.Errorf("Validate() failed for future timestamp: %v", err)
		}
	})

	t.Run("blob with very old timestamp", func(t *testing.T) {
		blob := &EncryptedBlob{
			KeyIdentifier: "dek:global",
			KeyVersion:    1,
			Algorithm:     AlgorithmAES256GCM,
			Nonce:         make([]byte, 12),
			Ciphertext:    []byte("data"),
			Tag:           make([]byte, 16),
			AAD:           []byte("/path"),
			EncryptedAt:   time.Unix(0, 1), // Almost epoch
		}
		if err := blob.Validate(); err != nil {
			t.Errorf("Validate() failed for old timestamp: %v", err)
		}
	})
}
