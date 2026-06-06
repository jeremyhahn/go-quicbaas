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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnabled verifies FIPS mode detection via crypto/fips140.Enabled().
func TestEnabled(t *testing.T) {
	// This test works for both FIPS and non-FIPS builds.
	// The result depends on whether GOFIPS140 was set during build.
	enabled := Enabled()

	t.Logf("FIPS mode enabled: %v", enabled)

	// Verify the function returns a boolean
	assert.IsType(t, false, enabled, "should return a boolean")
}

// TestEnabled_ReturnsConsistentValue verifies repeated calls return same value.
func TestEnabled_ReturnsConsistentValue(t *testing.T) {
	first := Enabled()
	second := Enabled()

	assert.Equal(t, first, second, "repeated calls should return consistent value")
}

// TestGetStatus verifies status structure is correctly populated.
func TestGetStatus(t *testing.T) {
	status := GetStatus()

	// Verify status structure is populated
	assert.NotNil(t, status, "should return non-nil status")
	assert.NotEmpty(t, status.CryptoProvider, "should have crypto provider")

	// Log status for diagnostics
	t.Logf("FIPS Status: %s", status.String())
	t.Logf("  Enabled: %v", status.Enabled)
	t.Logf("  Kernel FIPS Mode: %v", status.KernelFIPSMode)
	t.Logf("  Kernel Cmdline fips=1: %v", status.KernelCmdline)
	t.Logf("  Crypto Provider: %s", status.CryptoProvider)

	// Verify consistency with Enabled()
	assert.Equal(t, Enabled(), status.Enabled, "status.Enabled should match Enabled()")
}

// TestGetStatus_CryptoProvider verifies correct crypto provider string.
func TestGetStatus_CryptoProvider(t *testing.T) {
	status := GetStatus()

	if status.Enabled {
		assert.Equal(t, "Go FIPS 140-3", status.CryptoProvider,
			"FIPS mode should report 'Go FIPS 140-3' as provider")
	} else {
		assert.Equal(t, "Go Standard Crypto", status.CryptoProvider,
			"non-FIPS mode should report 'Go Standard Crypto' as provider")
	}
}

// TestFIPSStatus_String verifies the String() method output.
func TestFIPSStatus_String(t *testing.T) {
	status := GetStatus()
	str := status.String()

	assert.NotEmpty(t, str, "should produce non-empty string")
	assert.Contains(t, str, "FIPS Mode:", "should contain FIPS mode indicator")
	assert.Contains(t, str, "Kernel FIPS Mode:", "should contain kernel FIPS mode")
	assert.Contains(t, str, "Kernel Cmdline fips=1:", "should contain kernel cmdline")
	assert.Contains(t, str, "Crypto Provider:", "should contain crypto provider")
}

// TestFIPSStatus_StringEnabled verifies the String() output when FIPS is enabled.
func TestFIPSStatus_StringEnabled(t *testing.T) {
	status := &FIPSStatus{
		Enabled:        true,
		KernelFIPSMode: true,
		KernelCmdline:  true,
		CryptoProvider: "Go FIPS 140-3",
	}
	str := status.String()

	assert.Contains(t, str, "FIPS Mode: ENABLED")
	assert.Contains(t, str, "Kernel FIPS Mode: true")
	assert.Contains(t, str, "Kernel Cmdline fips=1: true")
	assert.Contains(t, str, "Crypto Provider: Go FIPS 140-3")
}

// TestFIPSStatus_StringDisabled verifies the String() output when FIPS is disabled.
func TestFIPSStatus_StringDisabled(t *testing.T) {
	status := &FIPSStatus{
		Enabled:        false,
		KernelFIPSMode: false,
		KernelCmdline:  false,
		CryptoProvider: "Go Standard Crypto",
	}
	str := status.String()

	assert.Contains(t, str, "FIPS Mode: DISABLED")
	assert.Contains(t, str, "Kernel FIPS Mode: false")
	assert.Contains(t, str, "Kernel Cmdline fips=1: false")
	assert.Contains(t, str, "Crypto Provider: Go Standard Crypto")
}

// TestLogStatus verifies LogStatus doesn't panic.
func TestLogStatus(t *testing.T) {
	// This test just verifies the function doesn't panic.
	// The actual logging output is not captured in unit tests.
	assert.NotPanics(t, func() {
		LogStatus()
	}, "LogStatus should not panic")
}

// TestIsKernelFIPSEnabled verifies kernel FIPS detection doesn't panic.
func TestIsKernelFIPSEnabled(t *testing.T) {
	// This function reads /proc/sys/crypto/fips_enabled which may not exist
	// on all systems (non-Linux, containers, etc.). It should handle this gracefully.
	assert.NotPanics(t, func() {
		result := isKernelFIPSEnabled()
		t.Logf("Kernel FIPS enabled: %v", result)
	}, "isKernelFIPSEnabled should not panic")
}

// TestIsKernelFIPSEnabled_ReturnsBool verifies the return type is always boolean.
func TestIsKernelFIPSEnabled_ReturnsBool(t *testing.T) {
	result := isKernelFIPSEnabled()
	// On a system without kernel FIPS mode, the result should be false.
	// On a system with kernel FIPS mode, the result should be true.
	// Either way, the result must be a valid boolean.
	assert.IsType(t, false, result)
}

// TestIsKernelCmdlineFIPS verifies kernel cmdline detection doesn't panic.
func TestIsKernelCmdlineFIPS(t *testing.T) {
	// This function reads /proc/cmdline which may not exist on all systems.
	// It should handle this gracefully.
	assert.NotPanics(t, func() {
		result := isKernelCmdlineFIPS()
		t.Logf("Kernel cmdline fips=1: %v", result)
	}, "isKernelCmdlineFIPS should not panic")
}

// TestIsKernelCmdlineFIPS_ReturnsBool verifies the return type is always boolean.
func TestIsKernelCmdlineFIPS_ReturnsBool(t *testing.T) {
	result := isKernelCmdlineFIPS()
	assert.IsType(t, false, result)
}

// TestGetStatus_FieldConsistency checks that all fields are populated consistently.
func TestGetStatus_FieldConsistency(t *testing.T) {
	status := GetStatus()

	// The Enabled field should always match the Enabled() function
	assert.Equal(t, Enabled(), status.Enabled)

	// The KernelFIPSMode field should match isKernelFIPSEnabled()
	assert.Equal(t, isKernelFIPSEnabled(), status.KernelFIPSMode)

	// The KernelCmdline field should match isKernelCmdlineFIPS()
	assert.Equal(t, isKernelCmdlineFIPS(), status.KernelCmdline)

	// CryptoProvider must always be set
	assert.NotEmpty(t, status.CryptoProvider)
}

// BenchmarkEnabled benchmarks the Enabled() function.
func BenchmarkEnabled(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Enabled()
	}
}

// BenchmarkGetStatus benchmarks the GetStatus() function.
func BenchmarkGetStatus(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = GetStatus()
	}
}

// =============================================================================
// Consolidated from fips_coverage_test.go
// =============================================================================

// TestIsKernelFIPSEnabled_FileContainsOne verifies that isKernelFIPSEnabled
// returns true when the proc file contains "1", covering the success path
// that is unreachable on non-FIPS kernels.
func TestIsKernelFIPSEnabled_FileContainsOne(t *testing.T) {
	tmpDir := t.TempDir()
	fipsFile := filepath.Join(tmpDir, "fips_enabled")
	require.NoError(t, os.WriteFile(fipsFile, []byte("1\n"), 0644))

	original := procFIPSEnabledPath
	procFIPSEnabledPath = fipsFile
	t.Cleanup(func() { procFIPSEnabledPath = original })

	result := isKernelFIPSEnabled()
	assert.True(t, result, "should return true when file contains '1'")
}

// TestIsKernelFIPSEnabled_FileContainsZero verifies that isKernelFIPSEnabled
// returns false when the proc file contains "0".
func TestIsKernelFIPSEnabled_FileContainsZero(t *testing.T) {
	tmpDir := t.TempDir()
	fipsFile := filepath.Join(tmpDir, "fips_enabled")
	require.NoError(t, os.WriteFile(fipsFile, []byte("0\n"), 0644))

	original := procFIPSEnabledPath
	procFIPSEnabledPath = fipsFile
	t.Cleanup(func() { procFIPSEnabledPath = original })

	result := isKernelFIPSEnabled()
	assert.False(t, result, "should return false when file contains '0'")
}

// TestIsKernelFIPSEnabled_FileNotFound verifies that isKernelFIPSEnabled
// returns false when the proc file does not exist.
func TestIsKernelFIPSEnabled_FileNotFound(t *testing.T) {
	original := procFIPSEnabledPath
	procFIPSEnabledPath = "/nonexistent/path/fips_enabled"
	t.Cleanup(func() { procFIPSEnabledPath = original })

	result := isKernelFIPSEnabled()
	assert.False(t, result, "should return false when file does not exist")
}

// TestIsKernelCmdlineFIPS_ContainsFIPSParam verifies that isKernelCmdlineFIPS
// returns true when the cmdline file contains the "fips=1" boot parameter,
// covering the success path unreachable on non-FIPS kernels.
func TestIsKernelCmdlineFIPS_ContainsFIPSParam(t *testing.T) {
	tmpDir := t.TempDir()
	cmdlineFile := filepath.Join(tmpDir, "cmdline")
	require.NoError(t, os.WriteFile(cmdlineFile,
		[]byte("BOOT_IMAGE=/boot/vmlinuz root=/dev/sda1 fips=1 quiet\n"), 0644))

	original := procCmdlinePath
	procCmdlinePath = cmdlineFile
	t.Cleanup(func() { procCmdlinePath = original })

	result := isKernelCmdlineFIPS()
	assert.True(t, result, "should return true when cmdline contains 'fips=1'")
}

// TestIsKernelCmdlineFIPS_NoFIPSParam verifies that isKernelCmdlineFIPS
// returns false when the cmdline file does not contain "fips=1".
func TestIsKernelCmdlineFIPS_NoFIPSParam(t *testing.T) {
	tmpDir := t.TempDir()
	cmdlineFile := filepath.Join(tmpDir, "cmdline")
	require.NoError(t, os.WriteFile(cmdlineFile,
		[]byte("BOOT_IMAGE=/boot/vmlinuz root=/dev/sda1 quiet splash\n"), 0644))

	original := procCmdlinePath
	procCmdlinePath = cmdlineFile
	t.Cleanup(func() { procCmdlinePath = original })

	result := isKernelCmdlineFIPS()
	assert.False(t, result, "should return false when cmdline lacks 'fips=1'")
}

// TestIsKernelCmdlineFIPS_FileNotFound verifies that isKernelCmdlineFIPS
// returns false when the cmdline file does not exist.
func TestIsKernelCmdlineFIPS_FileNotFound(t *testing.T) {
	original := procCmdlinePath
	procCmdlinePath = "/nonexistent/path/cmdline"
	t.Cleanup(func() { procCmdlinePath = original })

	result := isKernelCmdlineFIPS()
	assert.False(t, result, "should return false when file does not exist")
}

// TestIsKernelCmdlineFIPS_FIPSAsSubstring verifies that partial matches
// like "nofips=1" or "fips=10" do not produce false positives.
func TestIsKernelCmdlineFIPS_FIPSAsSubstring(t *testing.T) {
	tmpDir := t.TempDir()
	cmdlineFile := filepath.Join(tmpDir, "cmdline")
	require.NoError(t, os.WriteFile(cmdlineFile,
		[]byte("BOOT_IMAGE=/boot/vmlinuz nofips=1 fips=10 quiet\n"), 0644))

	original := procCmdlinePath
	procCmdlinePath = cmdlineFile
	t.Cleanup(func() { procCmdlinePath = original })

	result := isKernelCmdlineFIPS()
	assert.False(t, result,
		"should return false when 'fips=1' only appears as substring of other params")
}
