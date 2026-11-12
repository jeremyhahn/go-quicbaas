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

//go:build integration

// Package placement is the multi-transport integration suite for the
// scheduler's workload placement engine and for Async().Scheduler() job
// methods, driven through the SDK against the integration cluster's real
// server on every transport.
package placement

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler/schedulertest"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// Admin client certificate paths in the integration container, overridable
// exactly as the login suite's are.
const (
	adminCertPath = "/etc/quicbaas/certs/admin.crt"
	adminKeyPath  = "/etc/quicbaas/certs/admin.key"
)

// adminTLS trusts the cluster CA and presents the admin certificate, an
// identity holding placement:manage and the scheduler job permissions.
func adminTLS(t *testing.T) *tls.Config {
	t.Helper()
	cfg, err := helpers.IntegrationTLSConfig()
	if err != nil {
		t.Fatalf("integration TLS config: %v", err)
	}
	certPath, keyPath := adminCertPath, adminKeyPath
	if v := os.Getenv("QUICBAAS_TEST_PIV_CERT"); v != "" {
		certPath = v
	}
	if v := os.Getenv("QUICBAAS_TEST_PIV_KEY"); v != "" {
		keyPath = v
	}
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatalf("admin client certificate %s: %v", certPath, err)
	}
	cfg.Certificates = []tls.Certificate{cert}
	return cfg
}

// TestMain deletes the tenant this package's principals acted in once the
// tests have all run, keeping m.Run's exit code.
func TestMain(m *testing.M) {
	code := m.Run()
	if err := helpers.ReleaseDataPlaneTenants(); err != nil {
		fmt.Fprintf(os.Stderr, "release data-plane tenants: %v\n", err)
	}
	os.Exit(code)
}

// bearerScheme is what the helpers' per-RPC credentials put before the token
// in the authorization metadata.
const bearerScheme = "Bearer "

// restEndpoint returns the first REST endpoint, the one the helpers log
// their principals in at.
func restEndpoint(t *testing.T) string {
	t.Helper()
	endpoints := helpers.GetRESTEndpoints()
	if len(endpoints) == 0 {
		t.Fatalf("no REST endpoint configured")
	}
	return endpoints[0]
}

// operatorBearer returns the tenant operator's bearer for the SDK's
// WithAuthToken. The helpers hand the operator out as per-RPC credentials
// only, so the token is read from the authorization metadata they send.
func operatorBearer(t *testing.T) string {
	t.Helper()
	md, err := helpers.TenantOperatorGRPCCredentials(t).GetRequestMetadata(context.Background())
	if err != nil {
		t.Fatalf("tenant operator bearer: %v", err)
	}
	token, ok := strings.CutPrefix(md["authorization"], bearerScheme)
	if !ok || token == "" {
		t.Fatalf("tenant operator credentials sent no bearer")
	}
	return token
}

// transports builds an SDK client per transport. Every client presents
// bearer, so the server's authorization gate binds the tenant and checks the
// permissions of the principal the bearer was issued to.
func transports(t *testing.T, bearer string) map[string]func() (quicbaas.Client, error) {
	tlsCfg := adminTLS(t)
	first := func(endpoints []string, name string) string {
		if len(endpoints) == 0 {
			t.Fatalf("no %s endpoint configured", name)
		}
		return endpoints[0]
	}
	timeout := quicbaas.WithTimeout(30 * time.Second)
	auth := quicbaas.WithAuthToken(bearer)
	return map[string]func() (quicbaas.Client, error){
		"REST": func() (quicbaas.Client, error) {
			return quicbaas.NewClient(first(helpers.GetRESTEndpoints(), "REST"), quicbaas.WithREST(), quicbaas.WithTLSConfig(tlsCfg), auth, timeout)
		},
		"gRPC": func() (quicbaas.Client, error) {
			return quicbaas.NewClient(first(helpers.GetClusterEndpoints(), "gRPC"), quicbaas.WithTLSConfig(tlsCfg), auth, timeout)
		},
		"QUIC": func() (quicbaas.Client, error) {
			quicCfg := tlsCfg.Clone()
			quicCfg.NextProtos = []string{"quicbaas-quic"}
			return quicbaas.NewClient(first(helpers.GetQUICEndpoints(), "QUIC"), quicbaas.WithQUIC(), quicbaas.WithTLSConfig(quicCfg), auth, timeout)
		},
		"Unix": func() (quicbaas.Client, error) {
			socket := helpers.GetUnixSocketPath()
			unixCfg := tlsCfg.Clone()
			unixCfg.ServerName = "localhost"
			return quicbaas.NewClient("unix://"+socket, quicbaas.WithUnix(socket), quicbaas.WithTLSConfig(unixCfg), auth, timeout)
		},
		"MCP": func() (quicbaas.Client, error) {
			return quicbaas.NewClient(first(helpers.GetMCPEndpoints(), "MCP"), quicbaas.WithMCP(), quicbaas.WithTLSConfig(tlsCfg), auth, timeout)
		},
	}
}

// TestPlacementMultiProtocol runs the full placement conformance — node
// registration on a lease, constraints, anti-affinity, spread, idempotent
// placement, cordon, drain, release, deregistration and a refused placement
// — on every transport, each in its own ID namespace.
func TestPlacementMultiProtocol(t *testing.T) {
	stamp := strconv.FormatInt(time.Now().UnixNano(), 36)
	// Registering, cordoning and draining a node needs placement:manage,
	// which only the tenant's Admin role holds, so the conformance acts as
	// the tenant principal.
	bearer := helpers.AuthTenantPrincipal(t, restEndpoint(t))
	for name, build := range transports(t, bearer) {
		t.Run(name, func(t *testing.T) {
			client, err := build()
			if err != nil {
				t.Fatalf("%s client: %v", name, err)
			}
			defer func() {
				if err := client.Close(); err != nil {
					t.Errorf("close %s client: %v", name, err)
				}
			}()
			schedulertest.RunPlacementConformance(t, context.Background(), client.Async().Scheduler(), "it-"+stamp+"-"+name+"-")
		})
	}
}

// TestSchedulerJobsMultiProtocol proves Async().Scheduler() job methods work
// on every transport: a job created over the transport is read back,
// paused, resumed and deleted through the same transport.
func TestSchedulerJobsMultiProtocol(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	stamp := strconv.FormatInt(time.Now().UnixNano(), 36)

	// The Operator role holds every scheduler job permission this test
	// uses, so the jobs act as the tenant operator.
	for name, build := range transports(t, operatorBearer(t)) {
		t.Run(name, func(t *testing.T) {
			client, err := build()
			if err != nil {
				t.Fatalf("%s client: %v", name, err)
			}
			defer func() {
				if err := client.Close(); err != nil {
					t.Errorf("close %s client: %v", name, err)
				}
			}()
			jobs := client.Async().Scheduler()

			created, err := jobs.CreateJob(ctx, &scheduler.CreateJobRequest{
				Name:      "it-" + stamp + "-" + name,
				JobType:   scheduler.JobTypeRecurring,
				Handler:   "noop",
				Schedule:  "0 3 * * *",
				OwnerType: scheduler.OwnerTypeSystem,
				Tags:      map[string]string{"suite": "placement"},
			})
			if err != nil {
				t.Fatalf("CreateJob: %v", err)
			}
			info, err := jobs.GetJob(ctx, created.JobID)
			if err != nil {
				t.Fatalf("GetJob: %v", err)
			}
			if info.Name != "it-"+stamp+"-"+name || info.JobType != scheduler.JobTypeRecurring || info.Tags["suite"] != "placement" {
				t.Errorf("GetJob decoded %+v", info)
			}
			if _, err := jobs.PauseJob(ctx, created.JobID); err != nil {
				t.Fatalf("PauseJob: %v", err)
			}
			if paused, err := jobs.GetJob(ctx, created.JobID); err != nil || paused.Status != scheduler.JobStatusPaused {
				t.Errorf("after PauseJob: %+v, %v", paused, err)
			}
			if _, err := jobs.ResumeJob(ctx, created.JobID); err != nil {
				t.Fatalf("ResumeJob: %v", err)
			}
			if _, err := jobs.GetStatus(ctx); err != nil {
				t.Fatalf("GetStatus: %v", err)
			}
			if err := jobs.DeleteJob(ctx, created.JobID); err != nil {
				t.Fatalf("DeleteJob: %v", err)
			}
		})
	}
}
