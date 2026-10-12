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

package main

// This file installs the process-wide locked-memory budget every protected
// buffer and every opened secret in quicbaasd admits against. The budget
// belongs to the binary that owns the process, so it is installed here, once,
// before anything allocates locked memory; library code never installs one.
// The derivation mirrors go-qrdb's cmd/qrdbd, the reference wiring the SDK's
// secure package documents.

import (
	"errors"
	"fmt"
	"math"
	"os"

	qrdbsecure "github.com/jeremyhahn/go-qrdb/sdk/go/secure"
)

const (
	// defaultSecureMemoryBudgetMiB is the budget an unconfigured daemon gets
	// when the host's RLIMIT_MEMLOCK leaves room for it, and the ceiling of a
	// budget derived from a smaller limit. Keys, credentials and PINs are
	// KB-scale; the rest is headroom for concurrent unseal and rotation.
	defaultSecureMemoryBudgetMiB int64 = 64

	// secureMemoryHeadroomBytes is the part of a bounded RLIMIT_MEMLOCK a
	// derived budget leaves for locked memory the allocator does not account
	// for: sealing a Secret, the process sealer's own key, Shamir shares held
	// toward an unseal quorum. All are page-scale and protocol-bounded.
	secureMemoryHeadroomBytes int64 = 2 << 20

	// minSecureMemoryBudgetBytes is the smallest budget a derivation may
	// settle on. Below it the daemon refuses to start rather than fail later,
	// under load, with an exhausted budget far from the host setting that
	// caused it.
	minSecureMemoryBudgetBytes int64 = 1 << 20

	// secureMemoryLimitRemedy tells the operator what to change when the
	// host's locked-memory ceiling is what refused the budget.
	secureMemoryLimitRemedy = "raise RLIMIT_MEMLOCK (ulimit -l, LimitMEMLOCK= in a systemd unit, " +
		"or --ulimit memlock / --cap-add IPC_LOCK for a container) or set " +
		"server.secure_memory_budget_mib to a value that fits within it"
)

// Budget sources, logged with the installed budget.
const (
	secureMemoryBudgetConfigured = "configured"
	secureMemoryBudgetDefault    = "default"
	secureMemoryBudgetDerived    = "derived from RLIMIT_MEMLOCK"
)

// The go-enclave lifecycle calls are one-shot per process: SetAllocator
// succeeds once, and the first ProcessAllocator call installs a default.
// They are indirected so the test binary, which runs the daemon many times,
// can replace them; production never touches these variables.
var (
	discoverLockedMemoryLimit = qrdbsecure.DiscoverLockedMemoryLimit
	newSecureAllocator        = qrdbsecure.NewAllocatorWithinRLimit
	setSecureAllocator        = qrdbsecure.SetAllocator
	processSecureAllocator    = qrdbsecure.ProcessAllocator
)

// SecureMemoryBudgetError is returned when the process-wide locked-memory
// budget (server.secure_memory_budget_mib) cannot be resolved or installed.
// BudgetMiB is the configured figure, 0 when the budget was to be derived
// from the host.
type SecureMemoryBudgetError struct {
	BudgetMiB int64
	Err       error
}

func (e *SecureMemoryBudgetError) Error() string {
	msg := fmt.Sprintf("secure memory budget (server.secure_memory_budget_mib=%d): %v", e.BudgetMiB, e.Err)
	var tooSmall *SecureMemoryLimitTooSmallError
	if errors.Is(e.Err, qrdbsecure.ErrBudgetExceedsRLimit) || errors.As(e.Err, &tooSmall) {
		msg += "; " + secureMemoryLimitRemedy
	}
	return msg
}

func (e *SecureMemoryBudgetError) Unwrap() error { return e.Err }

// InvalidSecureMemoryBudgetError is wrapped by SecureMemoryBudgetError when
// the configured budget is negative or too large to convert to bytes. It is a
// configuration mistake, refused without consulting the host.
type InvalidSecureMemoryBudgetError struct {
	BudgetMiB int64
	Reason    string
}

func (e *InvalidSecureMemoryBudgetError) Error() string {
	return fmt.Sprintf("invalid secure memory budget %d MiB: %s", e.BudgetMiB, e.Reason)
}

// SecureMemoryLimitTooSmallError is wrapped by SecureMemoryBudgetError when no
// budget was configured and the host's RLIMIT_MEMLOCK, less the headroom,
// leaves less than the minimum budget. All figures are in bytes.
type SecureMemoryLimitTooSmallError struct {
	LimitBytes    int64
	HeadroomBytes int64
	MinimumBytes  int64
}

func (e *SecureMemoryLimitTooSmallError) Error() string {
	return fmt.Sprintf("RLIMIT_MEMLOCK of %d bytes leaves less than the %d-byte minimum budget after %d bytes of headroom",
		e.LimitBytes, e.MinimumBytes, e.HeadroomBytes)
}

// SecureMemoryBudgetMismatchError is wrapped by SecureMemoryBudgetError when
// a process allocator was already installed and its limit is not the budget
// this daemon resolved: the resolved budget would otherwise be silently
// discarded in favour of whatever installed first.
type SecureMemoryBudgetMismatchError struct {
	ResolvedBytes  int64
	InstalledBytes int64
}

func (e *SecureMemoryBudgetMismatchError) Error() string {
	return fmt.Sprintf("a locked-memory budget of %d bytes is already installed, not the resolved %d bytes",
		e.InstalledBytes, e.ResolvedBytes)
}

// secureMemoryBudget is a resolved locked-memory budget and how it was
// arrived at.
type secureMemoryBudget struct {
	bytes  int64
	source string
}

// resolveSecureMemoryBudget turns server.secure_memory_budget_mib into the
// budget to install.
//
// A positive value is the operator's choice and is used as given; the
// allocator constructor then refuses it if it exceeds a binding
// RLIMIT_MEMLOCK. Zero derives the budget from the host:
// min(defaultSecureMemoryBudgetMiB, RLIMIT_MEMLOCK - headroom), rounded down
// to whole pages. An unbounded ceiling (unlimited, or CAP_IPC_LOCK) gets the
// full default. A ceiling too small for the minimum, or one that cannot be
// read, fails closed. A negative value, or one that overflows converting MiB
// to bytes, fails closed without consulting the host.
func resolveSecureMemoryBudget(configuredMiB int64) (secureMemoryBudget, error) {
	switch {
	case configuredMiB < 0:
		return secureMemoryBudget{}, &SecureMemoryBudgetError{BudgetMiB: configuredMiB,
			Err: &InvalidSecureMemoryBudgetError{BudgetMiB: configuredMiB, Reason: "must not be negative"}}
	case configuredMiB > math.MaxInt64>>20:
		return secureMemoryBudget{}, &SecureMemoryBudgetError{BudgetMiB: configuredMiB,
			Err: &InvalidSecureMemoryBudgetError{BudgetMiB: configuredMiB, Reason: "overflows converting to bytes"}}
	case configuredMiB > 0:
		return secureMemoryBudget{bytes: configuredMiB << 20, source: secureMemoryBudgetConfigured}, nil
	}

	limit, err := discoverLockedMemoryLimit()
	if err != nil {
		return secureMemoryBudget{}, &SecureMemoryBudgetError{Err: err}
	}
	ceiling := defaultSecureMemoryBudgetMiB << 20
	if !limit.Bounded {
		return secureMemoryBudget{bytes: ceiling, source: secureMemoryBudgetDefault}, nil
	}
	derived := limit.Bytes - secureMemoryHeadroomBytes
	if derived >= ceiling {
		return secureMemoryBudget{bytes: ceiling, source: secureMemoryBudgetDefault}, nil
	}
	if page := int64(os.Getpagesize()); derived > 0 {
		derived -= derived % page
	}
	if derived < minSecureMemoryBudgetBytes {
		return secureMemoryBudget{}, &SecureMemoryBudgetError{Err: &SecureMemoryLimitTooSmallError{
			LimitBytes:    limit.Bytes,
			HeadroomBytes: secureMemoryHeadroomBytes,
			MinimumBytes:  minSecureMemoryBudgetBytes,
		}}
	}
	return secureMemoryBudget{bytes: derived, source: secureMemoryBudgetDerived}, nil
}

// installSecureMemoryBudget resolves the budget, builds an allocator within
// the host's RLIMIT_MEMLOCK and installs it as the process allocator. It must
// run before anything in the process allocates locked memory.
//
// An allocator already being installed is not by itself fatal: the daemon may
// run more than once in one process. It is accepted only when its limit is
// the budget just resolved; otherwise the operator's figure was discarded in
// favour of whatever installed first, and that is refused.
func installSecureMemoryBudget(configuredMiB int64) (secureMemoryBudget, error) {
	budget, err := resolveSecureMemoryBudget(configuredMiB)
	if err != nil {
		return secureMemoryBudget{}, err
	}
	allocator, err := newSecureAllocator(budget.bytes)
	if err != nil {
		return secureMemoryBudget{}, &SecureMemoryBudgetError{BudgetMiB: configuredMiB, Err: err}
	}
	err = setSecureAllocator(allocator)
	if err == nil {
		return budget, nil
	}
	if !errors.Is(err, qrdbsecure.ErrAllocatorInstalled) {
		return secureMemoryBudget{}, &SecureMemoryBudgetError{BudgetMiB: configuredMiB, Err: err}
	}
	installed, err := processSecureAllocator()
	if err != nil {
		return secureMemoryBudget{}, &SecureMemoryBudgetError{BudgetMiB: configuredMiB, Err: err}
	}
	if installed.Limit() != budget.bytes {
		return secureMemoryBudget{}, &SecureMemoryBudgetError{BudgetMiB: configuredMiB,
			Err: &SecureMemoryBudgetMismatchError{ResolvedBytes: budget.bytes, InstalledBytes: installed.Limit()}}
	}
	return budget, nil
}
