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

package server

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

func hostsFor(t *testing.T, hostname string, agents ...*orchestration.FleetNode) *localFleetNodeHosts {
	t.Helper()
	registry := orchestration.NewInMemoryFleetRegistry()
	for _, a := range agents {
		if err := registry.RegisterFleetNode(context.Background(), a); err != nil {
			t.Fatalf("RegisterFleetNode: %v", err)
		}
	}
	h := newLocalFleetNodeHosts(registry)
	h.hostname = func() (string, error) { return hostname, nil }
	return h
}

func agentReg(id, name, host string, kind orchestration.FleetNodeKind) *orchestration.FleetNode {
	return &orchestration.FleetNode{ID: id, TenantID: "acme", Name: name, Host: host, Backend: "claude",
		Kind: kind, Status: orchestration.FleetNodeStatusIdle}
}

func TestLocalAgentHosts_ResolvesTheAgentRegisteredFromThisNode(t *testing.T) {
	cases := map[string]struct {
		agents []*orchestration.FleetNode
		want   string
	}{
		"a daemon registered with a port": {
			agents: []*orchestration.FleetNode{
				agentReg("a1", "agent-09", "agent-09:9443", orchestration.FleetNodeKindDaemon),
				agentReg("a2", "agent-04-agent", "agent-04:9443", orchestration.FleetNodeKindDaemon),
			},
			want: "agent-04-agent",
		},
		"the embedded agent wins": {
			agents: []*orchestration.FleetNode{
				agentReg("a1", "agent-04", "agent-04", orchestration.FleetNodeKindDaemon),
				agentReg("a2", "embedded-agent-04", "agent-04", orchestration.FleetNodeKindEmbedded),
			},
			want: "embedded-agent-04",
		},
		// The fleet registry is shared: every node lists every node's
		// embedded agent, and only the one carrying this node's id is local.
		"this node's embedded agent among several": {
			agents: []*orchestration.FleetNode{
				agentReg("embedded-agent-09", "embedded-agent-09", "agent-09", orchestration.FleetNodeKindEmbedded),
				agentReg("embedded-sigma", "embedded-sigma", "sigma", orchestration.FleetNodeKindEmbedded),
				agentReg("embedded-agent-04", "embedded-agent-04", "agent-04", orchestration.FleetNodeKindEmbedded),
			},
			want: "embedded-agent-04",
		},
		"another node's embedded agent never beats a local daemon": {
			agents: []*orchestration.FleetNode{
				agentReg("embedded-agent-09", "embedded-agent-09", "agent-09", orchestration.FleetNodeKindEmbedded),
				agentReg("d1", "agent-04-agent", "agent-04:9443", orchestration.FleetNodeKindDaemon),
			},
			want: "agent-04-agent",
		},
		"another node's embedded agent with this node's host affinity": {
			agents: []*orchestration.FleetNode{
				agentReg("embedded-agent-09", "embedded-agent-09", "agent-04", orchestration.FleetNodeKindEmbedded),
				agentReg("d1", "agent-04-agent", "agent-04", orchestration.FleetNodeKindDaemon),
			},
			want: "agent-04-agent",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := hostsFor(t, "agent-04", tc.agents...).LocalFleetNodeHost(context.Background(), "acme", "claude")
			if err != nil || got != tc.want {
				t.Fatalf("LocalFleetNodeHost = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestLocalAgentHosts_RefusesWhenNoAgentIsLocal(t *testing.T) {
	h := hostsFor(t, "agent-04", agentReg("a1", "agent-09", "agent-09:9443", orchestration.FleetNodeKindDaemon))
	_, err := h.LocalFleetNodeHost(context.Background(), "acme", "claude")
	var notFound *LocalAgentNotFoundError
	if !errors.As(err, &notFound) || !strings.Contains(err.Error(), "agent-09@agent-09:9443") ||
		!strings.Contains(err.Error(), "smoke_host") {
		t.Fatalf("err = %v", err)
	}

	onlyRemoteEmbedded := hostsFor(t, "agent-04",
		agentReg("embedded-agent-09", "embedded-agent-09", "agent-09", orchestration.FleetNodeKindEmbedded),
		agentReg("embedded-sigma", "embedded-sigma", "sigma", orchestration.FleetNodeKindEmbedded))
	if got, err := onlyRemoteEmbedded.LocalFleetNodeHost(context.Background(), "acme", "claude"); !errors.As(err, &notFound) {
		t.Fatalf("only other nodes' embedded agents: got %q, err %v; want LocalAgentNotFoundError", got, err)
	}

	if _, err := newLocalFleetNodeHosts(nil).LocalFleetNodeHost(context.Background(), "acme", "claude"); !errors.As(err, &notFound) {
		t.Fatalf("a node without a registry: %v", err)
	}
	broken := hostsFor(t, "x")
	broken.hostname = func() (string, error) { return "", errors.New("uts") }
	if _, err := broken.LocalFleetNodeHost(context.Background(), "acme", "claude"); !errors.As(err, &notFound) {
		t.Fatalf("an unreadable hostname: %v", err)
	}
}

func TestRemoteFleetNodes_ListsEveryNodeThatIsNotThisOne(t *testing.T) {
	disabled := agentReg("d9", "build-host", "build-host:9443", orchestration.FleetNodeKindDaemon)
	disabled.DisabledAt = 1
	h := hostsFor(t, "agent-04",
		agentReg("embedded-agent-04", "embedded-agent-04", "agent-04", orchestration.FleetNodeKindEmbedded),
		agentReg("d1", "agent-04-agent", "agent-04:9443", orchestration.FleetNodeKindDaemon),
		agentReg("embedded-agent-09", "embedded-agent-09", "agent-04", orchestration.FleetNodeKindEmbedded),
		agentReg("d2", "worker-host-2", "worker-host-2:9443", orchestration.FleetNodeKindDaemon),
		disabled,
	)

	got, err := h.RemoteFleetNodes(context.Background(), "acme")
	if err != nil {
		t.Fatalf("RemoteFleetNodes: %v", err)
	}
	want := []string{"build-host@build-host:9443", "embedded-agent-09@agent-04", "worker-host-2@worker-host-2:9443"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("RemoteFleetNodes = %v, want %v (another node's embedded agent and a disabled node count)", got, want)
	}
}

func TestRemoteFleetNodes_ASingleHostFleetHasNone(t *testing.T) {
	h := hostsFor(t, "agent-04",
		agentReg("embedded-agent-04", "embedded-agent-04", "agent-04", orchestration.FleetNodeKindEmbedded),
		agentReg("d1", "agent-04-agent", "agent-04:9443", orchestration.FleetNodeKindDaemon))
	if got, err := h.RemoteFleetNodes(context.Background(), "acme"); err != nil || len(got) != 0 {
		t.Fatalf("RemoteFleetNodes = %v, %v; want none", got, err)
	}
	if got, err := newLocalFleetNodeHosts(nil).RemoteFleetNodes(context.Background(), "acme"); err != nil || len(got) != 0 {
		t.Fatalf("no registry: %v, %v; want none", got, err)
	}
}

func TestRemoteFleetNodes_RefusesWhenThisNodeCannotNameItself(t *testing.T) {
	h := hostsFor(t, "x")
	h.hostname = func() (string, error) { return "", errors.New("uts") }
	var topology *FleetTopologyError
	if _, err := h.RemoteFleetNodes(context.Background(), "acme"); !errors.As(err, &topology) {
		t.Fatalf("err = %v, want FleetTopologyError", err)
	}
}

func TestHostOnly(t *testing.T) {
	for in, want := range map[string]string{"h:9443": "h", "h": "h", "[::1]:80": "::1"} {
		if got := hostOnly(in); got != want {
			t.Errorf("hostOnly(%q) = %q, want %q", in, got, want)
		}
	}
}
