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

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	registrySDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/registry"
)

// fakeRegistryClient records what each command sent.
type fakeRegistryClient struct {
	gotRegister   *registrySDK.RegisterRequest
	gotRenew      int64
	gotDeregister int64
	gotResolve    string
	gotWatch      *registrySDK.WatchRequest
	events        []registrySDK.Event
	err           error
}

func (f *fakeRegistryClient) Register(_ context.Context, req *registrySDK.RegisterRequest) (*registrySDK.RegisterResponse, error) {
	f.gotRegister = req
	if f.err != nil {
		return nil, f.err
	}
	return &registrySDK.RegisterResponse{LeaseID: 42, TTLSeconds: req.TTLSeconds}, nil
}

func (f *fakeRegistryClient) Renew(_ context.Context, leaseID int64) (*registrySDK.RenewResponse, error) {
	f.gotRenew = leaseID
	if f.err != nil {
		return nil, f.err
	}
	return &registrySDK.RenewResponse{LeaseID: leaseID, TTLSeconds: 15}, nil
}

func (f *fakeRegistryClient) Deregister(_ context.Context, leaseID int64) error {
	f.gotDeregister = leaseID
	return f.err
}

func (f *fakeRegistryClient) Resolve(_ context.Context, service string) (*registrySDK.ResolveResponse, error) {
	f.gotResolve = service
	if f.err != nil {
		return nil, f.err
	}
	return &registrySDK.ResolveResponse{
		Instances: []registrySDK.ServiceInstance{{ID: "node-a", Service: service, Address: "10.0.4.7:8443", Metadata: map[string]string{"zone": "z1", "arch": "arm64"}}},
		Revision:  9,
	}, nil
}

func (f *fakeRegistryClient) Watch(_ context.Context, req *registrySDK.WatchRequest) (<-chan registrySDK.Event, error) {
	f.gotWatch = req
	if f.err != nil {
		return nil, f.err
	}
	ch := make(chan registrySDK.Event, len(f.events))
	for _, ev := range f.events {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

func withFakeRegistry(t *testing.T, fake *fakeRegistryClient) {
	t.Helper()
	origFactory, origTimeout, origOutput := registryClientFactory, globalTimeout, registryOutputFmt
	registryClientFactory = func() (registrySDK.Client, error) { return fake, nil }
	globalTimeout = 5 * time.Second
	t.Cleanup(func() {
		registryClientFactory, globalTimeout, registryOutputFmt = origFactory, origTimeout, origOutput
		registryService, registryInstanceID, registryAddress, registryTTL, registryMetadata, registryFromRevision, registryMaxEvents = "", "", "", 15, nil, 0, 0
	})
}

func runRegistryCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newRegistryCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestRegistryCLI_Register(t *testing.T) {
	fake := &fakeRegistryClient{}
	withFakeRegistry(t, fake)
	out, err := runRegistryCLI(t, "register", "--service", "api", "--id", "node-a", "--address", "10.0.4.7:8443",
		"--ttl", "20", "--meta", "zone=z1", "--meta", "arch=arm64", "--output", "json")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	want := registrySDK.ServiceInstance{ID: "node-a", Service: "api", Address: "10.0.4.7:8443", Metadata: map[string]string{"zone": "z1", "arch": "arm64"}}
	if fake.gotRegister.TTLSeconds != 20 || fake.gotRegister.Instance.Address != want.Address || fake.gotRegister.Instance.Metadata["arch"] != "arm64" {
		t.Fatalf("sent %+v", fake.gotRegister)
	}
	var lease registrySDK.RegisterResponse
	if err := json.Unmarshal([]byte(out), &lease); err != nil || lease.LeaseID != 42 {
		t.Fatalf("output %q (%v)", out, err)
	}
}

func TestRegistryCLI_RenewDeregisterResolve(t *testing.T) {
	fake := &fakeRegistryClient{}
	withFakeRegistry(t, fake)

	out, err := runRegistryCLI(t, "renew", "42")
	if err != nil || fake.gotRenew != 42 || !strings.Contains(out, "15s") {
		t.Fatalf("renew: %q, %v", out, err)
	}
	out, err = runRegistryCLI(t, "deregister", "43")
	if err != nil || fake.gotDeregister != 43 || !strings.Contains(out, "43") {
		t.Fatalf("deregister: %q, %v", out, err)
	}
	out, err = runRegistryCLI(t, "resolve", "api", "--output", "table")
	if err != nil || fake.gotResolve != "api" {
		t.Fatalf("resolve: %q, %v", out, err)
	}
	if !strings.Contains(out, "node-a") || !strings.Contains(out, "arch=arm64,zone=z1") || !strings.Contains(out, "9") {
		t.Fatalf("resolve table = %q", out)
	}
	out, err = runRegistryCLI(t, "resolve", "api", "--output", "yaml")
	if err != nil || !strings.Contains(out, "revision: 9") {
		t.Fatalf("resolve yaml = %q, %v", out, err)
	}
}

func TestRegistryCLI_Watch(t *testing.T) {
	fake := &fakeRegistryClient{events: []registrySDK.Event{
		{Type: registrySDK.EventPut, Instance: registrySDK.ServiceInstance{ID: "node-a", Address: "x:1"}, Revision: 10},
		{Type: registrySDK.EventDelete, Instance: registrySDK.ServiceInstance{ID: "node-a"}, Revision: 11},
	}}
	withFakeRegistry(t, fake)
	out, err := runRegistryCLI(t, "watch", "api", "--from-revision", "10")
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	if fake.gotWatch.FromRevision != 10 || !strings.Contains(out, "10\tPUT\tnode-a\tx:1") || !strings.Contains(out, "11\tDELETE") {
		t.Fatalf("watch output %q (sent %+v)", out, fake.gotWatch)
	}

	out, err = runRegistryCLI(t, "watch", "api", "--max-events", "1", "--output", "json")
	if err != nil || strings.Count(out, "\n") != 1 || !strings.Contains(out, `"revision":10`) {
		t.Fatalf("watch --max-events 1 = %q, %v", out, err)
	}

	ended := &fakeRegistryClient{events: []registrySDK.Event{{Err: &registrySDK.WatchCompactedError{Service: "api"}}}}
	withFakeRegistry(t, ended)
	if _, err := runRegistryCLI(t, "watch", "api"); err == nil {
		t.Fatal("a watch ended by the server must fail the command")
	}
}

func TestRegistryCLI_Errors(t *testing.T) {
	withFakeRegistry(t, &fakeRegistryClient{})
	if _, err := runRegistryCLI(t, "renew", "abc"); err == nil {
		t.Fatal("renew accepted a non-numeric lease")
	}
	if _, err := runRegistryCLI(t, "deregister", "0"); err == nil {
		t.Fatal("deregister accepted lease 0")
	}
	if _, err := runRegistryCLI(t, "register", "--service", "api", "--id", "a", "--address", "x:1", "--meta", "novalue"); err == nil {
		t.Fatal("register accepted malformed metadata")
	}
	if _, err := runRegistryCLI(t, "resolve", "api", "--output", "xml"); err == nil {
		t.Fatal("resolve accepted an unknown output format")
	}

	boom := errors.New("server down")
	withFakeRegistry(t, &fakeRegistryClient{err: boom})
	for _, args := range [][]string{
		{"register", "--service", "api", "--id", "a", "--address", "x:1"},
		{"renew", "1"}, {"deregister", "1"}, {"resolve", "api"}, {"watch", "api"},
	} {
		if _, err := runRegistryCLI(t, args...); err == nil {
			t.Fatalf("%v did not report the client failure", args)
		}
	}

	orig := registryClientFactory
	registryClientFactory = func() (registrySDK.Client, error) { return nil, boom }
	defer func() { registryClientFactory = orig }()
	if _, err := runRegistryCLI(t, "resolve", "api"); err == nil {
		t.Fatal("a factory failure must fail the command")
	}
	if (&InvalidLeaseIDError{Value: "x"}).Error() == "" || (&InvalidRegistryMetadataError{Value: "x"}).Error() == "" {
		t.Fatal("empty error messages")
	}
}
