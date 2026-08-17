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

package grpc

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	qrdbv1 "github.com/jeremyhahn/go-qrdb/pkg/api/grpc/proto/qrdbv1"

	// Registers every api/proto file descriptor (all quicbaas.* services)
	// into protoregistry.GlobalFiles so the guards below can enumerate them.
	_ "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// quicbaasProtoPackagePrefix scopes the descriptor walk to the broker's own
// proto packages (quicbaas.v1, quicbaas.kms.v1, ...). The qrdb.v1 composition
// services carry their annotations upstream in go-qrdb, and quicbaas.iam.v1
// lives in the separate api/proto/iam Go package outside the broker
// registration surface (unmapped, so default-deny either way).
const quicbaasProtoPackagePrefix = "quicbaas."

// quicbaasMethodDescriptors walks the protobuf registry — the same reflection
// path qrdb's unified-server authorization interceptor resolves permissions
// through — and invokes fn for every RPC method of every quicbaas.* service.
func quicbaasMethodDescriptors(t *testing.T, fn func(full string, m protoreflect.MethodDescriptor)) {
	t.Helper()
	count := 0
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if !strings.HasPrefix(string(fd.Package()), quicbaasProtoPackagePrefix) {
			return true
		}
		services := fd.Services()
		for i := 0; i < services.Len(); i++ {
			svc := services.Get(i)
			methods := svc.Methods()
			for j := 0; j < methods.Len(); j++ {
				m := methods.Get(j)
				full := "/" + string(svc.FullName()) + "/" + string(m.Name())
				fn(full, m)
				count++
			}
		}
		return true
	})
	require.NotZero(t, count, "no quicbaas.* service descriptors found in the protoregistry")
}

// protoPermissionOption reads the qrdb.v1.permission method option, returning
// ("", false) when the RPC is unannotated.
func protoPermissionOption(t *testing.T, m protoreflect.MethodDescriptor) (string, bool) {
	t.Helper()
	opts, ok := m.Options().(*descriptorpb.MethodOptions)
	if !ok || opts == nil {
		return "", false
	}
	if !proto.HasExtension(opts, qrdbv1.E_Permission) {
		return "", false
	}
	perm, ok := proto.GetExtension(opts, qrdbv1.E_Permission).(string)
	require.True(t, ok, "method %s: qrdb.v1.permission must be a string option", m.FullName())
	return perm, true
}

// TestGRPCPermissions_ProtoOptionMatchesAuthzMapping locks the proto
// option (qrdb.v1.permission) annotations to the authoritative Go PEP table
// (grpcMethodPermissions), in both directions, for every RPC that exists on
// the wire:
//
//   - an annotated RPC must have a table entry whose resource:action join is
//     the SAME string (qrdb's reflection interceptor on the unified server
//     enforces exactly what the legacy interceptor enforces), and
//   - an unannotated RPC must have NO table entry — it is either declared
//     public (publicGRPCMethods, mirroring qrdb's own posture where
//     Health-style RPCs carry no option) or intentionally unmapped and
//     therefore DEFAULT-DENY on both enforcement paths.
//
// The contrapositive of the second bullet also guards the forward direction:
// every table entry that names a real RPC is annotated. The 15 dead vector
// aliases that used to make this an approximate check (defensive entries such
// as the former /quicbaas.vector.v1.VectorDB/Get, which named no real
// descriptor) were deleted in the Wave 4 cutover; every remaining
// quicbaas.* table entry now resolves to a real RPC, so resolvable and
// annotated below are computed over the SAME set for that package prefix.
// The /qrdb.v1.* composition methods (annotated upstream in go-qrdb) are
// still outside this table by design and are excluded by the package-prefix
// filter. On mismatch the failure names the method.
func TestGRPCPermissions_ProtoOptionMatchesAuthzMapping(t *testing.T) {
	annotated := 0
	quicbaasMethodDescriptors(t, func(full string, m protoreflect.MethodDescriptor) {
		perm, hasOption := protoPermissionOption(t, m)
		want, mapped := grpcMethodPermissions[full]
		if hasOption {
			annotated++
			require.True(t, mapped,
				"method %q carries option (qrdb.v1.permission) = %q without a grpcMethodPermissions entry", full, perm)
			assert.Equal(t, want.resource+":"+want.action, perm,
				"method %q: proto option diverges from the authz mapping table", full)
			assert.False(t, publicGRPCMethods[full],
				"method %q is declared public but carries a permission annotation", full)
			return
		}
		assert.False(t, mapped,
			"method %q has a grpcMethodPermissions entry (%s:%s) but no option (qrdb.v1.permission) annotation — it would be DEFAULT-DENY on the unified server",
			full, want.resource, want.action)
	})

	// The annotated wire surface is exactly the resolvable slice of the table.
	resolvable := 0
	for full := range grpcMethodPermissions {
		if !strings.HasPrefix(full, "/"+quicbaasProtoPackagePrefix) {
			continue
		}
		name := strings.TrimPrefix(full, "/")
		name = strings.ReplaceAll(name, "/", ".")
		if _, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(name)); err == nil {
			resolvable++
		}
	}
	assert.Equal(t, resolvable, annotated,
		"annotated RPC count must equal the number of authz mapping entries that resolve to a real descriptor")
}

// TestGRPCPermissions_PublicMethodsCarryNoAnnotation pins the pre-auth
// surface: every quicbaas.* method in publicGRPCMethods must exist on the wire
// and carry NO option (qrdb.v1.permission), mirroring qrdb's own posture where
// public infrastructure RPCs are unannotated. An annotation appearing on a
// public method would silently gate the login/health path behind a token that
// cannot exist yet.
func TestGRPCPermissions_PublicMethodsCarryNoAnnotation(t *testing.T) {
	checked := 0
	for full := range publicGRPCMethods {
		if !strings.HasPrefix(full, "/"+quicbaasProtoPackagePrefix) {
			continue // qrdb.v1 composition surface, annotated upstream
		}
		name := strings.ReplaceAll(strings.TrimPrefix(full, "/"), "/", ".")
		d, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(name))
		require.NoError(t, err, "public method %q must exist in the protoregistry", full)
		m, ok := d.(protoreflect.MethodDescriptor)
		require.True(t, ok, "public entry %q must name a method descriptor", full)
		perm, hasOption := protoPermissionOption(t, m)
		assert.False(t, hasOption,
			"public method %q must not carry option (qrdb.v1.permission), got %q", full, perm)
		checked++
	}
	require.NotZero(t, checked, "no quicbaas.* public methods found — the public register moved?")
}
