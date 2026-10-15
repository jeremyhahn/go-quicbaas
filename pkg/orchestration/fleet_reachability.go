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
	"strconv"
	"strings"
	"time"
)

// FleetNodeReachability is what the control plane last learned about whether
// it can dial a fleet node at its registered address.
type FleetNodeReachability string

const (
	// FleetNodeReachable means the control plane resolved and dialed the
	// node's address when it registered.
	FleetNodeReachable FleetNodeReachability = "reachable"

	// FleetNodeReachabilityUnverified means the address was registered
	// without a probe: a control plane with no prober wired, or a record
	// re-published from a heartbeat.
	FleetNodeReachabilityUnverified FleetNodeReachability = "unverified"
)

// DefaultAgentAddressProbeTimeout bounds one reachability probe: the
// resolution and the dial together. Registration waits on it, so it is short.
const DefaultAgentAddressProbeTimeout = 5 * time.Second

// ErrAgentAddressUnreachable is the sentinel every AgentAddressUnreachableError
// wraps.
var ErrAgentAddressUnreachable = errors.New("fleet node: address is unreachable from the control plane")

// errReservedInvalidName is the cause for a name under the RFC 6761 ".invalid"
// domain, which no resolver may answer.
var errReservedInvalidName = errors.New("the .invalid domain never resolves (RFC 6761)")

// errEmptyAgentHost is the cause for an address with no host to dial.
var errEmptyAgentHost = errors.New("the host is empty")

// errUnspecifiedAgentHost is the cause for a wildcard host, which names no
// machine the control plane could dial.
var errUnspecifiedAgentHost = errors.New("a wildcard host names no machine to dial")

// AgentAddressUnreachableError reports a fleet node address the control plane
// could not resolve or dial. Registration refuses it: a node the control plane
// cannot reach would be placed work it can never be sent.
type AgentAddressUnreachableError struct {
	// Address is the host:port that was probed.
	Address string

	// Op is the step that failed: "parse", "resolve" or "dial".
	Op string

	// Cause is why the step failed.
	Cause error
}

// Error implements the error interface.
func (e *AgentAddressUnreachableError) Error() string {
	return "fleet node address " + strconv.Quote(e.Address) + " is unreachable from the control plane (" +
		e.Op + "): " + e.Cause.Error()
}

// Unwrap returns ErrAgentAddressUnreachable and the cause.
func (e *AgentAddressUnreachableError) Unwrap() []error {
	return []error{ErrAgentAddressUnreachable, e.Cause}
}

// ErrAgentAddressLoopbackInCluster is the sentinel every
// AgentAddressLoopbackInClusterError wraps.
var ErrAgentAddressLoopbackInCluster = errors.New("fleet node: a loopback address is unreachable from the other nodes of a clustered control plane")

// AgentAddressLoopbackInClusterError reports a fleet node address whose host is
// loopback, registered with a clustered control plane. The node that hosts the
// agent dials it, so the probe passes there, but no other node of the cluster
// can: every call routed to the agent from another node would fail.
type AgentAddressLoopbackInClusterError struct {
	// Address is the host:port that was refused.
	Address string
}

func (e *AgentAddressLoopbackInClusterError) Error() string {
	return "fleet node address " + strconv.Quote(e.Address) + " is loopback, which the other nodes of a clustered" +
		" control plane cannot dial: set the agent's --advertise (advertise_addr, QUICBAAS_ADVERTISE_ADDR)" +
		" to a host every node can dial"
}

// Unwrap returns ErrAgentAddressLoopbackInCluster and ErrAgentAddressUnreachable:
// the address is unreachable from the cluster, though not from this node.
func (e *AgentAddressLoopbackInClusterError) Unwrap() []error {
	return []error{ErrAgentAddressLoopbackInCluster, ErrAgentAddressUnreachable}
}

// RefuseLoopbackInCluster returns an AgentAddressLoopbackInClusterError when
// address names a loopback host: an IP loopback literal, localhost, or a name
// under the RFC 6761 ".localhost" domain. A clustered control plane calls it
// before the probe; a single-node one does not, as its only dialer is local.
// Any other address, including one that does not parse, is left to the probe.
func RefuseLoopbackInCluster(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err == nil && isLoopbackHost(host) {
		return &AgentAddressLoopbackInClusterError{Address: address}
	}
	return nil
}

// isLoopbackHost reports whether host names this machine's loopback interface.
func isLoopbackHost(host string) bool {
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	name := strings.ToLower(strings.TrimSuffix(host, "."))
	return name == "localhost" || strings.HasSuffix(name, ".localhost")
}

// HostResolver resolves a host name to addresses. *net.Resolver satisfies it.
type HostResolver interface {
	LookupHost(ctx context.Context, host string) ([]string, error)
}

// ContextDialer opens a connection. *net.Dialer satisfies it.
type ContextDialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

// AgentAddressProber checks that the control plane can resolve and dial a
// fleet node's address. It opens a TCP connection and closes it straight
// away; it does not speak the FleetRuntime protocol.
type AgentAddressProber struct {
	resolver HostResolver
	dialer   ContextDialer
	timeout  time.Duration
}

// NewAgentAddressProber returns a prober over resolver and dialer. A nil
// resolver or dialer takes the system one; a non-positive timeout takes
// DefaultAgentAddressProbeTimeout.
func NewAgentAddressProber(resolver HostResolver, dialer ContextDialer, timeout time.Duration) *AgentAddressProber {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	if dialer == nil {
		dialer = &net.Dialer{}
	}
	if timeout <= 0 {
		timeout = DefaultAgentAddressProbeTimeout
	}
	return &AgentAddressProber{resolver: resolver, dialer: dialer, timeout: timeout}
}

// Probe resolves address's host and dials each address it resolves to, in
// the resolver's order, until one answers — a node listening on IPv4 alone
// is reachable even when its name resolves to an IPv6 address first. Every
// attempt shares the one probe timeout. Any failure is an
// *AgentAddressUnreachableError naming the step; a dial failure carries the
// cause of every address tried.
func (p *AgentAddressProber) Probe(ctx context.Context, address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return &AgentAddressUnreachableError{Address: address, Op: "parse", Cause: err}
	}
	if host == "" {
		return &AgentAddressUnreachableError{Address: address, Op: "parse", Cause: errEmptyAgentHost}
	}
	// A wildcard host dials the control plane's own machine, so a probe of
	// one would "succeed" for the wrong host.
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		return &AgentAddressUnreachableError{Address: address, Op: "parse", Cause: errUnspecifiedAgentHost}
	}

	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	targets := []string{host}
	if net.ParseIP(host) == nil {
		resolved, err := p.resolve(ctx, host)
		if err != nil {
			return &AgentAddressUnreachableError{Address: address, Op: "resolve", Cause: err}
		}
		targets = resolved
	}

	var causes []error
	for _, target := range targets {
		err := p.dial(ctx, net.JoinHostPort(target, port))
		if err == nil {
			return nil
		}
		causes = append(causes, err)
		if ctx.Err() != nil {
			break
		}
	}
	return &AgentAddressUnreachableError{Address: address, Op: "dial", Cause: errors.Join(causes...)}
}

// dial opens a connection to target and closes it straight away.
func (p *AgentAddressProber) dial(ctx context.Context, target string) error {
	conn, err := p.dialer.DialContext(ctx, "tcp", target)
	if err != nil {
		return err
	}
	return conn.Close()
}

// resolve returns every address host resolves to. A name under ".invalid"
// is refused without a query, as RFC 6761 §6.4 asks of resolution APIs.
func (p *AgentAddressProber) resolve(ctx context.Context, host string) ([]string, error) {
	name := strings.ToLower(strings.TrimSuffix(host, "."))
	if name == "invalid" || strings.HasSuffix(name, ".invalid") {
		return nil, errReservedInvalidName
	}
	addrs, err := p.resolver.LookupHost(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, &net.DNSError{Err: "no addresses", Name: host, IsNotFound: true}
	}
	return addrs, nil
}
