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

package containerruntime

import (
	"errors"
	"slices"
	"strings"
)

// Ulimit is one resource limit of a container's init process, in docker's
// --ulimit spelling: Name is "nofile", "memlock" and so on.
type Ulimit struct {
	Name string `json:"name" yaml:"name"`
	Soft int64  `json:"soft" yaml:"soft"`
	Hard int64  `json:"hard" yaml:"hard"`
}

// ulimitResources are the setrlimit resources a Ulimit may name.
var ulimitResources = []string{
	"as", "core", "cpu", "data", "fsize", "locks", "memlock", "msgqueue",
	"nice", "nofile", "nproc", "rss", "rtprio", "rttime", "sigpending", "stack",
}

// Validate refuses an unknown resource, a negative limit, and a soft limit
// above the hard one.
func (u Ulimit) Validate() error {
	if !slices.Contains(ulimitResources, u.Name) {
		return errors.New("unknown resource " + `"` + u.Name + `"`)
	}
	if u.Soft < 0 || u.Hard < 0 {
		return errors.New("limits cannot be negative")
	}
	if u.Soft > u.Hard {
		return errors.New("soft limit exceeds hard limit")
	}
	return nil
}

// ociRlimits maps validated ulimits onto the OCI process rlimits.
func ociRlimits(ulimits []Ulimit) []ociRlimit {
	var rlimits []ociRlimit
	for _, u := range ulimits {
		rlimits = append(rlimits, ociRlimit{
			Type: "RLIMIT_" + strings.ToUpper(u.Name),
			Soft: uint64(u.Soft), // #nosec G115 -- Validate refuses negative limits.
			Hard: uint64(u.Hard), // #nosec G115 -- Validate refuses negative limits.
		})
	}
	return rlimits
}
