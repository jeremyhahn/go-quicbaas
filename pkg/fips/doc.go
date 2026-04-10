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

// Package fips provides FIPS 140-3 compliance status reporting for QuicBaaS.
//
// This package leverages Go 1.24+'s native FIPS 140-3 support via the crypto/fips140
// package. Algorithm validation is handled automatically by the Go runtime when
// GOFIPS140 mode is enabled - no manual validation is required.
//
// # Go FIPS 140-3 Mode
//
// Go 1.24+ includes native FIPS 140-3 support. When built with GOFIPS140 enabled,
// the Go crypto library automatically:
//   - Restricts to FIPS 140-3 approved algorithms
//   - Validates key sizes
//   - Uses approved cipher suites for TLS
//   - Errors/panics on non-compliant cryptographic operations
//
// # Build Instructions
//
// To build with FIPS support:
//
//	GOFIPS140=v1.0.0 go build -o bin/quicbaas-fips ./cmd/quicbaas
//
// Or use the Makefile target:
//
//	make build-fips
//
// # Runtime Requirements
//
// For full FIPS compliance, the following should be configured:
//
//  1. Build with GOFIPS140 environment variable set
//  2. Linux kernel FIPS mode enabled (/proc/sys/crypto/fips_enabled == 1)
//  3. Kernel booted with fips=1 parameter
//
// # Enabling Kernel FIPS Mode
//
// To enable FIPS mode in the Linux kernel:
//
//	# Ubuntu/Debian
//	sudo apt-get install fips-initramfs
//	sudo update-initramfs -u
//	# Add fips=1 to kernel boot parameters
//	sudo vi /etc/default/grub
//	# Add: GRUB_CMDLINE_LINUX="fips=1"
//	sudo update-grub
//	sudo reboot
//
//	# RHEL/CentOS
//	sudo yum install dracut-fips
//	sudo dracut -f
//	# Add fips=1 to kernel boot parameters
//	sudo grubby --update-kernel=ALL --args="fips=1"
//	sudo reboot
//
//	# Verify FIPS mode is enabled
//	cat /proc/sys/crypto/fips_enabled  # Should output: 1
//
// # Usage
//
//	package main
//
//	import (
//		"fmt"
//		"github.com/jeremyhahn/go-quicbaas/pkg/fips"
//	)
//
//	func main() {
//		// Check if FIPS mode is enabled
//		if fips.Enabled() {
//			fmt.Println("Running in FIPS 140-3 compliant mode")
//		}
//
//		// Get detailed status for diagnostics
//		status := fips.GetStatus()
//		fmt.Printf("FIPS Enabled: %v\n", status.Enabled)
//		fmt.Printf("Kernel FIPS Mode: %v\n", status.KernelFIPSMode)
//		fmt.Printf("Kernel Cmdline: %v\n", status.KernelCmdline)
//
//		// Log status for audit purposes
//		fips.LogStatus()
//	}
//
// # Approved Algorithms
//
// When Go FIPS mode is enabled, only FIPS 140-3 approved algorithms are available.
// The Go runtime automatically enforces this - non-approved algorithms will fail.
//
// Symmetric Encryption:
//   - AES-128-GCM, AES-256-GCM
//
// Hash Functions:
//   - SHA-256, SHA-384, SHA-512
//
// Digital Signatures:
//   - ECDSA with P-256, P-384, P-521
//   - RSA with 2048+ bit keys
//
// Key Agreement:
//   - ECDH with P-256, P-384, P-521
//
// TLS:
//   - TLS 1.2+ with approved cipher suites
//
// # References
//
//   - Go FIPS 140-3: https://go.dev/doc/fips
//   - FIPS 140-3 Standard: https://csrc.nist.gov/publications/detail/fips/140/3/final
//   - NIST CMVP: https://csrc.nist.gov/projects/cryptographic-module-validation-program
package fips
