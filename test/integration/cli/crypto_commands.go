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

//go:build integration

package cli

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// =============================================================================
// Crypto Commands
// =============================================================================

// cryptoTransportsToSkip defines which transports to skip for crypto operations.
// Crypto operations use the same transport layer as KMS.
var cryptoTransportsToSkip = []Transport{TransportREST, TransportQUIC}

// cryptoTimeout is the default timeout for crypto operations.
const cryptoTimeout = 10 * time.Second

// CryptoEncryptCommand returns a command to encrypt data with the named key.
func CryptoEncryptCommand(keyName, data string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("crypto encrypt %s", keyName),
		Args:           []string{"crypto", "encrypt", keyName, "--data", data},
		ExpectSuccess:  true,
		RequiresUnseal: true,
		SkipTransports: cryptoTransportsToSkip,
		Timeout:        cryptoTimeout,
	}
}

// CryptoEncryptWithContextCommand returns a command to encrypt data with an
// additional authenticated data (AAD) context.
func CryptoEncryptWithContextCommand(keyName, data, context string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("crypto encrypt %s with context", keyName),
		Args:           []string{"crypto", "encrypt", keyName, "--data", data, "--context", context},
		ExpectSuccess:  true,
		RequiresUnseal: true,
		SkipTransports: cryptoTransportsToSkip,
		Timeout:        cryptoTimeout,
	}
}

// CryptoEncryptBase64Command returns a command to encrypt base64-encoded data.
func CryptoEncryptBase64Command(keyName, base64Data string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("crypto encrypt %s base64", keyName),
		Args:           []string{"crypto", "encrypt", keyName, "--data", base64Data, "--base64"},
		ExpectSuccess:  true,
		RequiresUnseal: true,
		SkipTransports: cryptoTransportsToSkip,
		Timeout:        cryptoTimeout,
	}
}

// CryptoDecryptCommand returns a command to decrypt hex-encoded ciphertext.
func CryptoDecryptCommand(keyName, hexCiphertext string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("crypto decrypt %s", keyName),
		Args:           []string{"crypto", "decrypt", keyName, "--data", hexCiphertext},
		ExpectSuccess:  true,
		RequiresUnseal: true,
		SkipTransports: cryptoTransportsToSkip,
		Timeout:        cryptoTimeout,
	}
}

// CryptoDecryptBase64Command returns a command to decrypt base64-encoded ciphertext.
func CryptoDecryptBase64Command(keyName, base64Ciphertext string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("crypto decrypt %s base64", keyName),
		Args:           []string{"crypto", "decrypt", keyName, "--data", base64Ciphertext, "--base64"},
		ExpectSuccess:  true,
		RequiresUnseal: true,
		SkipTransports: cryptoTransportsToSkip,
		Timeout:        cryptoTimeout,
	}
}

// CryptoDecryptWithContextCommand returns a command to decrypt ciphertext that
// was encrypted with an additional authenticated data (AAD) context.
func CryptoDecryptWithContextCommand(keyName, hexCiphertext, context string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("crypto decrypt %s with context", keyName),
		Args:           []string{"crypto", "decrypt", keyName, "--data", hexCiphertext, "--base64", "--context", context},
		ExpectSuccess:  true,
		RequiresUnseal: true,
		SkipTransports: cryptoTransportsToSkip,
		Timeout:        cryptoTimeout,
	}
}

// CryptoRewrapCommand returns a command to re-encrypt ciphertext under the
// latest version of the named key without exposing the plaintext.
func CryptoRewrapCommand(keyName, base64Ciphertext string) CommandDef {
	// The rewrap CLI expects hex-encoded data; convert from base64
	hexData := base64Ciphertext
	if decoded, err := base64.StdEncoding.DecodeString(base64Ciphertext); err == nil {
		hexData = hex.EncodeToString(decoded)
	}
	return CommandDef{
		Name:           fmt.Sprintf("crypto rewrap %s", keyName),
		Args:           []string{"crypto", "rewrap", keyName, "--data", hexData},
		ExpectSuccess:  true,
		RequiresUnseal: true,
		SkipTransports: cryptoTransportsToSkip,
		Timeout:        cryptoTimeout,
	}
}

// CryptoSignCommand returns a command to sign data with the named key.
func CryptoSignCommand(keyName, data string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("crypto sign %s", keyName),
		Args:           []string{"crypto", "sign", keyName, "--data", data},
		ExpectSuccess:  true,
		RequiresUnseal: true,
		SkipTransports: cryptoTransportsToSkip,
		Timeout:        cryptoTimeout,
	}
}

// CryptoSignWithAlgorithmCommand returns a command to sign data using a
// specific signing algorithm.
func CryptoSignWithAlgorithmCommand(keyName, data, algorithm string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("crypto sign %s algorithm=%s", keyName, algorithm),
		Args:           []string{"crypto", "sign", keyName, "--data", data, "--algorithm", algorithm},
		ExpectSuccess:  true,
		RequiresUnseal: true,
		SkipTransports: cryptoTransportsToSkip,
		Timeout:        cryptoTimeout,
	}
}

// CryptoVerifyCommand returns a command to verify a signature against data
// using the named key.
func CryptoVerifyCommand(keyName, data, signature string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("crypto verify %s", keyName),
		Args:           []string{"crypto", "verify", keyName, "--data", data, "--signature", signature},
		ExpectSuccess:  true,
		RequiresUnseal: true,
		SkipTransports: cryptoTransportsToSkip,
		Timeout:        cryptoTimeout,
	}
}

// CryptoVerifyWithAlgorithmCommand returns a command to verify a signature
// using a specific algorithm.
func CryptoVerifyWithAlgorithmCommand(keyName, data, signature, algorithm string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("crypto verify %s algorithm=%s", keyName, algorithm),
		Args:           []string{"crypto", "verify", keyName, "--data", data, "--signature", signature, "--algorithm", algorithm},
		ExpectSuccess:  true,
		RequiresUnseal: true,
		SkipTransports: cryptoTransportsToSkip,
		Timeout:        cryptoTimeout,
	}
}

// CryptoHashCommand returns a command to compute a cryptographic hash of data
// using the default algorithm.
func CryptoHashCommand(data string) CommandDef {
	return CommandDef{
		Name:           "crypto hash",
		Args:           []string{"crypto", "hash", "--data", data},
		ExpectSuccess:  true,
		RequiresUnseal: true,
		SkipTransports: cryptoTransportsToSkip,
		Timeout:        cryptoTimeout,
	}
}

// CryptoHashWithAlgorithmCommand returns a command to compute a cryptographic
// hash using a specific algorithm.
func CryptoHashWithAlgorithmCommand(data, algorithm string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("crypto hash algorithm=%s", algorithm),
		Args:           []string{"crypto", "hash", "--data", data, "--algorithm", algorithm},
		ExpectSuccess:  true,
		RequiresUnseal: true,
		SkipTransports: cryptoTransportsToSkip,
		Timeout:        cryptoTimeout,
	}
}

// CryptoRandomCommand returns a command to generate cryptographically secure
// random bytes.
func CryptoRandomCommand(numBytes int) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("crypto random bytes=%d", numBytes),
		Args:           []string{"crypto", "random", "--bytes", fmt.Sprintf("%d", numBytes)},
		ExpectSuccess:  true,
		RequiresUnseal: true,
		SkipTransports: cryptoTransportsToSkip,
		Timeout:        cryptoTimeout,
	}
}

// CryptoRandomHexCommand returns a command to generate random bytes with
// hex-encoded output.
func CryptoRandomHexCommand(numBytes int) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("crypto random bytes=%d format=hex", numBytes),
		Args:           []string{"crypto", "random", "--bytes", fmt.Sprintf("%d", numBytes), "--format", "hex"},
		ExpectSuccess:  true,
		RequiresUnseal: true,
		SkipTransports: cryptoTransportsToSkip,
		Timeout:        cryptoTimeout,
	}
}

// CryptoDataKeyCommand returns a command to generate a data encryption key
// (DEK) wrapped by the named key.
func CryptoDataKeyCommand(keyName string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("crypto datakey %s", keyName),
		Args:           []string{"crypto", "datakey", keyName},
		ExpectSuccess:  true,
		RequiresUnseal: true,
		SkipTransports: cryptoTransportsToSkip,
		Timeout:        cryptoTimeout,
	}
}

// CryptoDataKeyWithBitsCommand returns a command to generate a data encryption
// key with a specific bit length.
func CryptoDataKeyWithBitsCommand(keyName string, bits int) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("crypto datakey %s bits=%d", keyName, bits),
		Args:           []string{"crypto", "datakey", keyName, "--bits", fmt.Sprintf("%d", bits)},
		ExpectSuccess:  true,
		RequiresUnseal: true,
		SkipTransports: cryptoTransportsToSkip,
		Timeout:        cryptoTimeout,
	}
}

// =============================================================================
// Crypto Capability Probe
// =============================================================================

// RequireCryptoAvailable probes the server to verify that the crypto service
// is available. If the server returns "not available", "not_implemented",
// "unknown service", or similar errors, it validates the error response is
// correct and returns false.
// Returns true if the crypto service is available, false otherwise.
// Tests should return early when this method returns false.
func (r *TestRunner) RequireCryptoAvailable() bool {
	r.t.Helper()

	probeCmd := CommandDef{
		Name:           "crypto service availability probe",
		Args:           []string{"crypto", "random", "--bytes", "1"},
		ExpectSuccess:  true,
		Timeout:        15 * time.Second,
		SkipTransports: cryptoTransportsToSkip,
	}

	for _, skip := range probeCmd.SkipTransports {
		if skip == r.transport {
			return true
		}
	}

	stdout, stderr, err := r.Run(probeCmd)
	if err != nil && outputIndicatesUnavailable(stdout, stderr) {
		combined := strings.TrimSpace(stdout + stderr)
		r.t.Logf("[%s] Crypto service not available (validated correct error response: %s)", r.transport, combined)
		return false
	}
	return true
}
