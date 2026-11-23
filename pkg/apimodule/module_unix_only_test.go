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
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// fakeServerStream is a grpc.ServerStream whose Context returns ctx.
type fakeServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *fakeServerStream) Context() context.Context { return s.ctx }

func TestUnixPeerOnly_RefusesNonUnixPeer(t *testing.T) {
	var unaryRan, streamRan bool
	desc := &grpc.ServiceDesc{
		ServiceName: "test.v1.Probe",
		HandlerType: (*any)(nil),
		Methods: []grpc.MethodDesc{{
			MethodName: "Call",
			Handler: func(_ any, _ context.Context, _ func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
				unaryRan = true
				return "ok", nil
			},
		}},
		Streams: []grpc.StreamDesc{{
			StreamName: "Watch",
			Handler: func(_ any, _ grpc.ServerStream) error {
				streamRan = true
				return nil
			},
			ServerStreams: true,
		}},
	}
	wrapped := unixPeerOnly(desc)
	require.Len(t, wrapped.Methods, 1)
	require.Len(t, wrapped.Streams, 1)
	assert.Equal(t, desc.ServiceName, wrapped.ServiceName)

	cases := []struct {
		name string
		ctx  context.Context
		runs bool
	}{
		{"tcp peer", peer.NewContext(context.Background(), &peer.Peer{Addr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4433}}), false},
		{"no peer", context.Background(), false},
		{"unix peer", peer.NewContext(context.Background(), &peer.Peer{Addr: &net.UnixAddr{Name: "s", Net: "unix"}}), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			unaryRan, streamRan = false, false

			_, err := wrapped.Methods[0].Handler(nil, tc.ctx, func(any) error { return nil }, nil)
			assert.Equal(t, tc.runs, unaryRan, "unary handler ran")
			if tc.runs {
				assert.NoError(t, err)
			} else {
				assert.Equal(t, codes.Unimplemented, status.Code(err), "unary: %v", err)
			}

			err = wrapped.Streams[0].Handler(nil, &fakeServerStream{ctx: tc.ctx})
			assert.Equal(t, tc.runs, streamRan, "stream handler ran")
			if tc.runs {
				assert.NoError(t, err)
			} else {
				assert.Equal(t, codes.Unimplemented, status.Code(err), "stream: %v", err)
			}
		})
	}
}
