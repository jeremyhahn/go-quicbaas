//go:build coverage

package main

import (
	"fmt"
	"os"
	"runtime/coverage"
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

// setupCoverageSignalHook is a no-op: coverage is flushed from runDaemon's
// graceful shutdown path.
//
// It previously installed a memguard.CatchSignal handler so the profile was
// written before memguard's secure-wipe os.Exit(1). That is no longer needed
// — and must not be reinstated — because main() now arms the secure subsystem
// with secure.EnablePurge() instead of secure.CatchInterrupt(): no memguard
// signal handler is installed at all, so SIGINT/SIGTERM reach runDaemon's own
// handler, which runs Server.Shutdown (closing the embedded qrdb node and its
// PebbleDB) and then calls flushCoverage. Registering memguard.CatchSignal
// here would reintroduce the hard os.Exit(1) that skipped the graceful
// shutdown and dropped every unflushed PebbleDB write.
func setupCoverageSignalHook() {}
