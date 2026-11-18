// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.

package helpers

import (
	"crypto/tls"
	"testing"
)

func TestQUICTLSConfig_LeavesALPNToTheSDK(t *testing.T) {
	t.Parallel()

	base := &tls.Config{
		MinVersion: tls.VersionTLS13,
		ServerName: "node.example",
		NextProtos: []string{"quicbaas-quic"},
	}

	got := QUICTLSConfig(base)

	if len(got.NextProtos) != 0 {
		t.Fatalf("NextProtos = %v, want empty so the SDK sets its HTTP/3 ALPN", got.NextProtos)
	}
	if got == base {
		t.Fatal("QUICTLSConfig returned base itself, want a clone")
	}
	if got.ServerName != base.ServerName {
		t.Fatalf("ServerName = %q, want %q", got.ServerName, base.ServerName)
	}
	if len(base.NextProtos) != 1 || base.NextProtos[0] != "quicbaas-quic" {
		t.Fatalf("base.NextProtos = %v, want it unchanged", base.NextProtos)
	}
}

func TestQUICTLSConfig_NilBase(t *testing.T) {
	t.Parallel()

	got := QUICTLSConfig(nil)
	if got == nil {
		t.Fatal("QUICTLSConfig(nil) = nil, want a config")
	}
	if len(got.NextProtos) != 0 {
		t.Fatalf("NextProtos = %v, want empty", got.NextProtos)
	}
}
