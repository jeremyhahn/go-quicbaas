// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"context"
	"math"
	"testing"

	"google.golang.org/grpc/codes"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
)

// TestMessagingHandlers_ResponseOutOfRange proves that a channel setting too
// large for its int32 wire field becomes codes.Internal on every handler that
// returns a channel, instead of a wrapped integer.
func TestMessagingHandlers_ResponseOutOfRange(t *testing.T) {
	const tooBig = math.MaxInt32 + 1
	ctx := context.Background()
	hugeHistory := &messaging.Channel{Name: "c1", MaxHistory: tooBig}
	hugeBuffer := &messaging.Channel{Name: "c1", BufferSize: tooBig}

	createChannel := func(ch *messaging.Channel) *mockMessagingService {
		return &mockMessagingService{
			createChannelFunc: func(context.Context, string, ...messaging.CreateChannelOption) (*messaging.Channel, error) {
				return ch, nil
			},
		}
	}
	getChannel := func(ch *messaging.Channel) *mockMessagingService {
		return &mockMessagingService{
			getChannelFunc: func(context.Context, string) (*messaging.Channel, error) { return ch, nil },
		}
	}
	listChannels := func(ch *messaging.Channel) *mockMessagingService {
		return &mockMessagingService{
			listChannelsFunc: func(context.Context) ([]*messaging.Channel, error) {
				return []*messaging.Channel{ch}, nil
			},
		}
	}
	callCreate := func(srv *MessagingServer) error {
		_, err := srv.CreateChannel(ctx, &pb.MessagingCreateChannelRequest{Name: "c1"})
		return err
	}
	callGet := func(srv *MessagingServer) error {
		_, err := srv.GetChannel(ctx, &pb.MessagingGetChannelRequest{Name: "c1"})
		return err
	}
	callList := func(srv *MessagingServer) error {
		_, err := srv.ListChannels(ctx, &pb.MessagingListChannelsRequest{})
		return err
	}

	tests := []struct {
		name  string
		field string
		svc   *mockMessagingService
		call  func(srv *MessagingServer) error
	}{
		{name: "CreateChannel max history", field: "max_history", svc: createChannel(hugeHistory), call: callCreate},
		{name: "CreateChannel buffer size", field: "buffer_size", svc: createChannel(hugeBuffer), call: callCreate},
		{name: "GetChannel max history", field: "max_history", svc: getChannel(hugeHistory), call: callGet},
		{name: "GetChannel buffer size", field: "buffer_size", svc: getChannel(hugeBuffer), call: callGet},
		{name: "ListChannels max history", field: "max_history", svc: listChannels(hugeHistory), call: callList},
		{name: "ListChannels buffer size", field: "buffer_size", svc: listChannels(hugeBuffer), call: callList},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireRangeStatus(t, tt.call(newTestMessagingServer(tt.svc)), codes.Internal, tt.field)
		})
	}
}
