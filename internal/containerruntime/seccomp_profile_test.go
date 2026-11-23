// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-trusted-platform.
//
// go-trusted-platform is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package containerruntime

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// allowed reports whether p has an unconditional SCMP_ACT_ALLOW rule for name.
func allowed(p *ociSeccomp, name string) bool {
	for _, sc := range p.Syscalls {
		if sc.Action == "SCMP_ACT_ALLOW" && len(sc.Args) == 0 && slices.Contains(sc.Names, name) {
			return true
		}
	}
	return false
}

// rule returns the first rule of p naming name with action, or nil.
func rule(p *ociSeccomp, name, action string) *ociSyscall {
	for i := range p.Syscalls {
		if p.Syscalls[i].Action == action && slices.Contains(p.Syscalls[i].Names, name) {
			return &p.Syscalls[i]
		}
	}
	return nil
}

func TestDockerSeccompProfile_Parses(t *testing.T) {
	var p *dockerSeccompProfile
	require.NotPanics(t, func() { p = parsedDockerSeccompProfile() })
	require.NotNil(t, p)
	assert.Equal(t, "SCMP_ACT_ERRNO", p.DefaultAction)
	require.NotNil(t, p.DefaultErrnoRet)
	assert.Equal(t, uint(1), *p.DefaultErrnoRet)
	names := 0
	for _, sc := range p.Syscalls {
		names += len(sc.Names)
	}
	assert.Greater(t, names, 300)
}

func TestDefaultSeccompProfile_DeniesByDefault(t *testing.T) {
	p := defaultSeccompProfile(nil, "amd64")
	assert.Equal(t, "SCMP_ACT_ERRNO", p.DefaultAction)
	require.NotNil(t, p.DefaultErrnoRet)
	assert.Equal(t, uint(1), *p.DefaultErrnoRet)
	assert.ElementsMatch(t, []string{"SCMP_ARCH_X86_64", "SCMP_ARCH_X86", "SCMP_ARCH_X32"}, p.Architectures)

	for _, name := range []string{"read", "write", "execve", "openat"} {
		assert.True(t, allowed(p, name), name)
	}
	for _, name := range []string{"keyctl", "add_key", "mount", "unshare", "reboot", "init_module", "kexec_load", "swapon", "chroot"} {
		assert.False(t, allowed(p, name), name)
	}

	clone3 := rule(p, "clone3", "SCMP_ACT_ERRNO")
	require.NotNil(t, clone3)
	require.NotNil(t, clone3.ErrnoRet)
	assert.Equal(t, uint(38), *clone3.ErrnoRet)

	assert.ElementsMatch(t, []string{"SCMP_ARCH_AARCH64", "SCMP_ARCH_ARM"}, defaultSeccompProfile(nil, "arm64").Architectures)
}

func TestDefaultSeccompProfile_FollowsCapabilities(t *testing.T) {
	admin := defaultSeccompProfile([]string{"CAP_SYS_ADMIN"}, "amd64")
	for _, name := range []string{"mount", "umount2", "unshare", "setns"} {
		assert.True(t, allowed(admin, name), name)
	}
	assert.Nil(t, rule(admin, "clone3", "SCMP_ACT_ERRNO"))

	assert.True(t, allowed(defaultSeccompProfile([]string{"CAP_SYS_CHROOT"}, "amd64"), "chroot"))
	assert.True(t, allowed(defaultSeccompProfile([]string{"CAP_SYS_BOOT"}, "amd64"), "reboot"))
	assert.True(t, allowed(defaultSeccompProfile([]string{"CAP_SYS_MODULE"}, "amd64"), "init_module"))

	clone := rule(defaultSeccompProfile(nil, "amd64"), "clone", "SCMP_ACT_ALLOW")
	require.NotNil(t, clone)
	assert.NotEmpty(t, clone.Args, "clone is allowed only under the namespace-flag mask")
}

func TestDefaultSeccompProfile_DoesNotShareTheParsedProfile(t *testing.T) {
	p := defaultSeccompProfile(nil, "amd64")
	require.NotEmpty(t, p.Syscalls)
	require.NotEmpty(t, p.Syscalls[0].Names)
	original := p.Syscalls[0].Names[0]
	p.Syscalls[0].Names[0] = "mutated"

	again := defaultSeccompProfile(nil, "amd64")
	assert.Equal(t, original, again.Syscalls[0].Names[0])
}

func TestKernelAtMost(t *testing.T) {
	assert.True(t, kernelAtMost("4.8", "5.14"))
	assert.True(t, kernelAtMost("5.14", "5.14"))
	assert.False(t, kernelAtMost("5.15", "5.14"))
	assert.False(t, kernelAtMost("6.1", "5.14"))
	assert.False(t, kernelAtMost("x", "5.14"))
}
