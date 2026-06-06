//go:build coverage

package main

import (
	"fmt"
	"os"
	"runtime/coverage"
	"syscall"

	"github.com/awnumar/memguard"
)

// flushCoverageProfile writes the accumulated coverage profile to the directory
// named by GOCOVERDIR. Safe to call multiple times; the runtime tolerates
// repeated flushes. No-op when GOCOVERDIR is unset.
func flushCoverageProfile() {
	dir := os.Getenv("GOCOVERDIR")
	if dir == "" {
		return
	}
	if err := coverage.WriteMetaDir(dir); err != nil {
		fmt.Fprintf(os.Stderr, "quicbaasd: coverage WriteMetaDir(%q): %v\n", dir, err)
	}
	if err := coverage.WriteCountersDir(dir); err != nil {
		fmt.Fprintf(os.Stderr, "quicbaasd: coverage WriteCountersDir(%q): %v\n", dir, err)
	}
}

// flushCoverage flushes the coverage profile from the daemon's graceful
// shutdown path (called from runDaemon). It is only compiled under the
// 'coverage' build tag; production builds use the no-op in
// coverage_flush_noop.go.
func flushCoverage() { flushCoverageProfile() }

// setupCoverageSignalHook re-registers the process signal handler so the
// coverage profile is flushed to GOCOVERDIR before the process exits on
// SIGINT/SIGTERM.
//
// Rationale: secure.CatchInterrupt installs a memguard signal handler that
// wipes mlocked memory and then calls os.Exit(1). That abrupt exit races
// ahead of the daemon's graceful-shutdown goroutine, so neither the graceful
// flushCoverage call nor the runtime's implicit exit-hook reliably write the
// counter file. memguard's CatchSignal documents that only the most recent
// registration takes effect, so we register last: our handler flushes
// coverage first, after which memguard still performs its secure-wipe exit.
// This makes the daemon-boot coverage capture deterministic under the
// signal-driven termination used by `docker compose down`.
//
// Compiled only under the 'coverage' build tag; the production no-op variant
// lives in coverage_flush_noop.go.
func setupCoverageSignalHook() {
	memguard.CatchSignal(func(_ os.Signal) {
		flushCoverageProfile()
	}, syscall.SIGINT, syscall.SIGTERM)
}
