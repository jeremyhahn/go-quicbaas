//go:build rest

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

package rest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/capabilities"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	testca "github.com/jeremyhahn/go-quicbaas/pkg/testutil/xkmsca"
)

// capabilityReasons is every reason string the report may carry. A reason
// names a setting or build tag; it never carries a configured value, so the
// body is checked against this fixed set.
var capabilityReasons = map[string]bool{
	"":                              true,
	"build with -tags timeseries":   true,
	"no messaging service is wired": true,
	"no agent service is wired":     true,
	"the agent service does not support session account rotation": true,
	"no xkms certificate authority is configured":                 true,
	"no vector service is wired":                                  true,
	"no scheduler service is wired":                               true,
}

// getCapabilities calls HandleCapabilities and decodes the report.
func getCapabilities(t *testing.T, s *Server) capabilities.Report {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/capabilities", nil)
	rec := httptest.NewRecorder()
	s.HandleCapabilities(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("HandleCapabilities status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var report capabilities.Report
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode capabilities report: %v", err)
	}
	return report
}

// requireUnconfigured asserts a subsystem is reported not configured with a
// reason.
func requireUnconfigured(t *testing.T, report capabilities.Report, name string) {
	t.Helper()
	sub, ok := report.Subsystems[name]
	if !ok {
		t.Fatalf("report has no %q subsystem: %+v", name, report.Subsystems)
	}
	if sub.Configured {
		t.Errorf("%s: configured = true, want false", name)
	}
	if sub.Reason == "" {
		t.Errorf("%s: reason is empty, want the setting that enables it", name)
	}
}

// requireConfigured asserts a subsystem is reported served and configured.
func requireConfigured(t *testing.T, report capabilities.Report, name string) {
	t.Helper()
	sub, ok := report.Subsystems[name]
	if !ok {
		t.Fatalf("report has no %q subsystem: %+v", name, report.Subsystems)
	}
	if !sub.Served || !sub.Configured || sub.Reason != "" {
		t.Errorf("%s = %+v, want served and configured with no reason", name, sub)
	}
}

// wireAll wires every subsystem the report checks on a mock server.
func wireAll(t *testing.T, s *Server, m *testutil.MockQuicBaaS) {
	t.Helper()
	m.On("Messaging").Return(&stubMessagingService{})
	m.AgentServiceOverride = &rotatingMockAgentService{mockAgentService: &mockAgentService{}}
	s.SetCertificateAuthority(testca.New(t, "capabilities-ca"))
}

func TestCapabilities_TimeSeriesUnservedWithoutBuildTag(t *testing.T) {
	server, m, cleanup := setupMockServer(t)
	defer cleanup()
	wireAll(t, server, m)

	report := getCapabilities(t, server)
	if capabilities.TimeSeriesServed {
		requireConfigured(t, report, "timeseries")
		return
	}
	requireUnconfigured(t, report, "timeseries")
	if report.Subsystems["timeseries"].Served {
		t.Error("timeseries: served = true in a binary built without the timeseries tag")
	}
}

func TestCapabilities_MessagingUnwired(t *testing.T) {
	server, m, cleanup := setupMockServer(t)
	defer cleanup()
	wireAll(t, server, m)
	m.ExpectedCalls = nil
	m.On("Messaging").Return(nil)

	report := getCapabilities(t, server)
	requireUnconfigured(t, report, "messaging")
	requireConfigured(t, report, "session_rotation")
}

func TestCapabilities_SessionRotationUnwired(t *testing.T) {
	t.Run("no agent service", func(t *testing.T) {
		server, m, cleanup := setupMockServer(t)
		defer cleanup()
		wireAll(t, server, m)
		m.AgentServiceOverride = nil

		requireUnconfigured(t, getCapabilities(t, server), "session_rotation")
	})
	t.Run("agent service without rotation", func(t *testing.T) {
		server, m, cleanup := setupMockServer(t)
		defer cleanup()
		wireAll(t, server, m)
		m.AgentServiceOverride = &mockAgentService{}

		requireUnconfigured(t, getCapabilities(t, server), "session_rotation")
	})
}

func TestCapabilities_XKMSCAUnwired(t *testing.T) {
	server, m, cleanup := setupMockServer(t)
	defer cleanup()
	wireAll(t, server, m)
	server.SetCertificateAuthority(nil)

	report := getCapabilities(t, server)
	requireUnconfigured(t, report, "xkms_ca")
	requireConfigured(t, report, "messaging")
}

func TestCapabilities_FullyWired(t *testing.T) {
	server, m, cleanup := setupMockServer(t)
	defer cleanup()
	wireAll(t, server, m)

	report := getCapabilities(t, server)
	for _, name := range []string{"messaging", "session_rotation", "xkms_ca", "vector", "scheduler"} {
		requireConfigured(t, report, name)
	}
	if len(report.Families) == 0 {
		t.Error("families is empty, want the SDK families this server serves")
	}
}

// TestCapabilities_RefusesUnauthenticated pins the route's class. The unified
// server registers every non-public route behind its authenticator and only
// RouteAuthPublic routes without one, so RouteAuthSession is what refuses an
// unauthenticated caller; RouteAuthPublic would admit one.
func TestCapabilities_RefusesUnauthenticated(t *testing.T) {
	var found bool
	for _, spec := range Routes() {
		if spec.Method != http.MethodGet || spec.Pattern != "/api/v1/capabilities" {
			continue
		}
		found = true
		if spec.Auth != RouteAuthSession {
			t.Errorf("GET /api/v1/capabilities auth = %v, want RouteAuthSession (authenticated, no permission)", spec.Auth)
		}
		if spec.Resource != "" || spec.Action != "" {
			t.Errorf("GET /api/v1/capabilities permission = %s:%s, want none", spec.Resource, spec.Action)
		}
	}
	if !found {
		t.Fatal("Routes() has no GET /api/v1/capabilities")
	}
}

func TestCapabilities_BodyCarriesNoConfigValue(t *testing.T) {
	server, m, cleanup := setupMockServer(t)
	defer cleanup()
	m.On("Messaging").Return(nil)

	report := getCapabilities(t, server)
	if len(report.Subsystems) == 0 {
		t.Fatal("report has no subsystems")
	}
	for name, sub := range report.Subsystems {
		if !capabilityReasons[sub.Reason] {
			t.Errorf("%s: reason %q is not one of the fixed reason strings", name, sub.Reason)
		}
	}
}
