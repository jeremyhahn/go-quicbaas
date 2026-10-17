// Copyright 2026 Jeremy Hahn
// SPDX-License-Identifier: Apache-2.0
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
// See the License for the specific language governing permissions and
// limitations under the License.

package grpc

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// qrdbBarrierSealMethod is the canonical qrdb.v1 seal RPC the Go SDK gRPC
// and Unix clients call.
const qrdbBarrierSealMethod = "/qrdb.v1.QRDBService/BarrierSeal"

// soPINRequest is satisfied by the qrdb.v1 BarrierSealRequest message,
// which carries the SO PIN in its so_pin field.
type soPINRequest interface {
	GetSoPin() string
}

// UnaryBarrierSealSOPINInterceptor gates the canonical qrdb.v1 BarrierSeal
// on the Security Officer PIN. The qrdb.v1 registration takes no PIN backend
// through the qrdb SDK, so without this guard gRPC and Unix callers could
// seal without the PIN REST and MCP require.
//
// The PIN is read from the x-qrdb-so-pin metadata entry the SDK clients
// send, falling back to the request's so_pin field, and is verified through
// service.VerifyBarrierSealSOPIN — the same check the BarrierService facade
// makes. Every other method passes through untouched. The PIN is never
// logged.
func UnaryBarrierSealSOPINInterceptor(backend qrdbsdk.PINBackend) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if info.FullMethod != qrdbBarrierSealMethod {
			return handler(ctx, req)
		}
		soPIN := []byte(incomingSealSOPIN(ctx, req))
		err := service.VerifyBarrierSealSOPIN(backend, soPIN)
		secure.Wipe(soPIN)
		if err != nil {
			return nil, mapBarrierError(err)
		}
		return handler(ctx, req)
	}
}

// incomingSealSOPIN returns the SO PIN a seal call presents: the first
// x-qrdb-so-pin metadata value, else the request's so_pin field.
func incomingSealSOPIN(ctx context.Context, req any) string {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if pins := md.Get(qrdbtransport.SOPINMetadataKey); len(pins) > 0 && pins[0] != "" {
			return pins[0]
		}
	}
	if r, ok := req.(soPINRequest); ok {
		return r.GetSoPin()
	}
	return ""
}
