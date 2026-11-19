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

package capabilities

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

func TestCapabilitiesBuild_NilServiceReportsEveryServiceUnconfigured(t *testing.T) {
	report := Build(nil, Probes{})
	for _, name := range []string{SubsystemMessaging, SubsystemSessionRotation, SubsystemXKMSCA, SubsystemVector, SubsystemScheduler} {
		sub := report.Subsystems[name]
		if sub.Configured || sub.Reason == "" {
			t.Errorf("%s = %+v, want not configured with a reason", name, sub)
		}
	}
}

func TestCapabilitiesBuild_TimeSeriesFollowsBuildTag(t *testing.T) {
	sub := Build(nil, Probes{}).Subsystems[SubsystemTimeSeries]
	if sub.Served != TimeSeriesServed || sub.Configured != TimeSeriesServed {
		t.Errorf("timeseries = %+v, want served and configured = %v", sub, TimeSeriesServed)
	}
	if !TimeSeriesServed && sub.Reason != ReasonTimeSeriesTag {
		t.Errorf("timeseries reason = %q, want %q", sub.Reason, ReasonTimeSeriesTag)
	}
}

func TestCapabilitiesBuild_ProbeAndServiceWired(t *testing.T) {
	m := testutil.NewMockQuicBaaS()
	m.On("Messaging").Return(nil)

	report := Build(m, Probes{CertificateAuthority: func() bool { return true }})
	if sub := report.Subsystems[SubsystemXKMSCA]; !sub.Configured || sub.Reason != "" {
		t.Errorf("xkms_ca = %+v, want configured", sub)
	}
	if sub := report.Subsystems[SubsystemVector]; !sub.Configured {
		t.Errorf("vector = %+v, want configured", sub)
	}
	if sub := report.Subsystems[SubsystemMessaging]; sub.Configured || sub.Reason != ReasonNoMessaging {
		t.Errorf("messaging = %+v, want not configured", sub)
	}
	if sub := report.Subsystems[SubsystemSessionRotation]; sub.Configured || sub.Reason != ReasonNoAgent {
		t.Errorf("session_rotation = %+v, want not configured, no agent", sub)
	}
	if len(report.Families) == 0 {
		t.Error("families is empty")
	}
}
