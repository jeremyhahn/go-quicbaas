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

package apimodule

import (
	"context"
	"sort"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// TestGRPCRegistrations_EveryProjectedMethodDeclaresAPermission walks the
// descriptor of every service RegisterQUIC registers.
// go-qrdb's QUIC server projects each unary method that is not listed as
// public and panics at construction when one carries no (qrdb.v1.permission)
// option; gRPC denies such a method on every call. A new RPC without the
// option must fail here, not at server startup.
func TestGRPCRegistrations_EveryProjectedMethodDeclaresAPermission(t *testing.T) {
	var missing []string
	for _, entry := range grpcRegistrations {
		missing = append(missing, unpermittedMethods(t, entry.desc)...)
	}
	sort.Strings(missing)
	assert.Empty(t, missing, "unary non-public methods without a (qrdb.v1.permission) option")
}

// TestMethodPermission_UndeclaredIsEmpty proves the walk above can fail: a
// method that declares no option reads as the empty permission.
func TestMethodPermission_UndeclaredIsEmpty(t *testing.T) {
	svc := serviceDescriptor(t, &healthpb.Health_ServiceDesc)
	assert.Empty(t, methodPermission(svc.Methods().ByName("Check")))
}

// TestModule_RegisterQUIC_RegistersTheGRPCSurface pins RegisterQUIC to the
// same registration path as RegisterGRPC: every service, every method.
func TestModule_RegisterQUIC_RegistersTheGRPCSurface(t *testing.T) {
	m, err := NewModule(func() service.QuicBaaS { return nil }, testLogger(t))
	require.NoError(t, err)

	viaGRPC := grpc.NewServer()
	m.RegisterGRPC(viaGRPC)
	viaQUIC := grpc.NewServer()
	m.RegisterQUIC(viaQUIC)

	quicSurface := registeredSurface(viaQUIC)
	assert.Len(t, quicSurface, len(grpcRegistrations))
	assert.Equal(t, registeredSurface(viaGRPC), quicSurface)
}

// TestModule_RegisterQUIC_ProjectedCallIsLateBound drives a registered unary
// method the way go-qrdb's QUIC projection does: the frame payload is
// decoded by the generated handler, which is invoked with a nil service and
// a nil interceptor (the frame was authorized before dispatch). Before the
// facade is composed the call answers Unavailable; afterwards the same
// registration serves.
func TestModule_RegisterQUIC_ProjectedCallIsLateBound(t *testing.T) {
	var svc atomic.Pointer[mocks.MockQuicBaaS]
	m, err := NewModule(func() service.QuicBaaS {
		if composed := svc.Load(); composed != nil {
			return composed
		}
		return nil
	}, testLogger(t))
	require.NoError(t, err)

	reg := &capturingRegistrar{}
	m.RegisterQUIC(reg)
	payload, err := proto.Marshal(&pb.GetSchedulerStatusRequest{})
	require.NoError(t, err)

	_, err = reg.callUnary(t, pb.SchedulerService_ServiceDesc.ServiceName, "GetSchedulerStatus", payload)
	assert.Equal(t, codes.Unavailable, status.Code(err),
		"a projected call before composition must answer Unavailable, got %v", err)

	svc.Store(&mocks.MockQuicBaaS{
		SchedulerServiceFunc: func() service.SchedulerService {
			return &mocks.MockSchedulerService{}
		},
	})
	out, err := reg.callUnary(t, pb.SchedulerService_ServiceDesc.ServiceName, "GetSchedulerStatus", payload)
	require.NoError(t, err, "the late-bound facade must serve without re-registration")
	resp, ok := out.(*pb.GetSchedulerStatusResponse)
	require.True(t, ok, "unexpected response type %T", out)
	assert.True(t, resp.Running)
}

// TestCapturingRegistrar_UnknownMethodFails keeps the projection harness
// honest: a method the module did not register is not silently served.
func TestCapturingRegistrar_UnknownMethodFails(t *testing.T) {
	m, err := NewModule(func() service.QuicBaaS { return nil }, testLogger(t))
	require.NoError(t, err)
	reg := &capturingRegistrar{}
	m.RegisterQUIC(reg)

	_, ok := reg.method(pb.SchedulerService_ServiceDesc.ServiceName, "NoSuchMethod")
	assert.False(t, ok)
	_, ok = reg.method("quicbaas.v1.NoSuchService", "GetSchedulerStatus")
	assert.False(t, ok)
}

// capturingRegistrar records the descriptors a module registers, standing in
// for go-qrdb's QUIC registrar, which the SDK does not export.
type capturingRegistrar struct {
	descs []*grpc.ServiceDesc
}

func (r *capturingRegistrar) RegisterService(desc *grpc.ServiceDesc, _ any) {
	r.descs = append(r.descs, desc)
}

func (r *capturingRegistrar) method(serviceName, methodName string) (grpc.MethodDesc, bool) {
	for _, desc := range r.descs {
		if desc.ServiceName != serviceName {
			continue
		}
		for _, md := range desc.Methods {
			if md.MethodName == methodName {
				return md, true
			}
		}
	}
	return grpc.MethodDesc{}, false
}

// callUnary invokes a registered method with QUIC's calling convention.
func (r *capturingRegistrar) callUnary(t *testing.T, serviceName, methodName string, payload []byte) (any, error) {
	t.Helper()
	md, ok := r.method(serviceName, methodName)
	require.True(t, ok, "/%s/%s is not registered", serviceName, methodName)
	dec := func(v any) error {
		msg, ok := v.(proto.Message)
		require.True(t, ok, "request %T is not a proto.Message", v)
		return proto.Unmarshal(payload, msg)
	}
	return md.Handler(nil, context.Background(), dec, nil)
}

// registeredSurface maps each registered service to its sorted method names;
// grpc.Server.GetServiceInfo lists methods in no fixed order.
func registeredSurface(server *grpc.Server) map[string][]string {
	surface := make(map[string][]string)
	for name, info := range server.GetServiceInfo() {
		methods := make([]string, 0, len(info.Methods))
		for _, method := range info.Methods {
			methods = append(methods, method.Name)
		}
		sort.Strings(methods)
		surface[name] = methods
	}
	return surface
}

// unpermittedMethods lists the unary methods of desc that are not public and
// declare no (qrdb.v1.permission) option: the ones go-qrdb's QUIC server
// refuses to project.
func unpermittedMethods(t *testing.T, desc *grpc.ServiceDesc) []string {
	t.Helper()
	public := make(map[string]struct{}, len(publicGRPCMethods))
	for _, method := range publicGRPCMethods {
		public[method] = struct{}{}
	}
	svc := serviceDescriptor(t, desc)
	var missing []string
	for _, md := range desc.Methods {
		fullMethod := "/" + desc.ServiceName + "/" + md.MethodName
		if _, ok := public[fullMethod]; ok {
			continue
		}
		method := svc.Methods().ByName(protoreflect.Name(md.MethodName))
		require.NotNil(t, method, "%s is in the ServiceDesc but not the file descriptor", fullMethod)
		if methodPermission(method) == "" {
			missing = append(missing, fullMethod)
		}
	}
	return missing
}

// serviceDescriptor resolves a generated ServiceDesc to its proto descriptor
// through the file its Metadata names.
func serviceDescriptor(t *testing.T, desc *grpc.ServiceDesc) protoreflect.ServiceDescriptor {
	t.Helper()
	path, ok := desc.Metadata.(string)
	require.True(t, ok, "%s: Metadata must name its proto file", desc.ServiceName)
	fd, err := protoregistry.GlobalFiles.FindFileByPath(path)
	require.NoError(t, err, "%s: proto file %s is not registered", desc.ServiceName, path)
	svc := fd.Services().ByName(protoreflect.FullName(desc.ServiceName).Name())
	require.NotNil(t, svc, "%s is not declared in %s", desc.ServiceName, path)
	return svc
}
