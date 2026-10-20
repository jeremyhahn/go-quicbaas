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

package objecttransfer

import (
	"context"
	stderrors "errors"
	"io"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	serviceName = "quicbaas.ObjectTransfer"
)

// Client provides object transfer operations.
type Client interface {
	// FetchObject retrieves a single object from a shard.
	FetchObject(ctx context.Context, req *FetchObjectRequest) (*FetchObjectResponse, error)

	// StreamFetchObjects retrieves multiple objects from a shard via streaming.
	// Returns a channel that receives responses for each requested object.
	// The channel is closed when all objects have been fetched or an error occurs.
	StreamFetchObjects(ctx context.Context, req *StreamFetchObjectsRequest) (<-chan *FetchObjectResponse, error)
}

// client implements the Client interface.
type client struct {
	transport transport.Transport
}

// NewClient creates a new Object Transfer client.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{
		transport: t,
	}
}

// =============================================================================
// Fetch Object Operations
// =============================================================================

// FetchObject retrieves a single object from a shard.
func (c *client) FetchObject(ctx context.Context, req *FetchObjectRequest) (*FetchObjectResponse, error) {
	if err := validateFetchObjectRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "FetchObject", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &FetchObjectResponse{
			ObjectRef: req.ObjectRef,
			Found:     false,
		}, nil
	}

	fetchResp, ok := resp.(*FetchObjectResponse)
	if !ok {
		return nil, errors.NewInternal("objecttransfer.FetchObject", "invalid response type")
	}

	return fetchResp, nil
}

// StreamFetchObjects retrieves multiple objects from a shard via streaming.
func (c *client) StreamFetchObjects(ctx context.Context, req *StreamFetchObjectsRequest) (<-chan *FetchObjectResponse, error) {
	if err := validateStreamFetchObjectsRequest(req); err != nil {
		return nil, err
	}

	// Create the streaming request.
	stream, err := c.transport.RequestStream(ctx, serviceName, "StreamFetchObjects", req)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeConnection, "objecttransfer.StreamFetchObjects", "failed to create stream")
	}

	// Create buffered response channel.
	responseChan := make(chan *FetchObjectResponse, len(req.Items))

	// Start goroutine to receive responses.
	go c.receiveStreamResponses(ctx, stream, responseChan)

	return responseChan, nil
}

// receiveStreamResponses receives responses from the stream and sends them to the channel.
func (c *client) receiveStreamResponses(ctx context.Context, stream transport.Stream, responseChan chan<- *FetchObjectResponse) {
	defer close(responseChan)

	failed := c.receiveUntilEnd(ctx, stream, responseChan)

	// Close runs exactly once. Its error is reported only when the stream
	// ended cleanly; an earlier failure already sent is the one that stands.
	closeErr := stream.Close()
	if closeErr != nil && !failed {
		wrapped := errors.Wrap(closeErr, errors.CodeConnection, "objecttransfer.StreamFetchObjects", "failed to close stream")
		responseChan <- &FetchObjectResponse{
			Found:        false,
			ErrorMessage: wrapped.Error(),
		}
	}
}

// receiveUntilEnd forwards stream responses to responseChan until the stream
// ends. It reports whether the stream ended with an error, which it has
// already sent as the final response.
func (c *client) receiveUntilEnd(ctx context.Context, stream transport.Stream, responseChan chan<- *FetchObjectResponse) bool {
	for {
		select {
		case <-ctx.Done():
			// Context canceled, send error response and return.
			responseChan <- &FetchObjectResponse{
				Found:        false,
				ErrorMessage: ctx.Err().Error(),
			}
			return true
		default:
			// Continue receiving.
		}

		resp, err := stream.Recv()
		if err != nil {
			// Check if stream ended normally.
			if isStreamEnded(err) {
				return false
			}
			// Send error response.
			responseChan <- &FetchObjectResponse{
				Found:        false,
				ErrorMessage: err.Error(),
			}
			return true
		}

		if resp == nil {
			// Stream ended.
			return false
		}

		fetchResp, ok := resp.(*FetchObjectResponse)
		if !ok {
			responseChan <- &FetchObjectResponse{
				Found:        false,
				ErrorMessage: "invalid response type from server",
			}
			continue
		}

		responseChan <- fetchResp
	}
}

// isStreamEnded checks if the error indicates the stream has ended normally.
func isStreamEnded(err error) bool {
	return stderrors.Is(err, io.EOF) || stderrors.Is(err, io.ErrClosedPipe)
}

// =============================================================================
// Validation Functions
// =============================================================================

func validateFetchObjectRequest(req *FetchObjectRequest) error {
	if req == nil {
		return errors.NewInvalidInput("objecttransfer.FetchObject", "request is required")
	}
	if req.ShardID == 0 {
		return errors.NewInvalidInput("objecttransfer.FetchObject", "shard ID is required")
	}
	if req.ObjectRef == "" {
		return errors.NewInvalidInput("objecttransfer.FetchObject", "object reference is required")
	}
	return nil
}

func validateStreamFetchObjectsRequest(req *StreamFetchObjectsRequest) error {
	if req == nil {
		return errors.NewInvalidInput("objecttransfer.StreamFetchObjects", "request is required")
	}
	if req.ShardID == 0 {
		return errors.NewInvalidInput("objecttransfer.StreamFetchObjects", "shard ID is required")
	}
	if len(req.Items) == 0 {
		return errors.NewInvalidInput("objecttransfer.StreamFetchObjects", "at least one item is required")
	}
	for i, item := range req.Items {
		if item.ObjectRef == "" {
			return errors.NewInvalidInput("objecttransfer.StreamFetchObjects", "object reference is required for item at index "+indexToString(i))
		}
	}
	return nil
}

// indexToString converts an index to a string representation.
func indexToString(index int) string {
	if index < 10 {
		return string(rune('0' + index))
	}
	// For larger indices, use byte conversion.
	digits := make([]byte, 0, 4)
	for index > 0 {
		digits = append([]byte{byte('0' + index%10)}, digits...)
		index /= 10
	}
	return string(digits)
}
