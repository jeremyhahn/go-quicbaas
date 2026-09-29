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

package orchestration

import (
	"slices"
	"strconv"
	"strings"
)

// UlimitMemlock is the ulimit name for RLIMIT_MEMLOCK, the most memory a
// process may lock into RAM (mlock, mlockall).
const UlimitMemlock = "memlock"

// ulimitNames is every resource name docker and podman accept for --ulimit.
// A name outside it is a typo, and the runtime would refuse to start the
// container over it, so it is refused when the spec or policy is validated
// instead of at launch.
var ulimitNames = map[string]struct{}{
	"core": {}, "cpu": {}, "data": {}, "fsize": {}, "locks": {},
	UlimitMemlock: {}, "msgqueue": {}, "nice": {}, "nofile": {}, "nproc": {},
	"rss": {}, "rtprio": {}, "rttime": {}, "sigpending": {}, "stack": {},
}

// ContainerUlimit is one per-process resource limit inside a container,
// emitted as --ulimit Name=Soft:Hard. Values are in the resource's own unit:
// bytes for memlock, a count for nofile.
type ContainerUlimit struct {
	// Name is the resource, in the runtime's spelling ("memlock", "nofile").
	Name string `json:"name"`

	// Soft is the limit a process starts with. It may not exceed Hard.
	Soft int64 `json:"soft"`

	// Hard is the ceiling an unprivileged process may raise Soft to.
	Hard int64 `json:"hard"`
}

// RunArg renders the limit as the value of a --ulimit flag.
func (u ContainerUlimit) RunArg() string {
	return u.Name + "=" + strconv.FormatInt(u.Soft, 10) + ":" + strconv.FormatInt(u.Hard, 10)
}

// Validate reports a limit the runtime could not apply: an unknown resource
// name, a negative value, or a soft limit above the hard one. There is no
// "unlimited" (-1): a sandbox limit is always a number an operator chose.
func (u ContainerUlimit) Validate() error {
	if _, ok := ulimitNames[u.Name]; !ok {
		return &InputValidationError{Field: "ulimits", Message: "unknown ulimit " + strconv.Quote(u.Name)}
	}
	if u.Soft < 0 || u.Hard < 0 {
		return &InputValidationError{Field: "ulimits", Message: u.Name + ": limits cannot be negative"}
	}
	if u.Soft > u.Hard {
		return &InputValidationError{Field: "ulimits", Message: u.Name + ": soft limit exceeds hard limit"}
	}
	return nil
}

// ParseContainerUlimit parses the runtime's own --ulimit syntax,
// "name=soft[:hard]". A missing hard limit equals the soft one, as it does
// for the runtime.
func ParseContainerUlimit(raw string) (ContainerUlimit, error) {
	name, limits, ok := strings.Cut(strings.TrimSpace(raw), "=")
	if !ok || name == "" || limits == "" {
		return ContainerUlimit{}, &InputValidationError{
			Field:   "ulimits",
			Message: strconv.Quote(raw) + " must be name=soft[:hard]",
		}
	}
	softText, hardText, hasHard := strings.Cut(limits, ":")
	if !hasHard {
		hardText = softText
	}
	soft, softErr := strconv.ParseInt(softText, 10, 64)
	hard, hardErr := strconv.ParseInt(hardText, 10, 64)
	if softErr != nil || hardErr != nil {
		return ContainerUlimit{}, &InputValidationError{
			Field:   "ulimits",
			Message: strconv.Quote(raw) + ": limits must be integers",
		}
	}
	u := ContainerUlimit{Name: strings.ToLower(name), Soft: soft, Hard: hard}
	if err := u.Validate(); err != nil {
		return ContainerUlimit{}, err
	}
	return u, nil
}

// ParseContainerUlimits parses every entry with ParseContainerUlimit and
// refuses a resource named twice. It is the one parser of the operator's
// "name=soft[:hard]" configuration, for the embedded agent and `quicbaas
// agent serve` alike. An empty list parses to nil.
func ParseContainerUlimits(raw []string) ([]ContainerUlimit, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]ContainerUlimit, 0, len(raw))
	for _, entry := range raw {
		u, err := ParseContainerUlimit(entry)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	if err := validateUlimits(out); err != nil {
		return nil, err
	}
	return out, nil
}

// validateUlimits validates every limit and refuses a resource named twice,
// which the runtime would resolve by argument order — an order
// ContainerConfigDigest does not measure.
func validateUlimits(ulimits []ContainerUlimit) error {
	seen := make(map[string]struct{}, len(ulimits))
	for _, u := range ulimits {
		if err := u.Validate(); err != nil {
			return err
		}
		if _, dup := seen[u.Name]; dup {
			return &InputValidationError{Field: "ulimits", Message: u.Name + ": named more than once"}
		}
		seen[u.Name] = struct{}{}
	}
	return nil
}

// sortedUlimits returns a copy of ulimits ordered by name: the one order both
// ContainerConfigDigest and the emitted command line use, so a spec cannot
// measure one way and launch another.
func sortedUlimits(ulimits []ContainerUlimit) []ContainerUlimit {
	if len(ulimits) == 0 {
		return nil
	}
	out := slices.Clone(ulimits)
	slices.SortFunc(out, func(a, b ContainerUlimit) int { return strings.Compare(a.Name, b.Name) })
	return out
}
