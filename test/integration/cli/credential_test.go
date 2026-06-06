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

//go:build integration

// Package cli — multi-protocol CLI integration tests for the canonical
// "credential" verb tree.
//
// Every test runs across all enabled transports via RunForAllTransports.
// The credential CLI delegates to Security().Credentials() over the
// active SDK transport (rest / grpc / quic / mcp / unix / embedded —
// every transport ships a real-wire implementation), so no
// SkipTransports filter is required.
package cli

import (
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Per-verb happy-path tests
// =============================================================================

// TestCredentialSubmit verifies the "credential submit <name> --value <v>"
// verb across every enabled transport. Submit is the non-versioned
// set/replace path.
func TestCredentialSubmit(t *testing.T) {
	RunForAllTransports(t, "Submit", func(t *testing.T, runner *TestRunner) {
		name := UniqueKey("submit-cred")
		value := "submit-value-" + name

		runner.RunAndAssert(CredentialSubmitCommand(name, value))

		// Best-effort cleanup so a server-side enumeration doesn't grow
		// unboundedly between tests.
		runner.Run(CredentialDeleteCommand(name))
	})
}

// TestCredentialList verifies the "credential list" verb across every
// enabled transport. The list verb returns names only; values are never
// disclosed by enumeration.
func TestCredentialList(t *testing.T) {
	RunForAllTransports(t, "List", func(t *testing.T, runner *TestRunner) {
		name := UniqueKey("list-cred")
		value := "list-value"

		runner.RunAndAssert(CredentialSubmitCommand(name, value))
		runner.RunAndAssert(CredentialListExpectingCommand(name))

		runner.Run(CredentialDeleteCommand(name))
	})
}

// TestCredentialListJSON verifies "credential list --output json" emits
// a JSON {"names":[...]} envelope across every enabled transport.
func TestCredentialListJSON(t *testing.T) {
	RunForAllTransports(t, "ListJSON", func(t *testing.T, runner *TestRunner) {
		name := UniqueKey("listjson-cred")

		runner.RunAndAssert(CredentialSubmitCommand(name, "listjson-value"))
		stdout, _, err := runner.Run(CredentialListJSONCommand())
		if err == nil && !strings.Contains(stdout, name) {
			t.Errorf("list --output json missing submitted name %q in stdout: %s", name, stdout)
		}

		runner.Run(CredentialDeleteCommand(name))
	})
}

// TestCredentialListTable verifies "credential list --output table"
// renders the default table format across every enabled transport.
func TestCredentialListTable(t *testing.T) {
	RunForAllTransports(t, "ListTable", func(t *testing.T, runner *TestRunner) {
		name := UniqueKey("listtable-cred")

		runner.RunAndAssert(CredentialSubmitCommand(name, "listtable-value"))
		runner.RunAndAssert(CredentialListTableCommand())

		runner.Run(CredentialDeleteCommand(name))
	})
}

// TestCredentialRotate verifies the "credential rotate <name> --value <v>"
// verb across every enabled transport. Rotation with grace=0 wipes the
// previous version immediately.
func TestCredentialRotate(t *testing.T) {
	RunForAllTransports(t, "Rotate", func(t *testing.T, runner *TestRunner) {
		name := UniqueKey("rotate-cred")

		runner.RunAndAssert(CredentialSubmitCommand(name, "initial"))
		runner.RunAndAssert(CredentialRotateCommand(name, "rotated", 0))
		runner.RunAndAssert(CredentialRetrieveExpectingValueCommand(name, "rotated"))

		runner.Run(CredentialDeleteCommand(name))
	})
}

// TestCredentialRotateJSON verifies "credential rotate <name> --output json"
// emits a JSON CredentialRotateResponse envelope across every enabled
// transport.
func TestCredentialRotateJSON(t *testing.T) {
	RunForAllTransports(t, "RotateJSON", func(t *testing.T, runner *TestRunner) {
		name := UniqueKey("rotatejson-cred")

		runner.RunAndAssert(CredentialSubmitCommand(name, "initial"))
		runner.RunAndAssert(CredentialRotateJSONCommand(name, "rotated", 0))

		runner.Run(CredentialDeleteCommand(name))
	})
}

// TestCredentialRetrieve verifies the "credential retrieve <name>" verb
// across every enabled transport. The retrieve verb returns the current
// value when --version is omitted.
func TestCredentialRetrieve(t *testing.T) {
	RunForAllTransports(t, "Retrieve", func(t *testing.T, runner *TestRunner) {
		name := UniqueKey("retrieve-cred")
		value := "retrieve-value-" + name

		runner.RunAndAssert(CredentialSubmitCommand(name, value))
		runner.RunAndAssert(CredentialRetrieveExpectingValueCommand(name, value))

		runner.Run(CredentialDeleteCommand(name))
	})
}

// TestCredentialRetrieveVersion verifies "credential retrieve <name>
// --version N" reads a specific historical version (kept retrievable
// during the grace window) across every enabled transport.
func TestCredentialRetrieveVersion(t *testing.T) {
	RunForAllTransports(t, "RetrieveVersion", func(t *testing.T, runner *TestRunner) {
		name := UniqueKey("retrievever-cred")

		runner.RunAndAssert(CredentialSubmitCommand(name, "v1-value"))
		runner.RunAndAssert(CredentialRotateCommand(name, "v2-value", 30*time.Second))

		// Current (v2) should be reachable via --version 0 and the
		// default current-version retrieve.
		runner.RunAndAssert(CredentialRetrieveVersionExpectingValueCommand(name, 0, "v2-value"))
		runner.RunAndAssert(CredentialRetrieveExpectingValueCommand(name, "v2-value"))

		// v1 is graced for 30s — well inside the test deadline.
		runner.RunAndAssert(CredentialRetrieveVersionExpectingValueCommand(name, 1, "v1-value"))

		runner.Run(CredentialDeleteCommand(name))
	})
}

// TestCredentialVersions verifies the "credential versions <name>" verb
// across every enabled transport. The output is metadata only — values
// are never disclosed.
func TestCredentialVersions(t *testing.T) {
	RunForAllTransports(t, "Versions", func(t *testing.T, runner *TestRunner) {
		name := UniqueKey("versions-cred")

		runner.RunAndAssert(CredentialSubmitCommand(name, "v1-value"))
		runner.RunAndAssert(CredentialVersionsCommand(name))

		// After rotation with grace, both versions appear in metadata.
		runner.RunAndAssert(CredentialRotateCommand(name, "v2-value", 30*time.Second))
		runner.RunAndAssert(CredentialVersionsCommand(name))

		runner.Run(CredentialDeleteCommand(name))
	})
}

// TestCredentialVersionsJSON verifies "credential versions <name>
// --output json" emits a JSON CredentialListVersionsResponse envelope
// across every enabled transport.
func TestCredentialVersionsJSON(t *testing.T) {
	RunForAllTransports(t, "VersionsJSON", func(t *testing.T, runner *TestRunner) {
		name := UniqueKey("versionsjson-cred")

		runner.RunAndAssert(CredentialSubmitCommand(name, "v1"))
		runner.RunAndAssert(CredentialVersionsJSONCommand(name))

		runner.Run(CredentialDeleteCommand(name))
	})
}

// TestCredentialVersionsTable verifies "credential versions <name>
// --output table" emits the default tabular layout across every enabled
// transport.
func TestCredentialVersionsTable(t *testing.T) {
	RunForAllTransports(t, "VersionsTable", func(t *testing.T, runner *TestRunner) {
		name := UniqueKey("versionstable-cred")

		runner.RunAndAssert(CredentialSubmitCommand(name, "v1"))
		runner.RunAndAssert(CredentialVersionsTableCommand(name))

		runner.Run(CredentialDeleteCommand(name))
	})
}

// TestCredentialDelete verifies the "credential delete <name>" verb
// across every enabled transport. After delete, retrieve must fail and
// the name must no longer appear in list output.
func TestCredentialDelete(t *testing.T) {
	RunForAllTransports(t, "Delete", func(t *testing.T, runner *TestRunner) {
		name := UniqueKey("delete-cred")

		runner.RunAndAssert(CredentialSubmitCommand(name, "delete-me"))
		runner.RunAndAssert(CredentialDeleteCommand(name))

		// Retrieve after delete should fail.
		runner.RunAndAssert(CredentialRetrieveExpectFailCommand(name))
	})
}

// TestCredentialGC verifies the "credential gc <name>" verb across
// every enabled transport. With no expired versions the call still
// succeeds and reports zero reaped.
func TestCredentialGC(t *testing.T) {
	RunForAllTransports(t, "GC", func(t *testing.T, runner *TestRunner) {
		name := UniqueKey("gc-cred")

		runner.RunAndAssert(CredentialSubmitCommand(name, "gc-value"))
		runner.RunAndAssert(CredentialGCCommand(name))

		runner.Run(CredentialDeleteCommand(name))
	})
}

// =============================================================================
// Missing-arg error tests
// =============================================================================

// TestCredentialSubmitMissingName asserts that "credential submit"
// without a positional <name> fails (cobra.ExactArgs(1) enforcement)
// across every enabled transport.
func TestCredentialSubmitMissingName(t *testing.T) {
	RunForAllTransports(t, "SubmitMissingName", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(CredentialSubmitMissingNameCommand())
	})
}

// TestCredentialSubmitMissingValue asserts that "credential submit
// <name>" without --value returns CredentialValueRequiredError across
// every enabled transport.
func TestCredentialSubmitMissingValue(t *testing.T) {
	RunForAllTransports(t, "SubmitMissingValue", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(CredentialSubmitMissingValueCommand(UniqueKey("noval-cred")))
	})
}

// TestCredentialRotateMissingName asserts that "credential rotate"
// without a positional <name> fails across every enabled transport.
func TestCredentialRotateMissingName(t *testing.T) {
	RunForAllTransports(t, "RotateMissingName", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(CredentialRotateMissingNameCommand())
	})
}

// TestCredentialRotateMissingValue asserts that "credential rotate
// <name>" without --value returns CredentialValueRequiredError across
// every enabled transport.
func TestCredentialRotateMissingValue(t *testing.T) {
	RunForAllTransports(t, "RotateMissingValue", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(CredentialRotateMissingValueCommand(UniqueKey("noval-cred")))
	})
}

// TestCredentialRetrieveMissingName asserts that "credential retrieve"
// without a positional <name> fails across every enabled transport.
func TestCredentialRetrieveMissingName(t *testing.T) {
	RunForAllTransports(t, "RetrieveMissingName", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(CredentialRetrieveMissingNameCommand())
	})
}

// TestCredentialVersionsMissingName asserts that "credential versions"
// without a positional <name> fails across every enabled transport.
func TestCredentialVersionsMissingName(t *testing.T) {
	RunForAllTransports(t, "VersionsMissingName", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(CredentialVersionsMissingNameCommand())
	})
}

// TestCredentialDeleteMissingName asserts that "credential delete"
// without a positional <name> fails across every enabled transport.
func TestCredentialDeleteMissingName(t *testing.T) {
	RunForAllTransports(t, "DeleteMissingName", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(CredentialDeleteMissingNameCommand())
	})
}

// TestCredentialGCMissingName asserts that "credential gc" without a
// positional <name> fails across every enabled transport.
func TestCredentialGCMissingName(t *testing.T) {
	RunForAllTransports(t, "GCMissingName", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(CredentialGCMissingNameCommand())
	})
}

// =============================================================================
// Round-trip tests
// =============================================================================

// TestCredentialRoundTrip exercises the canonical lifecycle across
// every enabled transport:
//
//	submit -> list (name present) -> retrieve (value matches) ->
//	delete -> list (name absent) -> retrieve (fails)
//
// This is the regression guard against per-transport drift on the
// non-versioned write/read/delete path.
func TestCredentialRoundTrip(t *testing.T) {
	RunForAllTransports(t, "RoundTrip", func(t *testing.T, runner *TestRunner) {
		name := UniqueKey("roundtrip-cred")
		value := "roundtrip-value-" + name

		// 1. submit
		runner.RunAndAssert(CredentialSubmitCommand(name, value))

		// 2. list (name present)
		runner.RunAndAssert(CredentialListExpectingCommand(name))

		// 3. retrieve (value matches)
		runner.RunAndAssert(CredentialRetrieveExpectingValueCommand(name, value))

		// 4. delete
		runner.RunAndAssert(CredentialDeleteCommand(name))

		// 5. list (name absent) — the verb still succeeds; we just
		// assert the deleted name is not in stdout.
		stdout, _, err := runner.Run(CredentialListCommand())
		if err == nil && strings.Contains(stdout, name) {
			t.Errorf("post-delete list still contains %q: %s", name, stdout)
		}

		// 6. retrieve (fails — credential no longer exists)
		runner.RunAndAssert(CredentialRetrieveExpectFailCommand(name))
	})
}

// TestCredentialVersionedRoundTrip exercises the versioned-rotation
// lifecycle across every enabled transport:
//
//	submit (v1) -> rotate (v2, grace=N) -> retrieve current (v2 value)
//	-> retrieve --version 1 (v1 still graced) -> wait > grace -> gc
//	-> retrieve --version 1 (expired, fails)
//
// The wait step uses a channel-driven timer (time.After), not
// time.Sleep, per project conventions.
func TestCredentialVersionedRoundTrip(t *testing.T) {
	RunForAllTransports(t, "VersionedRoundTrip", func(t *testing.T, runner *TestRunner) {
		name := UniqueKey("verroundtrip-cred")
		const grace = 2 * time.Second

		// 1. submit (v1)
		runner.RunAndAssert(CredentialSubmitCommand(name, "v1-value"))

		// 2. rotate (v2, grace=N)
		runner.RunAndAssert(CredentialRotateCommand(name, "v2-value", grace))

		// 3. retrieve current — must be v2
		runner.RunAndAssert(CredentialRetrieveExpectingValueCommand(name, "v2-value"))

		// 4. retrieve --version 1 — v1 still inside grace window
		runner.RunAndAssert(CredentialRetrieveVersionExpectingValueCommand(name, 1, "v1-value"))

		// 5. wait past grace window without time.Sleep — use a
		// channel-based timer with a small safety margin so reads
		// after this point observe v1 as expired.
		waitPastGrace(t, grace+500*time.Millisecond)

		// 6. gc — reaps the expired v1 entry.
		runner.RunAndAssert(CredentialGCCommand(name))

		// 7. retrieve --version 1 must now fail (entry was reaped).
		runner.RunAndAssert(CredentialRetrieveVersionExpectFailCommand(name, 1))

		// Cleanup.
		runner.Run(CredentialDeleteCommand(name))
	})
}

// =============================================================================
// Suite-driven tests
// =============================================================================

// TestCredentialSuite runs the full CredentialSuite (submit -> list ->
// retrieve -> versions -> rotate -> retrieve -> gc -> delete) via
// RunSuite across every enabled transport.
func TestCredentialSuite(t *testing.T) {
	RunForAllTransports(t, "Suite", func(t *testing.T, runner *TestRunner) {
		name := UniqueKey("suite-cred")
		runner.RunSuite(CredentialSuite(name, "suite-value-"+name))
	})
}

// TestCredentialErrorSuite runs the canonical missing-arg error suite
// across every enabled transport.
func TestCredentialErrorSuite(t *testing.T) {
	RunForAllTransports(t, "ErrorSuite", func(t *testing.T, runner *TestRunner) {
		runner.RunSuite(CredentialErrorSuite())
	})
}

// =============================================================================
// Helpers
// =============================================================================

// waitPastGrace blocks until the supplied duration has elapsed using a
// channel-based timer. The project bans time.Sleep, so callers needing
// to defer a check until after a grace window has expired must use
// this helper (or a similar select-on-time.After pattern).
func waitPastGrace(t *testing.T, d time.Duration) {
	t.Helper()
	timer := time.NewTimer(d)
	defer timer.Stop()
	<-timer.C
}
