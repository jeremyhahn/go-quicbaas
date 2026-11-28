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
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// deploymentProofFile holds the deployment proof tests, which only
// `make integration-test-cluster-failover` may run.
const deploymentProofFile = "deployment_proof_test.go"

// TestDeploymentProofTag_ExcludedFromDefaultSuite keeps the deployment proof
// out of every runner that compiles the default integration tags: run there,
// against a cluster nobody stops, it would fail or prove nothing.
func TestDeploymentProofTag_ExcludedFromDefaultSuite(t *testing.T) {
	src, err := os.ReadFile(deploymentProofFile)
	require.NoError(t, err, "read %s", deploymentProofFile)
	expr, ok := goBuildConstraint(string(src))
	require.True(t, ok, "%s has no go:build constraint", deploymentProofFile)

	defaultTags := map[string]bool{"integration": true, "rest": true, "grpc": true, "quic": true}
	assert.False(t, expr.Eval(func(tag string) bool { return defaultTags[tag] }),
		"%s must not build with the default integration tags", deploymentProofFile)

	proofTags := map[string]bool{"integration": true, "deployment_proof": true}
	assert.True(t, expr.Eval(func(tag string) bool { return proofTags[tag] }),
		"%s must build with the deployment_proof tag", deploymentProofFile)
}
