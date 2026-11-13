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

package embedded

import (
	"context"
	stderrors "errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	objclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/objectstorage"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// streamGetTransport is the embedded Transport with a StreamGetObject stream
// served by handler. The embedded transport registers no StreamGetObject
// handler, so the test supplies one; the stream it returns is a real embedded
// stream driven through runStreamHandler, so it ends exactly the way every
// embedded stream ends: the handler's last value, then Close.
type streamGetTransport struct {
	*Transport
	handler streamHandlerFunc
}

func (t *streamGetTransport) RequestStream(ctx context.Context, svcName, method string, req interface{}) (transport.Stream, error) {
	if svcName != "quicbaas.ObjectStorage" || method != "StreamGetObject" {
		return t.Transport.RequestStream(ctx, svcName, method, req)
	}
	ctx = inProcessTenant(ctx)
	s := newStream(ctx, 100)
	go runStreamHandler(ctx, svcName, method, t.handler, t.service, req, s)
	return s, nil
}

// chunkHandler delivers each payload as an ObjectChunk, marking the final one
// IsLast when markLast is set.
func chunkHandler(payloads []string, markLast bool) streamHandlerFunc {
	return func(_ context.Context, _ service.QuicBaaS, _ interface{}, s *stream) error {
		for i, p := range payloads {
			chunk := &objclient.ObjectChunk{
				Data:   []byte(p),
				IsLast: markLast && i == len(payloads)-1,
			}
			if err := s.deliver(chunk); err != nil {
				return err
			}
		}
		return nil
	}
}

func newStreamGetClient(t *testing.T, handler streamHandlerFunc) objclient.Client {
	t.Helper()
	tr, err := New(&mockQuicBaaS{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tr.Close()) })
	return objclient.NewClient(&streamGetTransport{Transport: tr, handler: handler})
}

func TestEmbedded_StreamGetObject_ReadAllReturnsObject(t *testing.T) {
	tests := []struct {
		name     string
		payloads []string
	}{
		{name: "last chunk carries data", payloads: []string{"hello ", "embedded ", "world"}},
		{name: "empty last chunk", payloads: []string{"hello ", "embedded ", "world", ""}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newStreamGetClient(t, chunkHandler(tt.payloads, true))

			reader, err := client.StreamGetObject(context.Background(), &objclient.GetObjectRequest{Key: "ns:stream/obj"})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, reader.Close()) })

			got, err := io.ReadAll(reader)
			require.NoError(t, err)
			assert.Equal(t, "hello embedded world", string(got))
		})
	}
}

func TestEmbedded_StreamGetObject_TruncatedStreamIsAnError(t *testing.T) {
	// The handler closes the stream without an IsLast chunk: the reader must
	// report the truncation as a connection error, not a clean io.EOF.
	client := newStreamGetClient(t, chunkHandler([]string{"partial"}, false))

	reader, err := client.StreamGetObject(context.Background(), &objclient.GetObjectRequest{Key: "ns:stream/obj"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reader.Close()) })

	got, err := io.ReadAll(reader)
	require.Error(t, err)
	assert.False(t, stderrors.Is(err, io.EOF))
	assert.Equal(t, errors.CodeConnection, errors.Code(err))
	assert.Equal(t, "partial", string(got))
}
