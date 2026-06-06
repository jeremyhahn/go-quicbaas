// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package server

import (
	"testing"
	"time"

	ggrpc "google.golang.org/grpc"
)

// All qrdb-node accessors return nil when no embedded node is present.
func TestServer_QRDBAccessors_NilNode(t *testing.T) {
	s := &Server{}

	if s.GetQRDBNodeClient() != nil {
		t.Error("GetQRDBNodeClient should be nil without a node")
	}
	if s.GetQRDBKVServicer() != nil {
		t.Error("GetQRDBKVServicer should be nil without a node")
	}
	if s.GetQRDBAdminServicer() != nil {
		t.Error("GetQRDBAdminServicer should be nil without a node")
	}
	if s.GetQRDBUserServicer() != nil {
		t.Error("GetQRDBUserServicer should be nil without a node")
	}
	if s.GetQRDBUserAuthFactorServicer() != nil {
		t.Error("GetQRDBUserAuthFactorServicer should be nil without a node")
	}
}

// GetDataPlaneBarrierService returns nil when ceremony services are absent.
func TestServer_GetDataPlaneBarrierService_NilCeremony(t *testing.T) {
	s := &Server{}
	if s.GetDataPlaneBarrierService() != nil {
		t.Error("GetDataPlaneBarrierService should be nil without ceremony services")
	}

	// With ceremony services present but no barrier, still nil.
	s.ceremonyServices = &CeremonyServices{}
	if s.GetDataPlaneBarrierService() != nil {
		t.Error("GetDataPlaneBarrierService should be nil when Barrier unset")
	}
}

// RESTAddr returns nil when the REST server has not been started.
func TestServer_RESTAddr_NilServer(t *testing.T) {
	s := &Server{}
	if s.RESTAddr() != nil {
		t.Error("RESTAddr should be nil without a started REST server")
	}
}

// requestTimeout derives the REST handler timeout from the write timeout,
// clamped into [5s, 25s] with a 5s safety margin.
func TestRequestTimeout(t *testing.T) {
	tests := []struct {
		name  string
		write time.Duration
		want  time.Duration
	}{
		{"zero defaults to max", 0, 25 * time.Second},
		{"negative defaults to max", -1, 25 * time.Second},
		{"below margin clamps to min", 6 * time.Second, 5 * time.Second},
		{"mid range subtracts margin", 20 * time.Second, 15 * time.Second},
		{"above max clamps to max", 60 * time.Second, 25 * time.Second},
		{"exactly margin+min", 10 * time.Second, 5 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := requestTimeout(tt.write); got != tt.want {
				t.Fatalf("requestTimeout(%v) = %v, want %v", tt.write, got, tt.want)
			}
		})
	}
}

// WithStorageConfig records the storage config on the sharded client.
func TestWithStorageConfig_Accessors(t *testing.T) {
	sc := &ShardedClient{}
	opt := WithStorageConfig(nil)
	opt(sc)
	if sc.storageCfg != nil {
		t.Fatalf("expected nil storage config recorded")
	}
}

// registerOllamaService is a no-op in the non-rag build and must not panic.
func TestRegisterOllamaService_NoRAGNoop(t *testing.T) {
	s := &Server{logger: buildTestLogger(t)}
	s.registerOllamaService(ggrpc.NewServer())
}
