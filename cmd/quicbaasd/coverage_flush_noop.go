//go:build !coverage

package main

// flushCoverage and setupCoverageSignalHook are no-ops in production builds.
// The coverage-instrumented variant (build tag 'coverage') flushes the Go
// coverage profile to GOCOVERDIR on graceful shutdown and on SIGINT/SIGTERM,
// used only by the integration-coverage pipeline.
func flushCoverage() {}

func setupCoverageSignalHook() {}
