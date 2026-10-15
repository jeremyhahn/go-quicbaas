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

package orchestration

import (
	"context"
	"errors"
	"net"
	"strings"
	"syscall"
	"testing"
)

// mapResolver answers from a fixed table and fails every other name, so no
// test depends on the host's DNS.
type mapResolver struct {
	hosts  map[string][]string
	lookup []string
}

func (m *mapResolver) LookupHost(_ context.Context, host string) ([]string, error) {
	m.lookup = append(m.lookup, host)
	if addrs, ok := m.hosts[host]; ok {
		return addrs, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

// loopbackListener is a listening socket on 127.0.0.1, closed with the test.
func loopbackListener(t *testing.T) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l
}

// requireUnreachable asserts err is an AgentAddressUnreachableError for
// address that failed at op.
func requireUnreachable(t *testing.T, err error, address, op string) {
	t.Helper()
	var unreachable *AgentAddressUnreachableError
	if !errors.As(err, &unreachable) || !errors.Is(err, ErrAgentAddressUnreachable) {
		t.Fatalf("err = %v, want an AgentAddressUnreachableError", err)
	}
	if unreachable.Address != address || unreachable.Op != op {
		t.Fatalf("unreachable %q at %q, want %q at %q", unreachable.Address, unreachable.Op, address, op)
	}
}

// TestAgentAddressProber_ReachesAListeningAddress: an IP literal is dialed
// without a lookup, and a name is dialed at the address it resolves to.
func TestAgentAddressProber_ReachesAListeningAddress(t *testing.T) {
	t.Parallel()
	l := loopbackListener(t)
	_, port, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	resolver := &mapResolver{hosts: map[string][]string{"agent-09.example.test": {"127.0.0.1"}}}
	prober := NewAgentAddressProber(resolver, nil, 0)

	if err := prober.Probe(context.Background(), l.Addr().String()); err != nil {
		t.Fatalf("loopback literal: %v", err)
	}
	if err := prober.Probe(context.Background(), net.JoinHostPort("agent-09.example.test", port)); err != nil {
		t.Fatalf("resolved name: %v", err)
	}
	if len(resolver.lookup) != 1 {
		t.Fatalf("lookups %v, want only the name", resolver.lookup)
	}
}

// refusingDialer refuses every address in refused and dials the rest for
// real, recording each address it was asked for.
type refusingDialer struct {
	refused map[string]bool
	dialed  []string
}

func (d *refusingDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	d.dialed = append(d.dialed, address)
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if d.refused[host] {
		return nil, &net.OpError{Op: "dial", Net: network, Err: syscall.ECONNREFUSED}
	}
	var real net.Dialer
	return real.DialContext(ctx, network, address)
}

// TestAgentAddressProber_TriesEveryResolvedAddress: a name whose first
// address refuses is reachable on a later one, and a name none of whose
// addresses answers is refused at the dial step with every address tried.
func TestAgentAddressProber_TriesEveryResolvedAddress(t *testing.T) {
	t.Parallel()
	l := loopbackListener(t)
	_, port, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	resolver := &mapResolver{hosts: map[string][]string{
		"dual.example.test": {"2001:db8::9", "127.0.0.1"},
		"dead.example.test": {"2001:db8::9", "192.0.2.9"},
	}}
	dialer := &refusingDialer{refused: map[string]bool{"2001:db8::9": true, "192.0.2.9": true}}
	prober := NewAgentAddressProber(resolver, dialer, 0)

	if err := prober.Probe(context.Background(), net.JoinHostPort("dual.example.test", port)); err != nil {
		t.Fatalf("reachable on its second address: %v", err)
	}
	if len(dialer.dialed) != 2 {
		t.Fatalf("dialed %v, want the refused address and then the reachable one", dialer.dialed)
	}

	dialer.dialed = nil
	dead := net.JoinHostPort("dead.example.test", port)
	err = prober.Probe(context.Background(), dead)
	requireUnreachable(t, err, dead, "dial")
	if !errors.Is(err, syscall.ECONNREFUSED) {
		t.Fatalf("err = %v, want the dial causes", err)
	}
	if len(dialer.dialed) != 2 {
		t.Fatalf("dialed %v, want every resolved address", dialer.dialed)
	}
}

// TestAgentAddressProber_RefusesWhatItCannotReach: a ".invalid" name is
// refused without asking any resolver, a name nothing resolves and a port
// nothing listens on are refused at their step, and so are a wildcard host
// and an address with no port.
func TestAgentAddressProber_RefusesWhatItCannotReach(t *testing.T) {
	t.Parallel()
	closed := loopbackListener(t)
	closedAddr := closed.Addr().String()
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	resolver := &mapResolver{}
	prober := NewAgentAddressProber(resolver, nil, 0)
	ctx := context.Background()

	requireUnreachable(t, prober.Probe(ctx, "agent-09.invalid:9091"), "agent-09.invalid:9091", "resolve")
	requireUnreachable(t, prober.Probe(ctx, "AGENT.INVALID.:9091"), "AGENT.INVALID.:9091", "resolve")
	if len(resolver.lookup) != 0 {
		t.Fatalf("a .invalid name was looked up: %v", resolver.lookup)
	}
	requireUnreachable(t, prober.Probe(ctx, "agent-77.example.test:9091"), "agent-77.example.test:9091", "resolve")
	requireUnreachable(t, prober.Probe(ctx, closedAddr), closedAddr, "dial")
	requireUnreachable(t, prober.Probe(ctx, "0.0.0.0:9091"), "0.0.0.0:9091", "parse")
	requireUnreachable(t, prober.Probe(ctx, ":9091"), ":9091", "parse")
	requireUnreachable(t, prober.Probe(ctx, "agent-09"), "agent-09", "parse")
}

// TestRefuseLoopbackInCluster: a loopback host passes the probe on the node
// that hosts the agent, yet no other node of a cluster can dial it, so a
// clustered control plane refuses it before probing.
func TestRefuseLoopbackInCluster(t *testing.T) {
	t.Parallel()
	for _, address := range []string{
		"127.0.0.1:9091", "127.8.0.1:9091", "[::1]:9091",
		"localhost:9091", "LocalHost.:9091", "agent01.localhost:9091",
	} {
		err := RefuseLoopbackInCluster(address)
		var loopback *AgentAddressLoopbackInClusterError
		if !errors.As(err, &loopback) || !errors.Is(err, ErrAgentAddressLoopbackInCluster) {
			t.Fatalf("%s: err = %v, want an AgentAddressLoopbackInClusterError", address, err)
		}
		if loopback.Address != address {
			t.Fatalf("%s: error names %q", address, loopback.Address)
		}
		for _, fix := range []string{"--advertise", "advertise_addr", "QUICBAAS_ADVERTISE_ADDR"} {
			if !strings.Contains(err.Error(), fix) {
				t.Fatalf("%s: error %q does not name %s", address, err, fix)
			}
		}
	}
}

// TestRefuseLoopbackInCluster_LeavesOtherAddressesToTheProbe: a routable
// host, a name and an address that does not parse are not this check's to
// refuse; the probe decides them.
func TestRefuseLoopbackInCluster_LeavesOtherAddressesToTheProbe(t *testing.T) {
	t.Parallel()
	for _, address := range []string{
		"192.0.2.10:9091", "[2001:db8::9]:9091", "agent01.example.test:9091",
		"notlocalhost:9091", "0.0.0.0:9091", "agent-09",
	} {
		if err := RefuseLoopbackInCluster(address); err != nil {
			t.Fatalf("%s: err = %v, want nil", address, err)
		}
	}
}
