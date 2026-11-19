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

// Package capabilities builds the report of which subsystems a server serves
// and has configured. Every transport (REST, QUIC, gRPC, Unix, MCP and
// Embedded) answers its capabilities request with Build, so the subsystem list
// and the checks behind it live in one place.
//
// Each subsystem's check is the one its handlers make before they answer 501
// or 503, so a subsystem the report calls configured is one whose handlers
// answer. A reason names the setting or build tag that enables a subsystem;
// it never carries a configured value.
package capabilities

import (
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// Subsystem names in the report.
const (
	SubsystemTimeSeries      = "timeseries"
	SubsystemMessaging       = "messaging"
	SubsystemSessionRotation = "session_rotation"
	SubsystemXKMSCA          = "xkms_ca"
	SubsystemVector          = "vector"
	SubsystemScheduler       = "scheduler"
)

// Reasons a subsystem is not served or not configured. The set is fixed: a
// reason names a setting or build tag, never a value.
const (
	ReasonTimeSeriesTag      = "build with -tags timeseries"
	ReasonNoMessaging        = "no messaging service is wired"
	ReasonNoAgent            = "no agent service is wired"
	ReasonNoSessionRotation  = "the agent service does not support session account rotation"
	ReasonNoCertificateAuthn = "no xkms certificate authority is configured"
	ReasonNoVector           = "no vector service is wired"
	ReasonNoScheduler        = "no scheduler service is wired"
)

// Subsystem is one subsystem's entry in the report.
type Subsystem struct {
	// Served is true when this binary has the subsystem's handlers.
	Served bool `json:"served"`
	// Configured is true when the service those handlers use is wired.
	Configured bool `json:"configured"`
	// Reason is empty when the subsystem is served and configured, and
	// otherwise names the setting or build tag that enables it.
	Reason string `json:"reason"`
}

// Report is the capabilities report a server returns.
type Report struct {
	// Subsystems maps a subsystem name to its entry.
	Subsystems map[string]Subsystem `json:"subsystems"`
	// Families lists the SDK families this server serves.
	Families []string `json:"families"`
}

// Probes carries the state that lives on a transport server rather than on
// the service facade. A nil probe reports the state as not configured.
type Probes struct {
	// CertificateAuthority reports whether an xkms certificate authority is
	// wired (on REST, the server's CA handlers are set).
	CertificateAuthority func() bool
}

// baseFamilies are the SDK families every server serves, whatever its build
// tags and wiring. Families of optional subsystems are added by Build.
var baseFamilies = []string{
	"account", "acme", "agents", "attestation", "auth", "backup", "batch",
	"cache", "certificate", "cluster", "crypto", "dek", "dynamicsecrets",
	"embedding", "health", "iam", "kms", "kv", "objectstorage",
	"objectstorageadmin", "objecttransfer", "ollama", "operations",
	"orchestrator", "parameterstore", "project", "prompt", "query", "rag",
	"rbac", "registry", "sdlc", "secrets", "spend", "ssh", "transaction",
	"trust",
}

// Build returns the capabilities report for svc. probes carries the state a
// transport server holds; pass the zero Probes when there is none.
func Build(svc service.QuicBaaS, probes Probes) Report {
	report := Report{Subsystems: map[string]Subsystem{}}

	report.Subsystems[SubsystemTimeSeries] = timeSeries()
	report.Subsystems[SubsystemMessaging] = wired(svc != nil && svc.Messaging() != nil, ReasonNoMessaging)
	report.Subsystems[SubsystemSessionRotation] = sessionRotation(svc)
	report.Subsystems[SubsystemXKMSCA] = wired(probes.CertificateAuthority != nil && probes.CertificateAuthority(), ReasonNoCertificateAuthn)
	report.Subsystems[SubsystemVector] = wired(svc != nil && svc.Vector() != nil, ReasonNoVector)
	report.Subsystems[SubsystemScheduler] = wired(svc != nil && svc.Scheduler() != nil, ReasonNoScheduler)

	report.Families = append([]string(nil), baseFamilies...)
	for _, name := range []string{SubsystemMessaging, SubsystemTimeSeries, SubsystemVector, SubsystemScheduler} {
		if report.Subsystems[name].Served {
			report.Families = append(report.Families, name)
		}
	}
	return report
}

// wired reports a subsystem whose handlers are always compiled in and whose
// service is wired when ok is true.
func wired(ok bool, reason string) Subsystem {
	if ok {
		return Subsystem{Served: true, Configured: true}
	}
	return Subsystem{Served: true, Reason: reason}
}

// timeSeries reports the time series subsystem from the build tag: its REST
// handlers are compiled in only with -tags timeseries.
func timeSeries() Subsystem {
	if TimeSeriesServed {
		return Subsystem{Served: true, Configured: true}
	}
	return Subsystem{Reason: ReasonTimeSeriesTag}
}

// sessionRotation makes the check the rotate handlers make:
// orchestration.RotationServiceFrom on the wired agent service.
func sessionRotation(svc service.QuicBaaS) Subsystem {
	if svc == nil {
		return wired(false, ReasonNoAgent)
	}
	agentSvc := svc.Agent()
	if agentSvc == nil {
		return wired(false, ReasonNoAgent)
	}
	if _, err := orchestration.RotationServiceFrom(agentSvc, "capabilities"); err != nil {
		return wired(false, ReasonNoSessionRotation)
	}
	return wired(true, "")
}
