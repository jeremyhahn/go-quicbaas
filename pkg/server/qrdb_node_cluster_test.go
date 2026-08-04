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

//go:build raft

package server

import (
	"path/filepath"
	"testing"

	clusterenroll "github.com/jeremyhahn/go-qrdb/sdk/go/clusterenroll"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// TestBuildClusterEnrollOptions_Defaults verifies that an enrollment config with
// no overrides maps onto clusterenroll.Options with the documented defaults
// ("quicbaas.cluster" trust domain, the default Noise port) and that membership
// of more than one node yields SingleNode=false.
func TestBuildClusterEnrollOptions_Defaults(t *testing.T) {
	dir := t.TempDir()
	s := &Server{config: &config.Config{
		Storage: config.StorageConfig{DataDir: dir},
		Raft: config.RaftConfig{
			RaftAddress:   "10.0.0.1:63001",
			JoinAddresses: []string{"1=10.0.0.1:63001", "2=10.0.0.2:63001"},
		},
	}}
	members := map[uint64]string{1: "10.0.0.1:63001", 2: "10.0.0.2:63001"}

	opts := s.buildClusterEnrollOptions(members)

	if opts.TrustDomain != defaultRaftTrustDomain {
		t.Errorf("TrustDomain = %q, want %q", opts.TrustDomain, defaultRaftTrustDomain)
	}
	if opts.Advertise.NoisePort != clusterenroll.DefaultNoisePort {
		t.Errorf("Advertise.NoisePort = %d, want %d", opts.Advertise.NoisePort, clusterenroll.DefaultNoisePort)
	}
	if opts.RaftAddress != "10.0.0.1:63001" {
		t.Errorf("RaftAddress = %q, want 10.0.0.1:63001", opts.RaftAddress)
	}
	wantDataDir := filepath.Join(dir, raftTLSCacheSubdir)
	if opts.DataDir != wantDataDir {
		t.Errorf("DataDir = %q, want %q", opts.DataDir, wantDataDir)
	}
	if opts.SingleNode {
		t.Error("SingleNode = true, want false for a 2-member cluster")
	}
	if opts.Bootstrap.DiscoveryMethod != "static" {
		t.Errorf("Bootstrap.DiscoveryMethod = %q, want static", opts.Bootstrap.DiscoveryMethod)
	}
	if len(opts.Bootstrap.Peers) != 2 {
		t.Errorf("Bootstrap.Peers = %v, want 2 entries", opts.Bootstrap.Peers)
	}
}

// TestBuildClusterEnrollOptions_ExplicitAndSingleNode verifies that explicit
// enrollment values (trust domain, advertise port, join bootstrap source) pass
// through unchanged, and that a single-member cluster yields SingleNode=true.
func TestBuildClusterEnrollOptions_ExplicitAndSingleNode(t *testing.T) {
	dir := t.TempDir()
	s := &Server{config: &config.Config{
		Storage: config.StorageConfig{DataDir: dir},
		Raft: config.RaftConfig{
			RaftAddress: "10.0.0.1:63001",
			RaftEnrollment: config.RaftEnrollmentConfig{
				TrustDomain:        "corp.cluster",
				AdvertiseNoisePort: 7777,
				Bootstrap: config.RaftBootstrapConfig{
					NoiseKey:  "deadbeef",
					NoiseAddr: "10.0.0.2:63006",
				},
			},
		},
	}}
	members := map[uint64]string{1: "10.0.0.1:63001"}

	opts := s.buildClusterEnrollOptions(members)

	if opts.TrustDomain != "corp.cluster" {
		t.Errorf("TrustDomain = %q, want corp.cluster", opts.TrustDomain)
	}
	if opts.Advertise.NoisePort != 7777 {
		t.Errorf("Advertise.NoisePort = %d, want 7777", opts.Advertise.NoisePort)
	}
	if !opts.SingleNode {
		t.Error("SingleNode = false, want true for a 1-member cluster")
	}
	if opts.Bootstrap.NoiseKey != "deadbeef" {
		t.Errorf("Bootstrap.NoiseKey = %q, want deadbeef", opts.Bootstrap.NoiseKey)
	}
	if opts.Bootstrap.NoiseAddr != "10.0.0.2:63006" {
		t.Errorf("Bootstrap.NoiseAddr = %q, want 10.0.0.2:63006", opts.Bootstrap.NoiseAddr)
	}
}

// TestBuildClusterEnrollOptions_DataDirFallback verifies that when Storage.DataDir
// is empty, standaloneQRDBDataDir falls back to Seal.BarrierDir (minus the
// filename component) so the raft-tls subdir is anchored to the barrier directory.
func TestBuildClusterEnrollOptions_DataDirFallback(t *testing.T) {
	dir := t.TempDir()
	barrierDir := filepath.Join(dir, "seal", "barrier.db")
	s := &Server{config: &config.Config{
		// Storage.DataDir intentionally empty
		Seal: config.SealConfig{BarrierDir: barrierDir},
		Raft: config.RaftConfig{
			RaftAddress: "10.0.0.1:63001",
		},
	}}
	members := map[uint64]string{1: "10.0.0.1:63001"}

	opts := s.buildClusterEnrollOptions(members)

	// standaloneQRDBDataDir strips the filename from BarrierDir.
	wantBase := filepath.Dir(barrierDir)
	wantDataDir := filepath.Join(wantBase, raftTLSCacheSubdir)
	if opts.DataDir != wantDataDir {
		t.Errorf("DataDir = %q, want %q", opts.DataDir, wantDataDir)
	}
}

// TestRaftJoinPeers verifies the peer filter: only "id=addr" entries are kept;
// bare "host:port" addresses without a replica-ID prefix are dropped.
func TestRaftJoinPeers(t *testing.T) {
	cases := []struct {
		name  string
		addrs []string
		want  []string
	}{
		{"nil input", nil, nil},
		{"empty slice", []string{}, nil},
		{"only id-prefixed", []string{"1=10.0.0.1:63001", "2=10.0.0.2:63001"}, []string{"1=10.0.0.1:63001", "2=10.0.0.2:63001"}},
		{"only bare addrs", []string{"10.0.0.1:63001", "10.0.0.2:63001"}, nil},
		{"mixed", []string{"1=10.0.0.1:63001", "10.0.0.2:63001"}, []string{"1=10.0.0.1:63001"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := raftJoinPeers(c.addrs)
			if len(got) != len(c.want) {
				t.Fatalf("raftJoinPeers(%v) = %v, want %v", c.addrs, got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("raftJoinPeers[%d] = %q, want %q", i, got[i], c.want[i])
				}
			}
		})
	}
}

// TestRaftTLSFilesConfigured verifies that all three file paths must be set for
// the function to return true, and that partial sets return false.
func TestRaftTLSFilesConfigured(t *testing.T) {
	cases := []struct {
		name string
		cfg  *config.Config
		want bool
	}{
		{
			"all set",
			&config.Config{Raft: config.RaftConfig{TLSCAFile: "ca.pem", TLSCertFile: "cert.pem", TLSKeyFile: "key.pem"}},
			true,
		},
		{
			"none set",
			&config.Config{},
			false,
		},
		{
			"only CA set",
			&config.Config{Raft: config.RaftConfig{TLSCAFile: "ca.pem"}},
			false,
		},
		{
			"CA and cert only",
			&config.Config{Raft: config.RaftConfig{TLSCAFile: "ca.pem", TLSCertFile: "cert.pem"}},
			false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := raftTLSFilesConfigured(c.cfg); got != c.want {
				t.Errorf("raftTLSFilesConfigured() = %v, want %v", got, c.want)
			}
		})
	}
}
