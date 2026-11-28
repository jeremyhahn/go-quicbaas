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

// seccomp_default.json is the default seccomp profile of
// github.com/moby/profiles/seccomp v0.1.0, Copyright The Moby Authors,
// licensed under the Apache License 2.0, included unmodified.

package containerruntime

import (
	_ "embed"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"sync"
)

//go:embed seccomp_default.json
var dockerSeccompProfileJSON []byte

// dockerSeccompProfile is the layout of Docker's seccomp profile file.
type dockerSeccompProfile struct {
	DefaultAction   string                 `json:"defaultAction"`
	DefaultErrnoRet *uint                  `json:"defaultErrnoRet,omitempty"`
	ArchMap         []dockerSeccompArch    `json:"archMap"`
	Syscalls        []dockerSeccompSyscall `json:"syscalls"`
}

// dockerSeccompArch maps an architecture to its sub-architectures.
type dockerSeccompArch struct {
	Arch      string   `json:"architecture"`
	SubArches []string `json:"subArchitectures"`
}

// dockerSeccompSyscall is one rule of the profile file, with the conditions
// under which Docker applies it.
type dockerSeccompSyscall struct {
	Names    []string            `json:"names"`
	Action   string              `json:"action"`
	ErrnoRet *uint               `json:"errnoRet,omitempty"`
	Args     []ociSeccompArg     `json:"args"`
	Includes dockerSeccompFilter `json:"includes"`
	Excludes dockerSeccompFilter `json:"excludes"`
}

// dockerSeccompFilter is a rule's include or exclude condition.
type dockerSeccompFilter struct {
	Caps      []string `json:"caps,omitempty"`
	Arches    []string `json:"arches,omitempty"`
	MinKernel string   `json:"minKernel,omitempty"`
}

// ociSeccomp is the OCI runtime-spec linux.seccomp section.
type ociSeccomp struct {
	DefaultAction   string       `json:"defaultAction"`
	DefaultErrnoRet *uint        `json:"defaultErrnoRet,omitempty"`
	Architectures   []string     `json:"architectures,omitempty"`
	Syscalls        []ociSyscall `json:"syscalls,omitempty"`
}

// ociSyscall is one OCI seccomp syscall rule.
type ociSyscall struct {
	Names    []string        `json:"names"`
	Action   string          `json:"action"`
	ErrnoRet *uint           `json:"errnoRet,omitempty"`
	Args     []ociSeccompArg `json:"args,omitempty"`
}

// ociSeccompArg is one OCI seccomp argument condition.
type ociSeccompArg struct {
	Index    uint   `json:"index"`
	Value    uint64 `json:"value"`
	ValueTwo uint64 `json:"valueTwo,omitempty"`
	Op       string `json:"op"`
}

// parsedDockerSeccompProfile parses the embedded profile once. The data is
// compiled in and a test parses it, so a bad file fails the tests, never
// production.
var parsedDockerSeccompProfile = sync.OnceValue(func() *dockerSeccompProfile {
	var p dockerSeccompProfile
	if err := json.Unmarshal(dockerSeccompProfileJSON, &p); err != nil {
		panic("containerruntime: embedded seccomp profile: " + err.Error())
	}
	return &p
})

// seccompArches maps a GOARCH to its libseccomp architecture name.
var seccompArches = map[string]string{
	"amd64":   "SCMP_ARCH_X86_64",
	"arm64":   "SCMP_ARCH_AARCH64",
	"386":     "SCMP_ARCH_X86",
	"arm":     "SCMP_ARCH_ARM",
	"ppc64le": "SCMP_ARCH_PPC64LE",
	"s390x":   "SCMP_ARCH_S390X",
	"riscv64": "SCMP_ARCH_RISCV64",
}

// seccompKernelFloor is the oldest kernel the runtime runs on (an exec needs
// cgroup.kill, Linux 5.14, see ErrExecNotKillable); a rule whose minKernel
// is above it is left out, so the profile never relies on a newer kernel.
const seccompKernelFloor = "5.14"

// defaultSeccompProfile returns Docker's default profile resolved the way
// Docker resolves it for a container: architectures from the archMap entry
// of goarch (that architecture and its subArchitectures; none when goarch
// has no entry); a syscall rule is kept only when every cap in
// Includes.Caps is in caps, no cap in Excludes.Caps is in caps, goarch is in
// Includes.Arches (when non-empty) and not in Excludes.Arches, and its
// Includes.MinKernel (when set) is at most seccompKernelFloor. Kept rules
// copy Names, Action, ErrnoRet and Args, in file order. DefaultAction and
// DefaultErrnoRet are copied. caps are compared as given (CAP_ prefixed,
// upper case, which is what capabilitySet returns). Each call returns a
// fresh value that shares no slices with the parsed profile.
func defaultSeccompProfile(caps []string, goarch string) *ociSeccomp {
	src := parsedDockerSeccompProfile()
	out := &ociSeccomp{DefaultAction: src.DefaultAction}
	if src.DefaultErrnoRet != nil {
		v := *src.DefaultErrnoRet
		out.DefaultErrnoRet = &v
	}

	if arch, ok := seccompArches[goarch]; ok {
		for _, m := range src.ArchMap {
			if m.Arch == arch {
				out.Architectures = append([]string{m.Arch}, m.SubArches...)
				break
			}
		}
	}

	for _, sc := range src.Syscalls {
		if !seccompRuleApplies(sc, caps, goarch) {
			continue
		}
		rule := ociSyscall{
			Names:  slices.Clone(sc.Names),
			Action: sc.Action,
			Args:   slices.Clone(sc.Args),
		}
		if sc.ErrnoRet != nil {
			v := *sc.ErrnoRet
			rule.ErrnoRet = &v
		}
		out.Syscalls = append(out.Syscalls, rule)
	}
	return out
}

// seccompRuleApplies reports whether a profile rule's include and exclude
// conditions hold for caps and goarch.
func seccompRuleApplies(sc dockerSeccompSyscall, caps []string, goarch string) bool {
	for _, c := range sc.Includes.Caps {
		if !slices.Contains(caps, c) {
			return false
		}
	}
	for _, c := range sc.Excludes.Caps {
		if slices.Contains(caps, c) {
			return false
		}
	}
	if len(sc.Includes.Arches) > 0 && !slices.Contains(sc.Includes.Arches, goarch) {
		return false
	}
	if slices.Contains(sc.Excludes.Arches, goarch) {
		return false
	}
	if sc.Includes.MinKernel != "" && !kernelAtMost(sc.Includes.MinKernel, seccompKernelFloor) {
		return false
	}
	return true
}

// kernelAtMost reports whether kernel version v ("major.minor") is at most
// floor. An unparsable version returns false, which leaves its rule out.
func kernelAtMost(v, floor string) bool {
	vMaj, vMin, ok := parseKernelVersion(v)
	if !ok {
		return false
	}
	fMaj, fMin, ok := parseKernelVersion(floor)
	if !ok {
		return false
	}
	if vMaj != fMaj {
		return vMaj < fMaj
	}
	return vMin <= fMin
}

// parseKernelVersion splits "major.minor" into its numbers.
func parseKernelVersion(v string) (major, minor int, ok bool) {
	majS, minS, found := strings.Cut(v, ".")
	if !found {
		return 0, 0, false
	}
	major, err := strconv.Atoi(majS)
	if err != nil {
		return 0, 0, false
	}
	minor, err = strconv.Atoi(minS)
	if err != nil {
		return 0, 0, false
	}
	return major, minor, true
}
