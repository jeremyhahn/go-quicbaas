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
	"slices"
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

// tsStorageService is a facade that reports whether its time series storage
// is wired.
type tsStorageService struct {
	*testutil.MockQuicBaaS
	wired bool
}

func (s tsStorageService) TimeSeriesStorageConfigured() bool { return s.wired }

func TestCapabilitiesBuild_TimeSeriesUnservedWithoutProbe(t *testing.T) {
	report := Build(nil, Probes{})
	sub := report.Subsystems[SubsystemTimeSeries]
	if sub.Served || sub.Configured || sub.Reason != ReasonTimeSeriesTag {
		t.Errorf("timeseries = %+v, want not served, not configured, reason %q", sub, ReasonTimeSeriesTag)
	}
	if slices.Contains(report.Families, SubsystemTimeSeries) {
		t.Errorf("families = %v, want no %q", report.Families, SubsystemTimeSeries)
	}
}

func TestCapabilitiesBuild_TimeSeriesServedButStorageUnwired(t *testing.T) {
	for _, wired := range []bool{false, true} {
		m := testutil.NewMockQuicBaaS()
		m.On("Messaging").Return(nil)
		report := Build(tsStorageService{m, wired}, Probes{TimeSeriesServed: true})
		sub := report.Subsystems[SubsystemTimeSeries]
		wantReason := ""
		if !wired {
			wantReason = ReasonNoTimeSeriesStorage
		}
		if !sub.Served || sub.Configured != wired || sub.Reason != wantReason {
			t.Errorf("wired=%v: timeseries = %+v, want served, configured %v, reason %q", wired, sub, wired, wantReason)
		}
		if !slices.Contains(report.Families, SubsystemTimeSeries) {
			t.Errorf("wired=%v: families = %v, want %q", wired, report.Families, SubsystemTimeSeries)
		}
	}

	sub := Build(nil, Probes{TimeSeriesServed: true}).Subsystems[SubsystemTimeSeries]
	if !sub.Served || sub.Configured || sub.Reason != ReasonNoTimeSeriesStorage {
		t.Errorf("nil service: timeseries = %+v, want served, not configured, reason %q", sub, ReasonNoTimeSeriesStorage)
	}
}

func TestCapabilitiesBuild_RAGFamilyFollowsProbe(t *testing.T) {
	if families := Build(nil, Probes{}).Families; slices.Contains(families, FamilyRAG) {
		t.Errorf("families = %v, want no %q without the probe", families, FamilyRAG)
	}
	if families := Build(nil, Probes{RAGServed: true}).Families; !slices.Contains(families, FamilyRAG) {
		t.Errorf("families = %v, want %q with the probe", families, FamilyRAG)
	}
}

func TestCapabilitiesBuild_ObjectTransferFamilyFollowsProbe(t *testing.T) {
	if families := Build(nil, Probes{}).Families; slices.Contains(families, FamilyObjectTransfer) {
		t.Errorf("families = %v, want no %q without the probe", families, FamilyObjectTransfer)
	}
	if families := Build(nil, Probes{ObjectTransferServed: true}).Families; !slices.Contains(families, FamilyObjectTransfer) {
		t.Errorf("families = %v, want %q with the probe", families, FamilyObjectTransfer)
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
