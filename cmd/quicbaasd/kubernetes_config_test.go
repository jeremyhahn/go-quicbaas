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

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// kubernetesConfigMap is deployments/kubernetes/configmap.yaml, the node
// configuration every StatefulSet replica starts from.
const kubernetesConfigMap = "../../deployments/kubernetes/configmap.yaml"

// TestKubernetesReplicaLoadsAsRaftNode2 starts the daemon's configuration path
// the way the StatefulSet starts pod quicbaas-1: the ConfigMap's quicbaas.yaml
// plus the QUICBAAS_RAFT_* exports of the start command.
func TestKubernetesReplicaLoadsAsRaftNode2(t *testing.T) {
	raw, err := os.ReadFile(kubernetesConfigMap)
	require.NoError(t, err)
	var cm struct {
		Data map[string]string `yaml:"data"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &cm))
	nodeYAML := cm.Data["quicbaas.yaml"]
	require.NotEmpty(t, nodeYAML, "ConfigMap has no quicbaas.yaml")

	origCfg, origLog, origCfgFile := cfg, log, cfgFile
	t.Cleanup(func() {
		cfg, log, cfgFile = origCfg, origLog, origCfgFile
		raftAddress = ""
		viper.Reset()
	})
	for _, name := range raftEnv {
		t.Setenv(name, "")
		require.NoError(t, os.Unsetenv(name))
	}
	// The image sets QUICBAAS_SERVER_UNIX_SOCKET; the ConfigMap alone must
	// name a writable socket path.
	for _, name := range []string{
		"QUICBAAS_SERVER_UNIX_SOCKET", "QUICBAAS_SERVER_UNIX_ENABLED",
		"QUICBAAS_RAFT_TLS_CERT_FILE", "QUICBAAS_RAFT_TLS_KEY_FILE", "QUICBAAS_RAFT_TLS_CA_FILE",
		"QUICBAAS_IAM_SPIFFE_TRUST_DOMAIN",
	} {
		t.Setenv(name, "")
		require.NoError(t, os.Unsetenv(name))
	}

	const peer = ".quicbaas-raft.quicbaas.svc.cluster.local:63000"
	members := []string{"1=quicbaas-0" + peer, "2=quicbaas-1" + peer, "3=quicbaas-2" + peer}
	t.Setenv("QUICBAAS_RAFT_NODE_ID", "2")
	t.Setenv("QUICBAAS_RAFT_RAFT_ADDRESS", "quicbaas-1"+peer)
	t.Setenv("QUICBAAS_RAFT_LISTEN_HOST", "10.0.0.2")
	t.Setenv("QUICBAAS_RAFT_JOIN_ADDRESSES", strings.Join(members, ","))
	founders := []string{"1=spki:" + strings.Repeat("a1", 32), "3=spki:" + strings.Repeat("c3", 32)}
	t.Setenv("QUICBAAS_RAFT_MEMBER_IDENTITIES", strings.Join(founders, ","))
	// The pod's env carries the operator's trust domain from ConfigMap
	// quicbaas-identity.
	t.Setenv("QUICBAAS_IAM_SPIFFE_TRUST_DOMAIN", "quicbaas.example.test")

	cfgFile = filepath.Join(t.TempDir(), "quicbaas.yaml")
	require.NoError(t, os.WriteFile(cfgFile, []byte(nodeYAML), 0o600))
	viper.Reset()
	initConfig()
	require.NoError(t, applyRaftFlags(newTestCmdWithRaftFlags(), failingHostname))

	assert.Equal(t, "cluster", cfg.Server.Mode)
	assert.EqualValues(t, 2, cfg.Raft.NodeID)
	assert.NotZero(t, cfg.Raft.DeploymentID, "every node must share one deployment ID")
	assert.Equal(t, "quicbaas-1"+peer, cfg.Raft.RaftAddress)
	assert.Equal(t, "10.0.0.2", cfg.Raft.ListenHost)
	assert.ElementsMatch(t, members, cfg.Raft.JoinAddresses)
	// go-quicraft refuses to bootstrap unless every other founder is
	// identified; the daemon passes this list to go-qrdb unchanged.
	assert.Equal(t, founders, cfg.Raft.MemberIdentities)
	assert.True(t, cfg.Raft.MutualTLS)
	assert.Equal(t, "/etc/quicbaas/raft-tls/ca.crt", cfg.Raft.TLSCAFile)
	// The raft-key init container places the replica's own pair here.
	assert.Equal(t, "/etc/quicbaas/raft-node/tls.crt", cfg.Raft.TLSCertFile)
	assert.Equal(t, "/etc/quicbaas/raft-node/tls.key", cfg.Raft.TLSKeyFile)
	assert.Equal(t, "quicbaas.example.test", cfg.IAM.SPIFFETrustDomain, "first boot refuses an empty trust domain")
	assert.True(t, cfg.Seal.Enabled, "data must not rest unencrypted")
	// The node serves the Unix socket whenever a path is set, and the root
	// filesystem is read-only: the socket must be on the run emptyDir.
	assert.Equal(t, "/var/run/quicbaas/quicbaas.sock", cfg.Server.Unix.Socket)

	// The certificate files exist only in the pod, so only the Raft and server
	// sections are held to validation here.
	var verrs config.ValidationErrors
	if err := config.Validate(cfg); err != nil && !errors.As(err, &verrs) {
		t.Fatalf("validate: %v", err)
	}
	for _, e := range verrs {
		if strings.HasPrefix(e.Field, "raft") || strings.HasPrefix(e.Field, "server") {
			t.Errorf("validate %s: %s", e.Field, e.Message)
		}
	}
}

// TestKubernetesGuideStatesTheMemlockFloor pins the RLIMIT_MEMLOCK floor the
// Kubernetes guide states to the daemon's own numbers: the smallest bounded
// limit that starts is the headroom plus the minimum budget.
func TestKubernetesGuideStatesTheMemlockFloor(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "deployment", "kubernetes.md"))
	require.NoError(t, err)
	doc := string(raw)
	floor := fmt.Sprintf("%d MiB", (secureMemoryHeadroomBytes+minSecureMemoryBudgetBytes)>>20)
	assert.Contains(t, doc, "RLIMIT_MEMLOCK")
	assert.Contains(t, doc, floor)
}
