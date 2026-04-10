// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package messaging

import (
	"context"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// mockTransport is a mock transport for testing.
type mockTransport struct {
	requestFn       func(ctx context.Context, service, method string, req interface{}) (interface{}, error)
	requestStreamFn func(ctx context.Context, service, method string, req interface{}) (transport.Stream, error)
}

func (m *mockTransport) Request(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	if m.requestFn != nil {
		return m.requestFn(ctx, service, method, req)
	}
	return nil, nil
}

func (m *mockTransport) RequestStream(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
	if m.requestStreamFn != nil {
		return m.requestStreamFn(ctx, service, method, req)
	}
	return nil, nil
}

func (m *mockTransport) Conn() interface{} {
	return nil
}

func (m *mockTransport) Close() error {
	return nil
}

func (m *mockTransport) Healthy() bool {
	return true
}

func TestNewClient(t *testing.T) {
	t.Run("with valid transport", func(t *testing.T) {
		client := NewClient(&mockTransport{})
		if client == nil {
			t.Error("expected non-nil client")
		}
	})

	t.Run("with nil transport", func(t *testing.T) {
		client := NewClient(nil)
		if client != nil {
			t.Error("expected nil client for nil transport")
		}
	})
}

func TestClient_CreateChannel_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &Channel{Name: "test-channel", CreatedAt: time.Now()}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *CreateChannelRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty name",
			req:     &CreateChannelRequest{},
			wantErr: true,
		},
		{
			name:    "name too long",
			req:     &CreateChannelRequest{Name: string(make([]byte, 300))},
			wantErr: true,
		},
		{
			name:    "negative buffer size",
			req:     &CreateChannelRequest{Name: "test", BufferSize: -1},
			wantErr: true,
		},
		{
			name:    "negative max history",
			req:     &CreateChannelRequest{Name: "test", MaxHistory: -1},
			wantErr: true,
		},
		{
			name:    "negative max subscribers",
			req:     &CreateChannelRequest{Name: "test", MaxSubs: -1},
			wantErr: true,
		},
		{
			name:    "negative TTL",
			req:     &CreateChannelRequest{Name: "test", TTL: -time.Second},
			wantErr: true,
		},
		{
			name:    "invalid codec",
			req:     &CreateChannelRequest{Name: "test", Codec: "invalid"},
			wantErr: true,
		},
		{
			name:    "valid minimal request",
			req:     &CreateChannelRequest{Name: "test-channel"},
			wantErr: false,
		},
		{
			name: "valid full request",
			req: &CreateChannelRequest{
				Name:       "test-channel",
				Codec:      CodecJSON,
				Persistent: true,
				MaxHistory: 100,
				BufferSize: 256,
				MaxSubs:    1000,
				TTL:        time.Hour,
			},
			wantErr: false,
		},
		{
			name:    "valid msgpack codec",
			req:     &CreateChannelRequest{Name: "test", Codec: CodecMsgpack},
			wantErr: false,
		},
		{
			name:    "valid protobuf codec",
			req:     &CreateChannelRequest{Name: "test", Codec: CodecProtobuf},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.CreateChannel(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateChannel() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_CreateChannel(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful creation",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if service != serviceName {
						t.Errorf("expected service %q, got %q", serviceName, service)
					}
					if method != "CreateChannel" {
						t.Errorf("expected method %q, got %q", "CreateChannel", method)
					}
					return &Channel{
						Name:        "test-channel",
						Codec:       CodecJSON,
						CreatedAt:   now,
						Subscribers: 0,
						Persistent:  true,
					}, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "nil context",
			ctx:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "transport error",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewUnavailable("test", "server unavailable")
				},
			},
			wantErr: true,
			errCode: errors.CodeUnavailable,
		},
		{
			name: "nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
		{
			name: "invalid response type",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return "invalid", nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	validReq := &CreateChannelRequest{Name: "test-channel"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.CreateChannel(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateChannel() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

func TestClient_DeleteChannel(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		channel   string
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name:    "successful delete",
			ctx:     context.Background(),
			channel: "test-channel",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "DeleteChannel" {
						t.Errorf("expected method %q, got %q", "DeleteChannel", method)
					}
					return &deleteChannelResponse{}, nil
				},
			},
			wantErr: false,
		},
		{
			name:    "successful delete with nil response",
			ctx:     context.Background(),
			channel: "test-channel",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "empty channel name",
			ctx:       context.Background(),
			channel:   "",
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "nil context",
			ctx:       nil,
			channel:   "test-channel",
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:    "channel not found",
			ctx:     context.Background(),
			channel: "nonexistent",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewNotFound("test", "channel not found")
				},
			},
			wantErr: true,
			errCode: errors.CodeNotFound,
		},
		{
			name:    "invalid response type",
			ctx:     context.Background(),
			channel: "test-channel",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return 123, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			err := client.DeleteChannel(tt.ctx, tt.channel)
			if (err != nil) != tt.wantErr {
				t.Errorf("DeleteChannel() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
		})
	}
}

func TestClient_GetChannel(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name      string
		ctx       context.Context
		channel   string
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name:    "successful get",
			ctx:     context.Background(),
			channel: "test-channel",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "GetChannel" {
						t.Errorf("expected method %q, got %q", "GetChannel", method)
					}
					return &Channel{
						Name:        "test-channel",
						Codec:       CodecJSON,
						CreatedAt:   now,
						Subscribers: 10,
						Persistent:  true,
						MaxHistory:  100,
					}, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "empty channel name",
			ctx:       context.Background(),
			channel:   "",
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "nil context",
			ctx:       nil,
			channel:   "test-channel",
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:    "channel not found",
			ctx:     context.Background(),
			channel: "nonexistent",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeNotFound,
		},
		{
			name:    "invalid response type",
			ctx:     context.Background(),
			channel: "test-channel",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return "invalid", nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.GetChannel(tt.ctx, tt.channel)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetChannel() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

func TestClient_ListChannels(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name         string
		ctx          context.Context
		transport    *mockTransport
		wantErr      bool
		errCode      errors.ErrorCode
		channelCount int
	}{
		{
			name: "successful list",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "ListChannels" {
						t.Errorf("expected method %q, got %q", "ListChannels", method)
					}
					return &ListChannelsResponse{
						Channels: []*Channel{
							{Name: "channel-1", CreatedAt: now},
							{Name: "channel-2", CreatedAt: now},
						},
					}, nil
				},
			},
			wantErr:      false,
			channelCount: 2,
		},
		{
			name: "empty list with nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr:      false,
			channelCount: 0,
		},
		{
			name:      "nil context",
			ctx:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "invalid response type",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return 123, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.ListChannels(tt.ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("ListChannels() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && len(resp.Channels) != tt.channelCount {
				t.Errorf("expected %d channels, got %d", tt.channelCount, len(resp.Channels))
			}
		})
	}
}

func TestClient_Subscribe_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &Subscription{ID: "sub-1", Channel: "test-channel", CreatedAt: time.Now()}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *SubscribeRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty channel",
			req:     &SubscribeRequest{},
			wantErr: true,
		},
		{
			name:    "negative buffer size",
			req:     &SubscribeRequest{Channel: "test", BufferSize: -1},
			wantErr: true,
		},
		{
			name:    "valid minimal request",
			req:     &SubscribeRequest{Channel: "test-channel"},
			wantErr: false,
		},
		{
			name: "valid full request",
			req: &SubscribeRequest{
				Channel:             "test-channel",
				SessionID:           "session-123",
				DeltaMode:           true,
				IncludeOldValues:    true,
				DedupMetadata:       true,
				DedupSkipDuplicates: true,
				BufferSize:          256,
				Filter: &SubscriptionFilter{
					EventTypes: []EventType{EventTypePut},
					KeyPrefix:  "users/",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Subscribe(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Subscribe() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_Subscribe(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful subscribe",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "Subscribe" {
						t.Errorf("expected method %q, got %q", "Subscribe", method)
					}
					return &Subscription{
						ID:        "sub-123",
						Channel:   "test-channel",
						ClientID:  "client-456",
						CreatedAt: now,
					}, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "nil context",
			ctx:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
		{
			name: "invalid response type",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return "invalid", nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	validReq := &SubscribeRequest{Channel: "test-channel"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.Subscribe(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("Subscribe() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

func TestClient_Unsubscribe(t *testing.T) {
	tests := []struct {
		name           string
		ctx            context.Context
		subscriptionID string
		transport      *mockTransport
		wantErr        bool
		errCode        errors.ErrorCode
	}{
		{
			name:           "successful unsubscribe",
			ctx:            context.Background(),
			subscriptionID: "sub-123",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "Unsubscribe" {
						t.Errorf("expected method %q, got %q", "Unsubscribe", method)
					}
					return &unsubscribeResponse{}, nil
				},
			},
			wantErr: false,
		},
		{
			name:           "successful unsubscribe with nil response",
			ctx:            context.Background(),
			subscriptionID: "sub-123",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: false,
		},
		{
			name:           "empty subscription ID",
			ctx:            context.Background(),
			subscriptionID: "",
			transport:      &mockTransport{},
			wantErr:        true,
			errCode:        errors.CodeInvalidInput,
		},
		{
			name:           "nil context",
			ctx:            nil,
			subscriptionID: "sub-123",
			transport:      &mockTransport{},
			wantErr:        true,
			errCode:        errors.CodeInvalidInput,
		},
		{
			name:           "subscription not found",
			ctx:            context.Background(),
			subscriptionID: "nonexistent",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewNotFound("test", "subscription not found")
				},
			},
			wantErr: true,
			errCode: errors.CodeNotFound,
		},
		{
			name:           "invalid response type",
			ctx:            context.Background(),
			subscriptionID: "sub-123",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return 123, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			err := client.Unsubscribe(tt.ctx, tt.subscriptionID)
			if (err != nil) != tt.wantErr {
				t.Errorf("Unsubscribe() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
		})
	}
}

func TestClient_Publish_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &publishResponse{}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *PublishRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty channel",
			req:     &PublishRequest{},
			wantErr: true,
		},
		{
			name:    "invalid codec",
			req:     &PublishRequest{Channel: "test", Codec: "invalid"},
			wantErr: true,
		},
		{
			name:    "valid minimal request",
			req:     &PublishRequest{Channel: "test-channel"},
			wantErr: false,
		},
		{
			name: "valid full request",
			req: &PublishRequest{
				Channel:   "test-channel",
				Payload:   []byte(`{"key": "value"}`),
				MessageID: "msg-123",
				DedupKey:  "dedup-key",
				Codec:     CodecJSON,
				Metadata:  map[string]string{"key": "value"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.Publish(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Publish() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_Publish(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful publish",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "Publish" {
						t.Errorf("expected method %q, got %q", "Publish", method)
					}
					return &publishResponse{}, nil
				},
			},
			wantErr: false,
		},
		{
			name: "successful publish with nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "nil context",
			ctx:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "transport error",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewUnavailable("test", "server unavailable")
				},
			},
			wantErr: true,
			errCode: errors.CodeUnavailable,
		},
		{
			name: "invalid response type",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return 123, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	validReq := &PublishRequest{Channel: "test-channel", Payload: []byte(`{"key": "value"}`)}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			err := client.Publish(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("Publish() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
		})
	}
}

func TestClient_PublishDelta_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &publishDeltaResponse{}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *PublishDeltaRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty channel",
			req:     &PublishDeltaRequest{},
			wantErr: true,
		},
		{
			name:    "invalid codec",
			req:     &PublishDeltaRequest{Channel: "test", Codec: "invalid"},
			wantErr: true,
		},
		{
			name:    "valid minimal request",
			req:     &PublishDeltaRequest{Channel: "test-channel"},
			wantErr: false,
		},
		{
			name: "valid full request",
			req: &PublishDeltaRequest{
				Channel:   "test-channel",
				Old:       map[string]interface{}{"name": "old"},
				New:       map[string]interface{}{"name": "new"},
				MessageID: "msg-123",
				DedupKey:  "dedup-key",
				Codec:     CodecJSON,
				Metadata:  map[string]string{"key": "value"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.PublishDelta(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("PublishDelta() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_PublishDelta(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful publish delta",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "PublishDelta" {
						t.Errorf("expected method %q, got %q", "PublishDelta", method)
					}
					return &publishDeltaResponse{}, nil
				},
			},
			wantErr: false,
		},
		{
			name: "successful publish delta with nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "nil context",
			ctx:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "invalid response type",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return 123, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	validReq := &PublishDeltaRequest{
		Channel: "test-channel",
		Old:     map[string]interface{}{"name": "old"},
		New:     map[string]interface{}{"name": "new"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			err := client.PublishDelta(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("PublishDelta() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
		})
	}
}

func TestClient_WatchChannel_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &Subscription{ID: "sub-1", Channel: "watch:users/", CreatedAt: time.Now()}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *WatchChannelRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty key prefix",
			req:     &WatchChannelRequest{},
			wantErr: true,
		},
		{
			name:    "negative buffer size",
			req:     &WatchChannelRequest{KeyPrefix: "users/", BufferSize: -1},
			wantErr: true,
		},
		{
			name:    "valid minimal request",
			req:     &WatchChannelRequest{KeyPrefix: "users/"},
			wantErr: false,
		},
		{
			name: "valid full request",
			req: &WatchChannelRequest{
				KeyPrefix:           "users/",
				SessionID:           "session-123",
				DeltaMode:           true,
				IncludeOldValues:    true,
				DedupMetadata:       true,
				DedupSkipDuplicates: true,
				BufferSize:          256,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.WatchChannel(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("WatchChannel() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_WatchChannel(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful watch",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "WatchChannel" {
						t.Errorf("expected method %q, got %q", "WatchChannel", method)
					}
					return &Subscription{
						ID:        "sub-123",
						Channel:   "watch:users/",
						ClientID:  "client-456",
						CreatedAt: now,
					}, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "nil context",
			ctx:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
		{
			name: "invalid response type",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return "invalid", nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	validReq := &WatchChannelRequest{KeyPrefix: "users/"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.WatchChannel(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("WatchChannel() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

func TestClient_GetStats(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful get stats",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "GetStats" {
						t.Errorf("expected method %q, got %q", "GetStats", method)
					}
					return &Stats{
						TotalChannels:      10,
						TotalSubscriptions: 100,
						MessagesPublished:  1000,
						MessagesDelivered:  950,
						DedupHits:          50,
						DroppedMessages:    5,
						BufferUtilization:  0.45,
					}, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "nil context",
			ctx:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
		{
			name: "invalid response type",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return "invalid", nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.GetStats(tt.ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetStats() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

func TestClient_GetDedupStats(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful get dedup stats",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "GetDedupStats" {
						t.Errorf("expected method %q, got %q", "GetDedupStats", method)
					}
					return &DedupStats{
						TotalChecks:        1000,
						DuplicatesDetected: 50,
						UniqueMessages:     950,
						CacheSize:          500,
						WindowDuration:     5 * time.Minute,
					}, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "nil context",
			ctx:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
		{
			name: "invalid response type",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return "invalid", nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.GetDedupStats(tt.ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetDedupStats() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

func TestClient_GetChannelStats(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		channel   string
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name:    "successful get channel stats",
			ctx:     context.Background(),
			channel: "test-channel",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "GetChannelStats" {
						t.Errorf("expected method %q, got %q", "GetChannelStats", method)
					}
					return &ChannelStats{
						Name:              "test-channel",
						Subscribers:       100,
						MessagesPublished: 1000,
						MessagesInHistory: 50,
					}, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "empty channel name",
			ctx:       context.Background(),
			channel:   "",
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "nil context",
			ctx:       nil,
			channel:   "test-channel",
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:    "channel not found",
			ctx:     context.Background(),
			channel: "nonexistent",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeNotFound,
		},
		{
			name:    "invalid response type",
			ctx:     context.Background(),
			channel: "test-channel",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return "invalid", nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.GetChannelStats(tt.ctx, tt.channel)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetChannelStats() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

func TestEventType_String(t *testing.T) {
	tests := []struct {
		eventType EventType
		expected  string
	}{
		{EventTypePut, "PUT"},
		{EventTypeDelete, "DELETE"},
		{EventType(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.eventType.String(); got != tt.expected {
				t.Errorf("String() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

func TestOpType_String(t *testing.T) {
	tests := []struct {
		opType   OpType
		expected string
	}{
		{OpSet, "set"},
		{OpDelete, "delete"},
		{OpAppend, "append"},
		{OpIncrement, "increment"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.opType.String(); got != tt.expected {
				t.Errorf("String() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

func TestChannel_Fields(t *testing.T) {
	now := time.Now()
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &Channel{
				Name:        "test-channel",
				Codec:       CodecJSON,
				CreatedAt:   now,
				Subscribers: 100,
				Persistent:  true,
				MaxHistory:  1000,
				BufferSize:  256,
				TTL:         time.Hour,
			}, nil
		},
	}

	client := NewClient(mockT)
	channel, err := client.GetChannel(context.Background(), "test-channel")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if channel.Name != "test-channel" {
		t.Errorf("expected Name='test-channel', got %q", channel.Name)
	}
	if channel.Codec != CodecJSON {
		t.Errorf("expected Codec='json', got %q", channel.Codec)
	}
	if channel.Subscribers != 100 {
		t.Errorf("expected Subscribers=100, got %d", channel.Subscribers)
	}
	if !channel.Persistent {
		t.Error("expected Persistent=true")
	}
	if channel.MaxHistory != 1000 {
		t.Errorf("expected MaxHistory=1000, got %d", channel.MaxHistory)
	}
	if channel.BufferSize != 256 {
		t.Errorf("expected BufferSize=256, got %d", channel.BufferSize)
	}
	if channel.TTL != time.Hour {
		t.Errorf("expected TTL=1h, got %v", channel.TTL)
	}
}

func TestSubscription_Fields(t *testing.T) {
	now := time.Now()
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &Subscription{
				ID:        "sub-123",
				Channel:   "test-channel",
				ClientID:  "client-456",
				CreatedAt: now,
				Filters: &SubscriptionFilter{
					EventTypes: []EventType{EventTypePut, EventTypeDelete},
					KeyPrefix:  "users/",
				},
			}, nil
		},
	}

	client := NewClient(mockT)
	sub, err := client.Subscribe(context.Background(), &SubscribeRequest{Channel: "test-channel"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sub.ID != "sub-123" {
		t.Errorf("expected ID='sub-123', got %q", sub.ID)
	}
	if sub.Channel != "test-channel" {
		t.Errorf("expected Channel='test-channel', got %q", sub.Channel)
	}
	if sub.ClientID != "client-456" {
		t.Errorf("expected ClientID='client-456', got %q", sub.ClientID)
	}
	if sub.Filters == nil {
		t.Fatal("expected non-nil Filters")
	}
	if len(sub.Filters.EventTypes) != 2 {
		t.Errorf("expected 2 EventTypes, got %d", len(sub.Filters.EventTypes))
	}
	if sub.Filters.KeyPrefix != "users/" {
		t.Errorf("expected KeyPrefix='users/', got %q", sub.Filters.KeyPrefix)
	}
}

func TestStats_Fields(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &Stats{
				TotalChannels:      10,
				TotalSubscriptions: 100,
				MessagesPublished:  10000,
				MessagesDelivered:  9500,
				DedupHits:          500,
				DroppedMessages:    50,
				BufferUtilization:  0.75,
			}, nil
		},
	}

	client := NewClient(mockT)
	stats, err := client.GetStats(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stats.TotalChannels != 10 {
		t.Errorf("expected TotalChannels=10, got %d", stats.TotalChannels)
	}
	if stats.TotalSubscriptions != 100 {
		t.Errorf("expected TotalSubscriptions=100, got %d", stats.TotalSubscriptions)
	}
	if stats.MessagesPublished != 10000 {
		t.Errorf("expected MessagesPublished=10000, got %d", stats.MessagesPublished)
	}
	if stats.MessagesDelivered != 9500 {
		t.Errorf("expected MessagesDelivered=9500, got %d", stats.MessagesDelivered)
	}
	if stats.DedupHits != 500 {
		t.Errorf("expected DedupHits=500, got %d", stats.DedupHits)
	}
	if stats.DroppedMessages != 50 {
		t.Errorf("expected DroppedMessages=50, got %d", stats.DroppedMessages)
	}
	if stats.BufferUtilization != 0.75 {
		t.Errorf("expected BufferUtilization=0.75, got %f", stats.BufferUtilization)
	}
}

func TestTransportErrorPropagation(t *testing.T) {
	transportErr := errors.NewConnection("test", "connection refused")
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, transportErr
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	t.Run("CreateChannel", func(t *testing.T) {
		_, err := client.CreateChannel(ctx, &CreateChannelRequest{Name: "test"})
		if err == nil {
			t.Error("expected error")
		}
		if errors.Code(err) != errors.CodeConnection {
			t.Errorf("expected CodeConnection, got %v", errors.Code(err))
		}
	})

	t.Run("DeleteChannel", func(t *testing.T) {
		err := client.DeleteChannel(ctx, "test")
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("GetChannel", func(t *testing.T) {
		_, err := client.GetChannel(ctx, "test")
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("ListChannels", func(t *testing.T) {
		_, err := client.ListChannels(ctx)
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("Subscribe", func(t *testing.T) {
		_, err := client.Subscribe(ctx, &SubscribeRequest{Channel: "test"})
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("Unsubscribe", func(t *testing.T) {
		err := client.Unsubscribe(ctx, "sub-123")
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("Publish", func(t *testing.T) {
		err := client.Publish(ctx, &PublishRequest{Channel: "test"})
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("PublishDelta", func(t *testing.T) {
		err := client.PublishDelta(ctx, &PublishDeltaRequest{Channel: "test"})
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("WatchChannel", func(t *testing.T) {
		_, err := client.WatchChannel(ctx, &WatchChannelRequest{KeyPrefix: "test/"})
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("GetStats", func(t *testing.T) {
		_, err := client.GetStats(ctx)
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("GetDedupStats", func(t *testing.T) {
		_, err := client.GetDedupStats(ctx)
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("GetChannelStats", func(t *testing.T) {
		_, err := client.GetChannelStats(ctx, "test")
		if err == nil {
			t.Error("expected error")
		}
	})
}

// DeltaApplier Tests

func TestDeltaApplier_Apply(t *testing.T) {
	type User struct {
		Name    string   `json:"name"`
		Age     int      `json:"age"`
		Email   string   `json:"email,omitempty"`
		Counter int      `json:"counter"`
		Tags    []string `json:"tags,omitempty"`
	}

	applier := NewDeltaApplier[User]()

	t.Run("nil delta", func(t *testing.T) {
		user := User{Name: "John", Age: 30}
		result, err := applier.Apply(user, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Name != "John" || result.Age != 30 {
			t.Error("expected unchanged user")
		}
	})

	t.Run("empty changes", func(t *testing.T) {
		user := User{Name: "John", Age: 30}
		result, err := applier.Apply(user, &DeltaPayload{Changes: []FieldChange{}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Name != "John" || result.Age != 30 {
			t.Error("expected unchanged user")
		}
	})

	t.Run("set operation", func(t *testing.T) {
		user := User{Name: "John", Age: 30}
		delta := &DeltaPayload{
			Changes: []FieldChange{
				{Path: "name", Operation: OpSet, Value: "Jane"},
				{Path: "age", Operation: OpSet, Value: float64(25)},
			},
		}
		result, err := applier.Apply(user, delta)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Name != "Jane" {
			t.Errorf("expected Name='Jane', got %q", result.Name)
		}
		if result.Age != 25 {
			t.Errorf("expected Age=25, got %d", result.Age)
		}
	})

	t.Run("delete operation", func(t *testing.T) {
		user := User{Name: "John", Age: 30, Email: "john@example.com"}
		delta := &DeltaPayload{
			Changes: []FieldChange{
				{Path: "email", Operation: OpDelete},
			},
		}
		result, err := applier.Apply(user, delta)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Email != "" {
			t.Errorf("expected empty Email, got %q", result.Email)
		}
	})

	t.Run("increment operation", func(t *testing.T) {
		user := User{Name: "John", Counter: 10}
		delta := &DeltaPayload{
			Changes: []FieldChange{
				{Path: "counter", Operation: OpIncrement, Value: float64(5)},
			},
		}
		result, err := applier.Apply(user, delta)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Counter != 15 {
			t.Errorf("expected Counter=15, got %d", result.Counter)
		}
	})

	t.Run("empty path error", func(t *testing.T) {
		user := User{Name: "John"}
		delta := &DeltaPayload{
			Changes: []FieldChange{
				{Path: "", Operation: OpSet, Value: "test"},
			},
		}
		_, err := applier.Apply(user, delta)
		if err == nil {
			t.Error("expected error for empty path")
		}
	})
}

func TestDeltaApplier_ApplyChanges(t *testing.T) {
	type Config struct {
		Debug bool `json:"debug"`
		Level int  `json:"level"`
	}

	applier := NewDeltaApplier[Config]()

	config := Config{Debug: false, Level: 1}
	changes := []FieldChange{
		{Path: "debug", Operation: OpSet, Value: true},
		{Path: "level", Operation: OpSet, Value: float64(5)},
	}

	result, err := applier.ApplyChanges(config, changes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Debug {
		t.Error("expected Debug=true")
	}
	if result.Level != 5 {
		t.Errorf("expected Level=5, got %d", result.Level)
	}
}

func TestDeltaBuilder(t *testing.T) {
	builder := NewDeltaBuilder()

	delta := builder.
		WithEntityID("user-123").
		WithEntityType("User").
		WithVersion(1, 2).
		Set("name", "John").
		SetWithOld("age", 30, 31).
		Delete("temp_field").
		DeleteWithOld("old_field", "old_value").
		Append("tags", "new-tag").
		Increment("counter", 1).
		Build()

	if delta.EntityID != "user-123" {
		t.Errorf("expected EntityID='user-123', got %q", delta.EntityID)
	}
	if delta.EntityType != "User" {
		t.Errorf("expected EntityType='User', got %q", delta.EntityType)
	}
	if delta.PrevVersion != 1 {
		t.Errorf("expected PrevVersion=1, got %d", delta.PrevVersion)
	}
	if delta.Version != 2 {
		t.Errorf("expected Version=2, got %d", delta.Version)
	}
	if len(delta.Changes) != 6 {
		t.Errorf("expected 6 changes, got %d", len(delta.Changes))
	}

	// Verify individual changes
	if delta.Changes[0].Operation != OpSet || delta.Changes[0].Path != "name" {
		t.Error("first change should be Set on 'name'")
	}
	if delta.Changes[1].OldValue != 30 {
		t.Error("second change should have OldValue=30")
	}
	if delta.Changes[2].Operation != OpDelete {
		t.Error("third change should be Delete")
	}
	if delta.Changes[3].OldValue != "old_value" {
		t.Error("fourth change should have OldValue='old_value'")
	}
	if delta.Changes[4].Operation != OpAppend {
		t.Error("fifth change should be Append")
	}
	if delta.Changes[5].Operation != OpIncrement {
		t.Error("sixth change should be Increment")
	}
}

func TestDeltaApplier_NestedPath(t *testing.T) {
	type Address struct {
		City string `json:"city"`
		Zip  string `json:"zip"`
	}

	type Person struct {
		Name    string  `json:"name"`
		Address Address `json:"address"`
	}

	applier := NewDeltaApplier[Person]()

	person := Person{
		Name:    "John",
		Address: Address{City: "NYC", Zip: "10001"},
	}

	delta := &DeltaPayload{
		Changes: []FieldChange{
			{Path: "address.city", Operation: OpSet, Value: "LA"},
		},
	}

	result, err := applier.Apply(person, delta)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Address.City != "LA" {
		t.Errorf("expected City='LA', got %q", result.Address.City)
	}
	if result.Address.Zip != "10001" {
		t.Errorf("expected Zip='10001', got %q", result.Address.Zip)
	}
}

func TestDeltaApplier_AppendToArray(t *testing.T) {
	type Item struct {
		Tags []string `json:"tags"`
	}

	applier := NewDeltaApplier[Item]()

	t.Run("append to existing array", func(t *testing.T) {
		item := Item{Tags: []string{"a", "b"}}
		delta := &DeltaPayload{
			Changes: []FieldChange{
				{Path: "tags", Operation: OpAppend, Value: "c"},
			},
		}
		result, err := applier.Apply(item, delta)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Tags) != 3 || result.Tags[2] != "c" {
			t.Errorf("expected Tags=['a','b','c'], got %v", result.Tags)
		}
	})

	t.Run("append to nil array", func(t *testing.T) {
		item := Item{Tags: nil}
		delta := &DeltaPayload{
			Changes: []FieldChange{
				{Path: "tags", Operation: OpAppend, Value: "first"},
			},
		}
		result, err := applier.Apply(item, delta)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Tags) != 1 || result.Tags[0] != "first" {
			t.Errorf("expected Tags=['first'], got %v", result.Tags)
		}
	})
}

func TestDeltaApplier_IncrementNonexistent(t *testing.T) {
	type Counter struct {
		Value int `json:"value"`
	}

	applier := NewDeltaApplier[Counter]()

	counter := Counter{}
	delta := &DeltaPayload{
		Changes: []FieldChange{
			{Path: "value", Operation: OpIncrement, Value: float64(10)},
		},
	}
	result, err := applier.Apply(counter, delta)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Value != 10 {
		t.Errorf("expected Value=10, got %d", result.Value)
	}
}

func TestDeltaApplier_InvalidOperations(t *testing.T) {
	type Data struct {
		Field string `json:"field"`
	}

	applier := NewDeltaApplier[Data]()
	data := Data{Field: "test"}

	t.Run("unknown operation", func(t *testing.T) {
		delta := &DeltaPayload{
			Changes: []FieldChange{
				{Path: "field", Operation: OpType("unknown"), Value: "value"},
			},
		}
		_, err := applier.Apply(data, delta)
		if err == nil {
			t.Error("expected error for unknown operation")
		}
	})

	t.Run("increment non-numeric", func(t *testing.T) {
		delta := &DeltaPayload{
			Changes: []FieldChange{
				{Path: "field", Operation: OpIncrement, Value: float64(1)},
			},
		}
		_, err := applier.Apply(data, delta)
		if err == nil {
			t.Error("expected error for incrementing non-numeric field")
		}
	})

	t.Run("increment with non-numeric value", func(t *testing.T) {
		delta := &DeltaPayload{
			Changes: []FieldChange{
				{Path: "field", Operation: OpIncrement, Value: "not a number"},
			},
		}
		_, err := applier.Apply(data, delta)
		if err == nil {
			t.Error("expected error for non-numeric increment value")
		}
	})
}
