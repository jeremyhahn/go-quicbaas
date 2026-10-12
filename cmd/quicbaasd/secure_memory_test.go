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

import (
	"errors"
	"math"
	"os"
	"strings"
	"testing"

	qrdbsecure "github.com/jeremyhahn/go-qrdb/sdk/go/secure"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// The test binary runs the daemon and init many times, but SetAllocator
// succeeds once per process and a library's first locked allocation installs
// a default. So every test runs against doubles that install nothing and see
// an unbounded host ceiling; the tests below replace them per case. This is
// an init function rather than TestMain because the integration build of
// this package already declares one.
func init() {
	discoverLockedMemoryLimit = func() (qrdbsecure.LockedMemoryLimit, error) {
		return qrdbsecure.LockedMemoryLimit{Source: qrdbsecure.LimitUnlimited}, nil
	}
	newSecureAllocator = func(int64) (*qrdbsecure.Allocator, error) { return nil, nil }
	setSecureAllocator = func(*qrdbsecure.Allocator) error { return nil }
}

// errLimitUnreadable stands in for a getrlimit the sandbox filtered.
var errLimitUnreadable = errors.New("getrlimit filtered")

// secureMemoryDoubles records what installSecureMemoryBudget asked of the
// go-enclave lifecycle.
type secureMemoryDoubles struct {
	requested []int64
	installed []*qrdbsecure.Allocator
}

// useSecureMemoryDoubles points the lifecycle at doubles for one test: the
// host ceiling is limit, a new allocator is refused with newErr, installing
// one returns setErr, and an already-installed allocator is existing.
func useSecureMemoryDoubles(t *testing.T, limit qrdbsecure.LockedMemoryLimit, newErr, setErr error, existing *qrdbsecure.Allocator) *secureMemoryDoubles {
	t.Helper()
	discover, build, set, process := discoverLockedMemoryLimit, newSecureAllocator, setSecureAllocator, processSecureAllocator
	t.Cleanup(func() {
		discoverLockedMemoryLimit, newSecureAllocator, setSecureAllocator, processSecureAllocator = discover, build, set, process
	})

	d := &secureMemoryDoubles{}
	discoverLockedMemoryLimit = func() (qrdbsecure.LockedMemoryLimit, error) { return limit, nil }
	newSecureAllocator = func(n int64) (*qrdbsecure.Allocator, error) {
		d.requested = append(d.requested, n)
		if newErr != nil {
			return nil, newErr
		}
		return smallAllocator(t, n), nil
	}
	setSecureAllocator = func(a *qrdbsecure.Allocator) error {
		d.installed = append(d.installed, a)
		return setErr
	}
	processSecureAllocator = func() (*qrdbsecure.Allocator, error) { return existing, nil }
	return d
}

// smallAllocator builds a real, uninstalled allocator reporting limit. It is
// constructed at one page, which fits any host ceiling, and then forced to
// limit; nothing is ever allocated through it, so no memory is locked.
func smallAllocator(t *testing.T, limit int64) *qrdbsecure.Allocator {
	t.Helper()
	a, err := qrdbsecure.NewAllocatorWithinRLimit(int64(os.Getpagesize()))
	require.NoError(t, err)
	_, err = a.ForceLimit(limit)
	require.NoError(t, err)
	return a
}

func bounded(bytes int64) qrdbsecure.LockedMemoryLimit {
	return qrdbsecure.LockedMemoryLimit{Bytes: bytes, Bounded: true, Source: qrdbsecure.LimitRLimit}
}

func TestResolveSecureMemoryBudget(t *testing.T) {
	page := int64(os.Getpagesize())
	cases := map[string]struct {
		configuredMiB int64
		limit         qrdbsecure.LockedMemoryLimit
		wantBytes     int64
		wantSource    string
	}{
		"configured is used as given":             {16, bounded(1 << 20), 16 << 20, secureMemoryBudgetConfigured},
		"unbounded host gets the default":         {0, qrdbsecure.LockedMemoryLimit{Source: qrdbsecure.LimitNotEnforced}, 64 << 20, secureMemoryBudgetDefault},
		"roomy host gets the default":             {0, bounded(1 << 30), 64 << 20, secureMemoryBudgetDefault},
		"stock container limit is derived":        {0, bounded(8 << 20), 6 << 20, secureMemoryBudgetDerived},
		"derived budget is rounded down to pages": {0, bounded(8<<20 + page/2), 6 << 20, secureMemoryBudgetDerived},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			useSecureMemoryDoubles(t, tc.limit, nil, nil, nil)
			budget, err := resolveSecureMemoryBudget(tc.configuredMiB)
			require.NoError(t, err)
			require.Equal(t, tc.wantBytes, budget.bytes)
			require.Equal(t, tc.wantSource, budget.source)
		})
	}
}

func TestResolveSecureMemoryBudget_Refusals(t *testing.T) {
	t.Run("negative", func(t *testing.T) {
		_, err := resolveSecureMemoryBudget(-1)
		var invalid *InvalidSecureMemoryBudgetError
		require.ErrorAs(t, err, &invalid)
		require.Equal(t, int64(-1), invalid.BudgetMiB)
	})
	t.Run("overflows converting to bytes", func(t *testing.T) {
		_, err := resolveSecureMemoryBudget(math.MaxInt64>>20 + 1)
		var invalid *InvalidSecureMemoryBudgetError
		require.ErrorAs(t, err, &invalid)
	})
	t.Run("host limit too small after headroom", func(t *testing.T) {
		useSecureMemoryDoubles(t, bounded(2<<20+512<<10), nil, nil, nil)
		_, err := resolveSecureMemoryBudget(0)
		var tooSmall *SecureMemoryLimitTooSmallError
		require.ErrorAs(t, err, &tooSmall)
		require.Equal(t, int64(2<<20+512<<10), tooSmall.LimitBytes)
		require.Contains(t, err.Error(), "RLIMIT_MEMLOCK (ulimit -l")
	})
	t.Run("host limit unreadable", func(t *testing.T) {
		useSecureMemoryDoubles(t, bounded(0), nil, nil, nil)
		discoverLockedMemoryLimit = func() (qrdbsecure.LockedMemoryLimit, error) {
			return qrdbsecure.LockedMemoryLimit{}, errLimitUnreadable
		}
		_, err := resolveSecureMemoryBudget(0)
		var budgetErr *SecureMemoryBudgetError
		require.ErrorAs(t, err, &budgetErr)
		require.ErrorIs(t, err, errLimitUnreadable)
	})
}

func TestInstallSecureMemoryBudget_InstallsTheResolvedBudget(t *testing.T) {
	d := useSecureMemoryDoubles(t, bounded(1<<30), nil, nil, nil)
	budget, err := installSecureMemoryBudget(32)
	require.NoError(t, err)
	require.Equal(t, int64(32<<20), budget.bytes)
	require.Equal(t, []int64{32 << 20}, d.requested)
	require.Len(t, d.installed, 1)
	require.Equal(t, int64(32<<20), d.installed[0].Limit())
}

func TestInstallSecureMemoryBudget_RefusesABudgetOverTheHostCeiling(t *testing.T) {
	ceiling := bounded(8 << 20)
	d := useSecureMemoryDoubles(t, ceiling, &qrdbsecure.RLimitError{Requested: 128 << 20, Limit: ceiling}, nil, nil)
	_, err := installSecureMemoryBudget(128)
	var budgetErr *SecureMemoryBudgetError
	require.ErrorAs(t, err, &budgetErr)
	require.Equal(t, int64(128), budgetErr.BudgetMiB)
	require.ErrorIs(t, err, qrdbsecure.ErrBudgetExceedsRLimit)
	require.Contains(t, err.Error(), "server.secure_memory_budget_mib to a value that fits")
	require.Empty(t, d.installed, "a refused budget must not be installed")
}

func TestInstallSecureMemoryBudget_RefusesAnInvalidConfiguration(t *testing.T) {
	d := useSecureMemoryDoubles(t, bounded(1<<30), nil, nil, nil)
	_, err := installSecureMemoryBudget(-8)
	var invalid *InvalidSecureMemoryBudgetError
	require.ErrorAs(t, err, &invalid)
	require.Empty(t, d.requested, "an invalid budget must not reach the allocator")
	require.Empty(t, d.installed)
}

func TestInstallSecureMemoryBudget_AlreadyInstalled(t *testing.T) {
	t.Run("same budget is accepted", func(t *testing.T) {
		useSecureMemoryDoubles(t, bounded(1<<30), nil, qrdbsecure.ErrAllocatorInstalled, smallAllocator(t, 32<<20))
		budget, err := installSecureMemoryBudget(32)
		require.NoError(t, err)
		require.Equal(t, int64(32<<20), budget.bytes)
	})
	t.Run("a different budget is refused", func(t *testing.T) {
		useSecureMemoryDoubles(t, bounded(1<<30), nil, qrdbsecure.ErrAllocatorInstalled, smallAllocator(t, 8<<20))
		_, err := installSecureMemoryBudget(32)
		var mismatch *SecureMemoryBudgetMismatchError
		require.ErrorAs(t, err, &mismatch)
		require.Equal(t, int64(32<<20), mismatch.ResolvedBytes)
		require.Equal(t, int64(8<<20), mismatch.InstalledBytes)
	})
}

func TestInstallSecureMemoryBudget_ReportsAFailedInstall(t *testing.T) {
	useSecureMemoryDoubles(t, bounded(1<<30), nil, qrdbsecure.ErrNilAllocator, nil)
	_, err := installSecureMemoryBudget(32)
	var budgetErr *SecureMemoryBudgetError
	require.ErrorAs(t, err, &budgetErr)
	require.ErrorIs(t, err, qrdbsecure.ErrNilAllocator)
	require.False(t, strings.Contains(err.Error(), "ulimit"), "only a host-ceiling failure carries the rlimit remedy")
}

// TestRunDaemon_RefusesToStartWithoutABudget proves the budget is a startup
// gate: an invalid one stops the daemon before it creates anything.
func TestRunDaemon_RefusesToStartWithoutABudget(t *testing.T) {
	cfg = config.Default()
	cfg.Storage.DataDir = t.TempDir() + "/never-created"
	cfg.Server.SecureMemoryBudgetMiB = -1
	var err error
	log, err = logger.New(logger.LogConfig{Level: "info", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	err = runDaemon("budget-test")
	var invalid *InvalidSecureMemoryBudgetError
	require.ErrorAs(t, err, &invalid)
	require.NoDirExists(t, cfg.Storage.DataDir, "the daemon prepared itself before refusing")
}

// TestBuildInitConfig_CarriesTheSecureMemoryBudget proves init installs the
// operator's budget, not a default, for its embedded server.
func TestBuildInitConfig_CarriesTheSecureMemoryBudget(t *testing.T) {
	saved := cfg
	t.Cleanup(func() { cfg = saved })
	cfg = config.Default()
	cfg.Server.SecureMemoryBudgetMiB = 24
	initCfg := buildInitConfig(t.TempDir(), &inlineInitFlags{barrierStrategy: "none"}, false)
	require.Equal(t, int64(24), initCfg.Server.SecureMemoryBudgetMiB)

	cfg = nil
	initCfg = buildInitConfig(t.TempDir(), &inlineInitFlags{barrierStrategy: "none"}, false)
	require.Zero(t, initCfg.Server.SecureMemoryBudgetMiB)
}
