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

package fips

import (
	"crypto/fips140"
	"fmt"
	"log"
	"os"
	"strings"
)

// procFIPSEnabledPath is the path to the kernel FIPS enabled flag.
// Overridable in tests to use temp files for branch coverage.
var procFIPSEnabledPath = "/proc/sys/crypto/fips_enabled"

// procCmdlinePath is the path to the kernel command line.
// Overridable in tests to use temp files for branch coverage.
var procCmdlinePath = "/proc/cmdline"

// FIPSStatus represents the current FIPS configuration state.
type FIPSStatus struct {
	// Enabled indicates whether Go FIPS 140-3 mode is active
	Enabled bool

	// KernelFIPSMode indicates if /proc/sys/crypto/fips_enabled is set to 1
	KernelFIPSMode bool

	// KernelCmdline indicates if /proc/cmdline contains fips=1
	KernelCmdline bool

	// CryptoProvider identifies the crypto library being used
	CryptoProvider string
}

// String returns a human-readable representation of the FIPS status.
func (s *FIPSStatus) String() string {
	var sb strings.Builder
	if s.Enabled {
		sb.WriteString("FIPS Mode: ENABLED\n")
	} else {
		sb.WriteString("FIPS Mode: DISABLED\n")
	}
	sb.WriteString(fmt.Sprintf("Kernel FIPS Mode: %v\n", s.KernelFIPSMode))
	sb.WriteString(fmt.Sprintf("Kernel Cmdline fips=1: %v\n", s.KernelCmdline))
	sb.WriteString(fmt.Sprintf("Crypto Provider: %s\n", s.CryptoProvider))
	return sb.String()
}

// Enabled returns whether Go FIPS 140-3 mode is active.
// This delegates to crypto/fips140.Enabled() which is available in Go 1.24+.
// When GOFIPS140 environment variable is set during build, this returns true.
func Enabled() bool {
	return fips140.Enabled()
}

// GetStatus returns the current FIPS configuration status.
// This provides diagnostic information about FIPS mode from multiple sources:
// - Go runtime FIPS mode (crypto/fips140.Enabled())
// - Linux kernel FIPS mode (/proc/sys/crypto/fips_enabled)
// - Kernel command line (fips=1 in /proc/cmdline).
func GetStatus() *FIPSStatus {
	enabled := fips140.Enabled()
	status := &FIPSStatus{
		Enabled:        enabled,
		KernelFIPSMode: isKernelFIPSEnabled(),
		KernelCmdline:  isKernelCmdlineFIPS(),
	}

	if enabled {
		status.CryptoProvider = "Go FIPS 140-3"
	} else {
		status.CryptoProvider = "Go Standard Crypto"
	}

	return status
}

// LogStatus logs the current FIPS status for diagnostics and compliance auditing.
func LogStatus() {
	status := GetStatus()
	log.Printf("=== FIPS 140-3 Status ===")
	log.Printf("Go FIPS Mode: %v", status.Enabled)
	log.Printf("Kernel FIPS Mode: %v", status.KernelFIPSMode)
	log.Printf("Kernel Cmdline fips=1: %v", status.KernelCmdline)
	log.Printf("Crypto Provider: %s", status.CryptoProvider)
	log.Printf("========================")
}

// isKernelFIPSEnabled checks if the Linux kernel FIPS mode is enabled.
// This reads /proc/sys/crypto/fips_enabled which is available on Linux systems.
func isKernelFIPSEnabled() bool {
	data, err := os.ReadFile(procFIPSEnabledPath)
	if err != nil {
		// File doesn't exist or can't be read (non-Linux system or no FIPS support)
		return false
	}
	return strings.TrimSpace(string(data)) == "1"
}

// isKernelCmdlineFIPS checks if the kernel was booted with fips=1 parameter.
// This parses /proc/cmdline for the fips=1 boot parameter.
func isKernelCmdlineFIPS() bool {
	data, err := os.ReadFile(procCmdlinePath)
	if err != nil {
		// File doesn't exist or can't be read (non-Linux system)
		return false
	}
	cmdline := string(data)
	// Check for fips=1 as a separate parameter
	for _, param := range strings.Fields(cmdline) {
		if param == "fips=1" {
			return true
		}
	}
	return false
}
