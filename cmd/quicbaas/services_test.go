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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	servicesSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/services"
)

// fakeServicesClient records each verb and the argument it received.
type fakeServicesClient struct {
	calls  []string
	signed *servicesSDK.SignedManifest
	err    error
}

func (f *fakeServicesClient) rec(verb, name string) (*servicesSDK.Record, error) {
	f.calls = append(f.calls, verb+" "+name)
	if f.err != nil {
		return nil, f.err
	}
	return &servicesSDK.Record{Tenant: "acme", Name: name, State: "running", Verb: verb, Revision: 3,
		KeyID: "k", Grants: map[string]string{"rbac": "rbac:acme/" + name, "certificate": "certificate:acme/" + name}}, nil
}

func (f *fakeServicesClient) manifest(verb string, m *servicesSDK.SignedManifest) (*servicesSDK.Record, error) {
	f.signed = m
	return f.rec(verb, "web")
}

func (f *fakeServicesClient) Deploy(_ context.Context, m *servicesSDK.SignedManifest) (*servicesSDK.Record, error) {
	return f.manifest("deploy", m)
}
func (f *fakeServicesClient) Redeploy(_ context.Context, m *servicesSDK.SignedManifest) (*servicesSDK.Record, error) {
	return f.manifest("redeploy", m)
}
func (f *fakeServicesClient) Undeploy(_ context.Context, name string) error {
	_, err := f.rec("undeploy", name)
	return err
}
func (f *fakeServicesClient) Start(_ context.Context, n string) (*servicesSDK.Record, error) {
	return f.rec("start", n)
}
func (f *fakeServicesClient) Stop(_ context.Context, n string) (*servicesSDK.Record, error) {
	return f.rec("stop", n)
}
func (f *fakeServicesClient) Resume(_ context.Context, n string) (*servicesSDK.Record, error) {
	return f.rec("resume", n)
}
func (f *fakeServicesClient) Restart(_ context.Context, n string) (*servicesSDK.Record, error) {
	return f.rec("restart", n)
}
func (f *fakeServicesClient) Get(_ context.Context, n string) (*servicesSDK.Record, error) {
	return f.rec("get", n)
}
func (f *fakeServicesClient) List(context.Context) (*servicesSDK.ListResponse, error) {
	f.calls = append(f.calls, "list")
	if f.err != nil {
		return nil, f.err
	}
	return &servicesSDK.ListResponse{Services: []servicesSDK.Record{{Name: "web", State: "running", Revision: 3}}}, nil
}

func withFakeServices(t *testing.T, fake *fakeServicesClient) {
	t.Helper()
	origFactory, origTimeout, origOutput, origStdin := servicesClientFactory, globalTimeout, servicesOutputFmt, servicesStdin
	servicesClientFactory = func() (servicesSDK.Client, error) { return fake, nil }
	globalTimeout = 5 * time.Second
	t.Cleanup(func() {
		servicesClientFactory, globalTimeout, servicesOutputFmt, servicesStdin = origFactory, origTimeout, origOutput, origStdin
		servicesManifest = ""
	})
}

func runServicesCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newServicesCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func writeSignedManifest(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "web.signed.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const signedManifestJSON = `{"payload":"eyJuYW1lIjoid2ViIn0=","key_id":"k","signature":"AQI="}`

func TestServicesCLI_DeployReadsTheSignedManifest(t *testing.T) {
	fake := &fakeServicesClient{}
	withFakeServices(t, fake)
	out, err := runServicesCLI(t, "deploy", "--manifest", writeSignedManifest(t, signedManifestJSON))
	if err != nil {
		t.Fatal(err)
	}
	if fake.signed == nil || string(fake.signed.Payload) != `{"name":"web"}` || fake.signed.KeyID != "k" || len(fake.signed.Signature) != 2 {
		t.Fatalf("sent %+v", fake.signed)
	}
	for _, want := range []string{"Name:", "web", "running", "certificate=certificate:acme/web, rbac=rbac:acme/web"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output lacks %q:\n%s", want, out)
		}
	}

	servicesStdin = strings.NewReader(signedManifestJSON)
	servicesOutputFmt = "json"
	out, err = runServicesCLI(t, "redeploy", "--manifest", "-", "--output", "json")
	if err != nil {
		t.Fatal(err)
	}
	var rec servicesSDK.Record
	if err := json.Unmarshal([]byte(out), &rec); err != nil || rec.Verb != "redeploy" || rec.Revision != 3 {
		t.Fatalf("json output %q: %+v, %v", out, rec, err)
	}
}

func TestServicesCLI_NamedVerbsAndList(t *testing.T) {
	fake := &fakeServicesClient{}
	withFakeServices(t, fake)
	for _, verb := range []string{"start", "stop", "resume", "restart", "get", "undeploy"} {
		if _, err := runServicesCLI(t, verb, "web"); err != nil {
			t.Fatalf("%s: %v", verb, err)
		}
	}
	out, err := runServicesCLI(t, "list")
	if err != nil || !strings.Contains(out, "NAME") || !strings.Contains(out, "running") {
		t.Fatalf("list = %q, %v", out, err)
	}
	want := []string{"start web", "stop web", "resume web", "restart web", "get web", "undeploy web", "list"}
	if strings.Join(fake.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v", fake.calls, want)
	}
}

func TestServicesCLI_RefusesBadManifestsAndSurfacesErrors(t *testing.T) {
	fake := &fakeServicesClient{}
	withFakeServices(t, fake)
	for _, args := range [][]string{
		{"deploy"},
		{"deploy", "--manifest", filepath.Join(t.TempDir(), "missing.json")},
		{"deploy", "--manifest", writeSignedManifest(t, "not json")},
		{"deploy", "--manifest", writeSignedManifest(t, `{"payload":"eA==","key_id":"k"}`)},
	} {
		if _, err := runServicesCLI(t, args...); err == nil {
			t.Fatalf("%v succeeded", args)
		}
	}
	if len(fake.calls) != 0 {
		t.Fatalf("a refused manifest reached the client: %v", fake.calls)
	}
	if _, err := runServicesCLI(t, "start"); err == nil {
		t.Fatal("start without a name succeeded")
	}
	fake.err = errors.New("service not found")
	if _, err := runServicesCLI(t, "get", "web"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("get error = %v", err)
	}
	var bad *InvalidSignedManifestError
	if _, err := readSignedManifest(""); !errors.As(err, &bad) {
		t.Fatalf("empty path = %v", err)
	}
}
