// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package server

import (
	"testing"

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
	if s.GetQRDBIndexServicer() != nil {
		t.Error("GetQRDBIndexServicer should be nil without a node")
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
