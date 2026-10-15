//go:build rest

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

package rest

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// enrollAdvertising posts an enrollment that advertises address.
func enrollAdvertising(t *testing.T, srv *Server, address string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"tenant_id":"acme","bind_addr":"0.0.0.0:9091","hostname":"agent01","advertise_addr":"` + address + `"}`
	w := httptest.NewRecorder()
	srv.HandleFleetEnroll(w, agentRegisterReq(t, body, agentRegisterTestPeerCert(t, "acme", "agent01")))
	return w
}

// TestHandleFleetEnroll_RefusesAnUnreachableAdvertisedAddress (go-quicbaas#795):
// an advertised address the control plane cannot resolve is refused with a
// 422 naming it, and nothing is registered.
func TestHandleFleetEnroll_RefusesAnUnreachableAdvertisedAddress(t *testing.T) {
	srv := newFleetEnrollTestServer(t, "")
	reg := &fakeFleetRegistrar{}
	srv.SetFleetRegistrar(reg)
	srv.SetFleetAddressProber(orchestration.NewAgentAddressProber(nil, nil, 0))

	w := enrollAdvertising(t, srv, "agent01.invalid:9091")

	require.Equal(t, http.StatusUnprocessableEntity, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "agent_address_unreachable")
	assert.Contains(t, w.Body.String(), "agent01.invalid:9091")
	assert.False(t, reg.called, "an unreachable node must not be registered")
}

// TestHandleFleetEnroll_RecordsAReachableAdvertisedAddress: an advertised
// address that dials is registered as the node's host, verbatim, and marked
// reachable.
func TestHandleFleetEnroll_RecordsAReachableAdvertisedAddress(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })

	srv := newFleetEnrollTestServer(t, "")
	reg := &fakeFleetRegistrar{}
	srv.SetFleetRegistrar(reg)
	srv.SetFleetAddressProber(orchestration.NewAgentAddressProber(nil, nil, 0))

	w := enrollAdvertising(t, srv, l.Addr().String())

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.True(t, reg.called)
	assert.Equal(t, l.Addr().String(), reg.req.Host)
	assert.Equal(t, l.Addr().String(), reg.req.AdvertisedAddress)
	assert.Equal(t, string(orchestration.FleetNodeReachable), reg.req.Reachability)
}

// firstAddressRefused resolves every name to an address that refuses and
// then to loopback, the way a dual-stack name reaches an IPv4-only listener.
type firstAddressRefused struct{}

const refusedProbeAddress = "2001:db8::9"

func (firstAddressRefused) LookupHost(context.Context, string) ([]string, error) {
	return []string{refusedProbeAddress, "127.0.0.1"}, nil
}

func (firstAddressRefused) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if host == refusedProbeAddress {
		return nil, &net.OpError{Op: "dial", Net: network, Err: syscall.ECONNREFUSED}
	}
	var dialer net.Dialer
	return dialer.DialContext(ctx, network, address)
}

// TestHandleFleetEnroll_ReachableOnALaterResolvedAddress: a name whose first
// resolved address refuses is enrolled when a later one answers.
func TestHandleFleetEnroll_ReachableOnALaterResolvedAddress(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	_, port, err := net.SplitHostPort(l.Addr().String())
	require.NoError(t, err)

	srv := newFleetEnrollTestServer(t, "")
	reg := &fakeFleetRegistrar{}
	srv.SetFleetRegistrar(reg)
	probe := firstAddressRefused{}
	srv.SetFleetAddressProber(orchestration.NewAgentAddressProber(probe, probe, 0))

	address := net.JoinHostPort("agent01.example.test", port)
	w := enrollAdvertising(t, srv, address)

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.True(t, reg.called)
	assert.Equal(t, address, reg.req.Host)
	assert.Equal(t, string(orchestration.FleetNodeReachable), reg.req.Reachability)
}

// TestHandleFleetEnroll_RefusesWhenNoResolvedAddressAnswers: a name none of
// whose addresses answers is still refused with the typed 422.
func TestHandleFleetEnroll_RefusesWhenNoResolvedAddressAnswers(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	_, port, err := net.SplitHostPort(l.Addr().String())
	require.NoError(t, err)
	// Closed now, so loopback refuses on this port as the first address does.
	require.NoError(t, l.Close())

	srv := newFleetEnrollTestServer(t, "")
	reg := &fakeFleetRegistrar{}
	srv.SetFleetRegistrar(reg)
	probe := firstAddressRefused{}
	srv.SetFleetAddressProber(orchestration.NewAgentAddressProber(probe, probe, 0))

	address := net.JoinHostPort("agent01.example.test", port)
	w := enrollAdvertising(t, srv, address)

	require.Equal(t, http.StatusUnprocessableEntity, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "agent_address_unreachable")
	assert.False(t, reg.called, "an unreachable node must not be registered")
}

// TestHandleFleetEnroll_UnprobedAddressIsUnverified: with no prober wired the
// derived address is registered and marked unverified rather than reachable.
func TestHandleFleetEnroll_UnprobedAddressIsUnverified(t *testing.T) {
	srv := newFleetEnrollTestServer(t, "")
	reg := &fakeFleetRegistrar{}
	srv.SetFleetRegistrar(reg)

	body := `{"tenant_id":"acme","bind_addr":"192.168.1.50:9091","hostname":"agent01"}`
	w := httptest.NewRecorder()
	srv.HandleFleetEnroll(w, agentRegisterReq(t, body, agentRegisterTestPeerCert(t, "acme", "agent01")))

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.True(t, reg.called)
	assert.Equal(t, "192.168.1.50:9091", reg.req.Host)
	assert.Empty(t, reg.req.AdvertisedAddress)
	assert.Equal(t, string(orchestration.FleetNodeReachabilityUnverified), reg.req.Reachability)
	assert.False(t, strings.Contains(w.Body.String(), "agent_address_unreachable"))
}

// TestHandleFleetEnroll_ClusterRefusesALoopbackAdvertise: on a clustered
// control plane a loopback advertise is refused with a 422 naming the fix,
// even though the node hosting the agent could dial it: no other node can.
func TestHandleFleetEnroll_ClusterRefusesALoopbackAdvertise(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	_, port, err := net.SplitHostPort(l.Addr().String())
	require.NoError(t, err)

	for _, host := range []string{"127.0.0.1", "::1", "localhost"} {
		t.Run(host, func(t *testing.T) {
			srv := newFleetEnrollTestServer(t, "")
			reg := &fakeFleetRegistrar{}
			srv.SetFleetRegistrar(reg)
			srv.SetFleetAddressProber(orchestration.NewAgentAddressProber(nil, nil, 0))
			srv.SetFleetClusteredControlPlane(true)

			w := enrollAdvertising(t, srv, net.JoinHostPort(host, port))

			require.Equal(t, http.StatusUnprocessableEntity, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), "agent_address_loopback_in_cluster")
			assert.Contains(t, w.Body.String(), "--advertise")
			assert.False(t, reg.called, "a loopback node must not be registered on a cluster")
		})
	}
}

// TestHandleFleetEnroll_ClusterRefusesADerivedLoopbackAddress: with no
// advertise, an address derived from a loopback bind is refused the same way.
func TestHandleFleetEnroll_ClusterRefusesADerivedLoopbackAddress(t *testing.T) {
	srv := newFleetEnrollTestServer(t, "")
	reg := &fakeFleetRegistrar{}
	srv.SetFleetRegistrar(reg)
	srv.SetFleetClusteredControlPlane(true)

	body := `{"tenant_id":"acme","bind_addr":"127.0.0.1:9091","hostname":"agent01"}`
	w := httptest.NewRecorder()
	srv.HandleFleetEnroll(w, agentRegisterReq(t, body, agentRegisterTestPeerCert(t, "acme", "agent01")))

	require.Equal(t, http.StatusUnprocessableEntity, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "agent_address_loopback_in_cluster")
	assert.False(t, reg.called)
}

// TestHandleFleetEnroll_SingleNodeAcceptsALoopbackAdvertise: a single-node
// control plane is the agent's only dialer, so loopback stays valid.
func TestHandleFleetEnroll_SingleNodeAcceptsALoopbackAdvertise(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })

	srv := newFleetEnrollTestServer(t, "")
	reg := &fakeFleetRegistrar{}
	srv.SetFleetRegistrar(reg)
	srv.SetFleetAddressProber(orchestration.NewAgentAddressProber(nil, nil, 0))
	srv.SetFleetClusteredControlPlane(false)

	w := enrollAdvertising(t, srv, l.Addr().String())

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.True(t, reg.called)
	assert.Equal(t, string(orchestration.FleetNodeReachable), reg.req.Reachability)
}

// TestHandleFleetEnroll_ClusterProbesARoutableAdvertise: on a cluster a
// routable advertise goes to the reachability probe as before: one that
// answers is registered reachable, one that does not is refused unreachable.
func TestHandleFleetEnroll_ClusterProbesARoutableAdvertise(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	_, port, err := net.SplitHostPort(l.Addr().String())
	require.NoError(t, err)

	srv := newFleetEnrollTestServer(t, "")
	reg := &fakeFleetRegistrar{}
	srv.SetFleetRegistrar(reg)
	probe := firstAddressRefused{}
	srv.SetFleetAddressProber(orchestration.NewAgentAddressProber(probe, probe, 0))
	srv.SetFleetClusteredControlPlane(true)

	address := net.JoinHostPort("agent01.example.test", port)
	w := enrollAdvertising(t, srv, address)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	assert.Equal(t, address, reg.req.Host)
	assert.Equal(t, string(orchestration.FleetNodeReachable), reg.req.Reachability)

	unreachable := newFleetEnrollTestServer(t, "")
	unreachable.SetFleetRegistrar(&fakeFleetRegistrar{})
	unreachable.SetFleetAddressProber(orchestration.NewAgentAddressProber(nil, nil, 0))
	unreachable.SetFleetClusteredControlPlane(true)
	w = enrollAdvertising(t, unreachable, "agent01.invalid:9091")
	require.Equal(t, http.StatusUnprocessableEntity, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "agent_address_unreachable")
}
