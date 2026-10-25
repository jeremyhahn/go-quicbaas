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

//go:build desktop

package main

import (
	"sync/atomic"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// embeddedFleetCARef is an atomic pointer to the CA of whichever
// embedded quicbaasd is currently running under the desktop process.
// `installEmbeddedFleetCA` publishes the CA when the server starts;
// `uninstallEmbeddedFleetCA` clears it on Stop.
//
// The agent-deploy factory consults this pointer via the
// resolveFleetNodeCertIssuer seam to mint daemon identities against the
// embedded CA. Invocations that happen before the embedded server is
// ready (or from a CLI process without an embedded server) get a typed
// error telling the operator where to run the command from.
var embeddedFleetCARef atomic.Pointer[xkmssdk.TrustedCA]

// embeddedFleetCertRecordsRef holds the embedded server's issued-cert record
// store beside its CA: every identity the deploy mints is recorded there, the
// only certificates fleet enrollment accepts (stopgap until go-quicbaas#1821).
var embeddedFleetCertRecordsRef atomic.Pointer[service.FleetNodeCertRecords]

// embeddedMCPEndpointRef holds the embedded server's reachable MCP endpoint
// (https://host:port/mcp), published when the server starts and cleared on
// Stop. The agent-deploy factory reads it via the resolveDeployMCPEndpoint
// seam so deployed daemons are started with --mcp-endpoint. Empty when the
// embedded server has MCP disabled or no reachable host could be derived.
var embeddedMCPEndpointRef atomic.Pointer[string]

// embeddedSPIFFETrustDomainRef holds the embedded server's configured SPIFFE
// trust domain, published when the server starts and cleared on Stop. The
// agent-deploy factory reads it via the resolveDeploySPIFFETrustDomain seam so
// the in-process CA mints the agent SPIFFE SAN fleet enrolment requires.
var embeddedSPIFFETrustDomainRef atomic.Pointer[string]

// init installs the embedded cert-issuer resolver so `quicbaas fleet
// deploy` talks to the in-process CA when the desktop build is active.
// The CLI-only build preserves its default resolver (which returns a
// typed error when invoked).
func init() {
	resolveFleetNodeCertIssuer = resolveEmbeddedFleetNodeCertIssuer
	resolveDeployMCPEndpoint = resolveEmbeddedDeployMCPEndpoint
	resolveDeploySPIFFETrustDomain = resolveEmbeddedDeploySPIFFETrustDomain
}

// installEmbeddedFleetCA publishes the running server's CA into the
// process-wide seam. Called by EmbeddedServerBinding.Start after
// server.initializeCA has completed.
func installEmbeddedFleetCA(ca xkmssdk.TrustedCA) {
	embeddedFleetCARef.Store(&ca)
}

// uninstallEmbeddedFleetCA clears the process-wide seam. Called by
// EmbeddedServerBinding.Stop so stale references cannot outlive the
// server that owned them.
func uninstallEmbeddedFleetCA() {
	embeddedFleetCARef.Store(nil)
}

// installEmbeddedFleetCertRecords publishes the embedded server's issued-cert
// record store. Called alongside installEmbeddedFleetCA.
func installEmbeddedFleetCertRecords(records service.FleetNodeCertRecords) {
	embeddedFleetCertRecordsRef.Store(&records)
}

// uninstallEmbeddedFleetCertRecords clears the record store seam when the
// embedded server stops.
func uninstallEmbeddedFleetCertRecords() {
	embeddedFleetCertRecordsRef.Store(nil)
}

// installEmbeddedMCPEndpoint publishes the embedded server's reachable MCP
// endpoint into the process-wide seam so `quicbaas fleet deploy` can stamp it
// onto each deployed daemon's command line. An empty endpoint clears the seam
// (MCP disabled or no reachable host), leaving the "quicbaas" preset
// unresolved on deployed agents.
func installEmbeddedMCPEndpoint(endpoint string) {
	if endpoint == "" {
		embeddedMCPEndpointRef.Store(nil)
		return
	}
	embeddedMCPEndpointRef.Store(&endpoint)
}

// uninstallEmbeddedMCPEndpoint clears the MCP endpoint seam on server Stop.
func uninstallEmbeddedMCPEndpoint() {
	embeddedMCPEndpointRef.Store(nil)
}

// resolveEmbeddedDeployMCPEndpoint returns the currently-published embedded MCP
// endpoint, or empty when none is set.
func resolveEmbeddedDeployMCPEndpoint() string {
	if p := embeddedMCPEndpointRef.Load(); p != nil {
		return *p
	}
	return ""
}

// installEmbeddedSPIFFETrustDomain publishes the embedded server's configured
// SPIFFE trust domain. An empty value clears the seam.
func installEmbeddedSPIFFETrustDomain(trustDomain string) {
	if trustDomain == "" {
		embeddedSPIFFETrustDomainRef.Store(nil)
		return
	}
	embeddedSPIFFETrustDomainRef.Store(&trustDomain)
}

// uninstallEmbeddedSPIFFETrustDomain clears the published trust domain.
func uninstallEmbeddedSPIFFETrustDomain() {
	embeddedSPIFFETrustDomainRef.Store(nil)
}

// resolveEmbeddedDeploySPIFFETrustDomain returns the published trust domain,
// or "" when no embedded server published one.
func resolveEmbeddedDeploySPIFFETrustDomain() string {
	if p := embeddedSPIFFETrustDomainRef.Load(); p != nil {
		return *p
	}
	return ""
}

// resolveEmbeddedFleetNodeCertIssuer wraps the currently-installed embedded
// CA in an XKMSFleetNodeCertIssuer. Returns a typed error when no embedded
// server is running so the operator sees a clear diagnostic rather
// than a nil-pointer panic.
func resolveEmbeddedFleetNodeCertIssuer() (service.FleetNodeCertIssuer, error) {
	caPtr := embeddedFleetCARef.Load()
	if caPtr == nil || *caPtr == nil {
		return nil, &deployNoCertIssuerError{}
	}
	recordsPtr := embeddedFleetCertRecordsRef.Load()
	if recordsPtr == nil || *recordsPtr == nil {
		return nil, &deployNoCertIssuerError{}
	}
	return service.NewXKMSFleetNodeCertIssuer(*caPtr, *recordsPtr)
}
