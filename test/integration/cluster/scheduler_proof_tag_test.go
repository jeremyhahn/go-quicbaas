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

package cluster

import (
	"go/build/constraint"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// schedulerProofFile holds the orchestrated scheduler leader-firing proof.
const schedulerProofFile = "scheduler_leader_test.go"

// TestSchedulerProofTag_ExcludedFromDefaultSuite guards the default
// integration run: the leader-stop proof fails unless the Makefile stops the
// leader between tests, so a build with only the integration tag must not
// compile it, and the proof target's tag set must.
func TestSchedulerProofTag_ExcludedFromDefaultSuite(t *testing.T) {
	expr := schedulerProofConstraint(t)

	defaultTags := map[string]bool{"integration": true, "rest": true, "grpc": true, "quic": true}
	assert.False(t, expr.Eval(func(tag string) bool { return defaultTags[tag] }),
		"%s must not build with the default integration tags", schedulerProofFile)

	proofTags := map[string]bool{"integration": true, "scheduler_proof": true}
	assert.True(t, expr.Eval(func(tag string) bool { return proofTags[tag] }),
		"%s must build with the scheduler_proof tag", schedulerProofFile)
}

// TestSchedulerProofTag_MissingConstraint shows the parser the guard relies
// on rejects a file header without a go:build line, so a dropped constraint
// cannot pass the guard silently.
func TestSchedulerProofTag_MissingConstraint(t *testing.T) {
	_, ok := goBuildConstraint("package cluster\n")
	assert.False(t, ok)
}

// schedulerProofConstraint returns the go:build expression of the proof file.
func schedulerProofConstraint(t *testing.T) constraint.Expr {
	t.Helper()
	src, err := os.ReadFile(schedulerProofFile)
	require.NoError(t, err, "read %s", schedulerProofFile)
	expr, ok := goBuildConstraint(string(src))
	require.True(t, ok, "%s has no go:build constraint", schedulerProofFile)
	return expr
}

// goBuildConstraint returns the first go:build expression in src.
func goBuildConstraint(src string) (constraint.Expr, bool) {
	for _, line := range strings.Split(src, "\n") {
		if !constraint.IsGoBuild(line) {
			continue
		}
		expr, err := constraint.Parse(line)
		if err != nil {
			return nil, false
		}
		return expr, true
	}
	return nil, false
}
