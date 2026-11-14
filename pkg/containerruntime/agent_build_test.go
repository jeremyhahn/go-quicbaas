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

package containerruntime

import (
	"crypto/fips140"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Two agents on one host are given different roots, and their runtimes then
// share no directory at all.
func TestRuntimeConfigUnder_TwoRootsShareNothing(t *testing.T) {
	a := RuntimeConfigUnder("/var/lib/agent-a/containers")
	b := RuntimeConfigUnder("/var/lib/agent-b/containers/")
	require.NoError(t, a.Validate())
	require.NoError(t, b.Validate())

	dirs := func(c *RuntimeConfig) []string {
		return []string{c.Root, c.RunRoot, c.TmpDir, c.StaticDir, c.VolumePath, c.ImageStorePath}
	}
	for _, dir := range dirs(a) {
		assert.True(t, strings.HasPrefix(dir, "/var/lib/agent-a/containers/"), "%s is outside agent a's root", dir)
		for _, other := range dirs(b) {
			assert.False(t, dir == other || strings.HasPrefix(other, dir+"/") || strings.HasPrefix(dir, other+"/"),
				"agent a's %s overlaps agent b's %s", dir, other)
		}
	}
	for _, dir := range dirs(b) {
		assert.True(t, strings.HasPrefix(dir, "/var/lib/agent-b/containers/"), "%s is outside agent b's root", dir)
	}
}

// fipsChildEnv marks the re-executed test binary of
// TestFIPSModuleUnaffectedByTheRuntime.
const fipsChildEnv = "QUICBAAS_CONTAINERRUNTIME_FIPS_CHILD"

// The agent links this package with cgo; the Go-native FIPS 140-3 module must
// still switch on in such a binary. This test binary is one: it links the
// runtime with cgo, so it re-executes itself with fips140=on and the child
// reports whether the module is enabled and answers a known SHA-256 vector.
func TestFIPSModuleUnaffectedByTheRuntime(t *testing.T) {
	if os.Getenv(fipsChildEnv) == "1" {
		sum := sha256.Sum256([]byte("abc"))
		fmt.Printf("fips140.Enabled=%t sha256=%x\n", fips140.Enabled(), sum)
		return
	}

	self, err := os.Executable()
	require.NoError(t, err)
	cmd := exec.Command(self, "-test.run=^TestFIPSModuleUnaffectedByTheRuntime$", "-test.count=1") // #nosec G204 -- the test binary itself.
	cmd.Dir = filepath.Dir(self)
	cmd.Env = append(os.Environ(), fipsChildEnv+"=1", "GODEBUG=fips140=on")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "child: %s", out)

	assert.Contains(t, string(out), "fips140.Enabled=true", "child: %s", out)
	assert.Contains(t, string(out), "sha256=ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", "child: %s", out)
}
