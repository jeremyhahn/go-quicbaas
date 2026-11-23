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

package subprotocol

import (
	"bytes"
	"context"
	"crypto/fips140"
	"encoding/binary"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	ws "github.com/jeremyhahn/go-quicbaas/pkg/api/websocket"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// newGrantedSession is ws.NewSession with a grant that allows every
// operation, for handler tests that exercise publishing.
func newGrantedSession(id, subprotocol string, conn *websocket.Conn) *ws.Session {
	s := ws.NewSession(id, subprotocol, conn)
	s.SetGrant(ws.Grant{
		TenantID:  "test-tenant",
		Authorize: func(context.Context, string, string) error { return nil },
	})
	return s
}

// newDeniedSession is ws.NewSession with a grant that refuses every
// operation.
func newDeniedSession(id, subprotocol string, conn *websocket.Conn) *ws.Session {
	s := ws.NewSession(id, subprotocol, conn)
	s.SetGrant(ws.Grant{
		TenantID: "test-tenant",
		Authorize: func(_ context.Context, resource, action string) error {
			return errors.New("denied " + resource + ":" + action)
		},
	})
	return s
}

// subscribeTestHub registers a hub client subscribed to topic and returns
// the payloads it receives.
func subscribeTestHub(t *testing.T, hub PubSubHub, clientID, topic string) <-chan []byte {
	t.Helper()
	got := make(chan []byte, 16)
	if err := hub.RegisterClient(clientID, func(_ string, payload []byte, _ byte, _ bool) {
		select {
		case got <- payload:
		default:
		}
	}); err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	if err := hub.Subscribe(clientID, topic, 0); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	t.Cleanup(func() { _ = hub.UnregisterClient(clientID) })
	return got
}

func testMessagingLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "debug", Format: "console", Output: "stdout"})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	return log
}

func TestNewMessagingServer_RegistersThreeSubprotocols(t *testing.T) {
	srv, err := NewMessagingServer(testMessagingLogger(t))
	if fips140.Enforced() {
		if err == nil {
			t.Fatal("NewMessagingServer succeeded under FIPS-only mode")
		}
		t.Skip("FIPS-only mode refuses the WebSocket server")
	}
	if err != nil {
		t.Fatalf("NewMessagingServer: %v", err)
	}

	got := srv.GetSubprotocols()
	sort.Strings(got)
	want := []string{"amqp", "mqtt", "stomp"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetSubprotocols() = %v, want %v", got, want)
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// tenantTestServer serves a TenantMessagingServer behind a wrapper that
// grants the tenant named by the request's tenant query parameter (no
// parameter, no grant) and allows every operation.
func tenantTestServer(t *testing.T) (*TenantMessagingServer, string) {
	t.Helper()
	tms, err := NewTenantMessagingServer(testMessagingLogger(t))
	if fips140.Enforced() {
		if err == nil {
			t.Fatal("NewTenantMessagingServer succeeded under FIPS-only mode")
		}
		t.Skip("FIPS-only mode refuses the WebSocket server")
	}
	if err != nil {
		t.Fatalf("NewTenantMessagingServer: %v", err)
	}
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tenant := r.URL.Query().Get("tenant"); tenant != "" {
			authorize := func(context.Context, string, string) error { return nil }
			if r.URL.Query().Get("deny") != "" {
				authorize = func(_ context.Context, resource, action string) error {
					return errors.New("denied " + resource + ":" + action)
				}
			}
			r = r.WithContext(ws.WithGrant(r.Context(), ws.Grant{
				TenantID:  tenant,
				Authorize: authorize,
			}))
		}
		tms.ServeHTTP(w, r)
	}))
	t.Cleanup(func() {
		_ = tms.Close()
		hs.Close()
	})
	return tms, "ws" + strings.TrimPrefix(hs.URL, "http")
}

func dialSubprotocol(t *testing.T, ctx context.Context, url, subprotocol string) *websocket.Conn {
	t.Helper()
	conn, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{Subprotocols: []string{subprotocol}})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial %s: %v", subprotocol, err)
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "done") })
	return conn
}

func stompFrame(command string, headers map[string]string, body string) []byte {
	var b strings.Builder
	b.WriteString(command)
	b.WriteByte('\n')
	for k, v := range headers {
		b.WriteString(k + ":" + v + "\n")
	}
	b.WriteByte('\n')
	b.WriteString(body)
	b.WriteByte(0)
	return []byte(b.String())
}

func stompWriteRead(t *testing.T, ctx context.Context, conn *websocket.Conn, frame []byte, wantPrefix string) {
	t.Helper()
	if err := conn.Write(ctx, websocket.MessageText, frame); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read %s: %v", wantPrefix, err)
	}
	if !strings.HasPrefix(string(data), wantPrefix) {
		t.Fatalf("got %q, want a %s frame", data, wantPrefix)
	}
}

// stompSubscriber connects over STOMP and subscribes to destination; the
// RECEIPT means the subscription is in place.
func stompSubscriber(t *testing.T, ctx context.Context, url, destination string) *websocket.Conn {
	t.Helper()
	conn := dialSubprotocol(t, ctx, url, "stomp")
	stompWriteRead(t, ctx, conn, stompFrame("CONNECT", map[string]string{"accept-version": "1.2", "host": "localhost"}, ""), "CONNECTED")
	stompWriteRead(t, ctx, conn, stompFrame("SUBSCRIBE", map[string]string{"id": "sub-1", "destination": destination, "receipt": "r-sub"}, ""), "RECEIPT")
	return conn
}

// stompSend connects over STOMP and sends body to destination, waiting for
// the RECEIPT so the publish has happened when it returns.
func stompSend(t *testing.T, ctx context.Context, url, destination, body string) {
	t.Helper()
	conn := dialSubprotocol(t, ctx, url, "stomp")
	stompWriteRead(t, ctx, conn, stompFrame("CONNECT", map[string]string{"accept-version": "1.2", "host": "localhost"}, ""), "CONNECTED")
	stompWriteRead(t, ctx, conn, stompFrame("SEND", map[string]string{"destination": destination, "receipt": "r-send"}, body), "RECEIPT")
}

// stompExpectMessage waits for a MESSAGE carrying body, or for its absence.
func stompExpectMessage(t *testing.T, conn *websocket.Conn, body string, want bool, wait time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	_, data, err := conn.Read(ctx)
	switch {
	case want && err != nil:
		t.Fatalf("no MESSAGE within %s: %v", wait, err)
	case want && (!strings.HasPrefix(string(data), "MESSAGE") || !strings.Contains(string(data), body)):
		t.Fatalf("got %q, want a MESSAGE with body %q", data, body)
	case !want && err == nil:
		t.Fatalf("received %q across tenants", data)
	}
}

func TestTenantMessagingServer_TenantsDoNotShareAHub(t *testing.T) {
	tms, url := tenantTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	subA := stompSubscriber(t, ctx, url+"?tenant=tenant-a", "orders")

	// The tenant-b publish goes first: had it leaked, it would be the first
	// MESSAGE tenant-a's subscriber reads. (A Read whose context expires
	// closes the connection, so the absence check comes last.)
	stompSend(t, ctx, url+"?tenant=tenant-b", "orders", "from-tenant-b")
	stompSend(t, ctx, url+"?tenant=tenant-a", "orders", "from-tenant-a")
	stompExpectMessage(t, subA, "from-tenant-a", true, 5*time.Second)
	stompExpectMessage(t, subA, "from-tenant-b", false, 500*time.Millisecond)

	if n := tms.TenantCount(); n != 2 {
		t.Fatalf("TenantCount = %d, want 2", n)
	}
}

func TestTenantMessagingServer_RefusesWithoutGrantAndAfterClose(t *testing.T) {
	tms, url := tenantTestServer(t)
	httpURL := "http" + strings.TrimPrefix(url, "ws")

	resp, err := http.Get(httpURL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("no grant: status = %d, want 403", resp.StatusCode)
	}

	if err := tms.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	resp, err = http.Get(httpURL + "?tenant=tenant-a")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("after Close: status = %d, want 503", resp.StatusCode)
	}
}

// TestTenantMessagingServer_PublishNeedsPublishPermission: a connection whose
// grant refuses messaging:publish gets a STOMP ERROR for SEND, has its MQTT
// connection closed on PUBLISH, and nothing reaches a same-tenant subscriber.
func TestTenantMessagingServer_PublishNeedsPublishPermission(t *testing.T) {
	_, url := tenantTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sub := stompSubscriber(t, ctx, url+"?tenant=tenant-a", "orders")
	denied := url + "?tenant=tenant-a&deny=1"

	// STOMP SEND, plain and inside a transaction.
	conn := dialSubprotocol(t, ctx, denied, "stomp")
	stompWriteRead(t, ctx, conn, stompFrame("CONNECT", map[string]string{"accept-version": "1.2", "host": "localhost"}, ""), "CONNECTED")
	stompWriteRead(t, ctx, conn, stompFrame("SEND", map[string]string{"destination": "orders", "receipt": "r"}, "stomp-denied"), "ERROR")
	tx := dialSubprotocol(t, ctx, denied, "stomp")
	stompWriteRead(t, ctx, tx, stompFrame("CONNECT", map[string]string{"accept-version": "1.2", "host": "localhost"}, ""), "CONNECTED")
	stompWriteRead(t, ctx, tx, stompFrame("BEGIN", map[string]string{"transaction": "t1", "receipt": "rb"}, ""), "RECEIPT")
	stompWriteRead(t, ctx, tx, stompFrame("SEND", map[string]string{"destination": "orders", "transaction": "t1", "receipt": "rs"}, "stomp-tx-denied"), "ERROR")

	// MQTT 3.1.1 PUBLISH QoS 0: the connection is closed.
	mq := dialSubprotocol(t, ctx, denied, "mqtt")
	connect := []byte{0x10, 13, 0, 4, 'M', 'Q', 'T', 'T', 4, 0x02, 0, 60, 0, 1, 'c'}
	if err := mq.Write(ctx, websocket.MessageBinary, connect); err != nil {
		t.Fatalf("mqtt CONNECT: %v", err)
	}
	if _, data, err := mq.Read(ctx); err != nil || len(data) == 0 || data[0] != 0x20 {
		t.Fatalf("mqtt CONNACK = %x, %v", data, err)
	}
	publish := append([]byte{0x30, byte(2 + len("orders") + len("mqtt-denied")), 0, byte(len("orders"))}, "ordersmqtt-denied"...)
	if err := mq.Write(ctx, websocket.MessageBinary, publish); err != nil {
		t.Fatalf("mqtt PUBLISH: %v", err)
	}
	if _, data, err := mq.Read(ctx); err == nil {
		t.Fatalf("mqtt connection stayed open after a denied PUBLISH, read %x", data)
	}

	stompExpectMessage(t, sub, "denied", false, 500*time.Millisecond)
}

// amqpTestFrame builds an AMQP 0-9-1 frame without the handler, so this file
// compiles in every build.
func amqpTestFrame(frameType byte, channel uint16, payload []byte) []byte {
	buf := &bytes.Buffer{}
	buf.WriteByte(frameType)
	_ = binary.Write(buf, binary.BigEndian, channel)
	_ = binary.Write(buf, binary.BigEndian, uint32(len(payload)))
	buf.Write(payload)
	buf.WriteByte(0xCE)
	return buf.Bytes()
}

func amqpTestMethod(classID, methodID uint16, args []byte) []byte {
	buf := &bytes.Buffer{}
	_ = binary.Write(buf, binary.BigEndian, classID)
	_ = binary.Write(buf, binary.BigEndian, methodID)
	buf.Write(args)
	return amqpTestFrame(1, 1, buf.Bytes())
}

// TestTenantMessagingServer_AMQPBodyReachesSubscriber publishes a non-empty
// AMQP body through the mounted handler and reads it on a same-tenant STOMP
// subscriber.
func TestTenantMessagingServer_AMQPBodyReachesSubscriber(t *testing.T) {
	_, url := tenantTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	url += "?tenant=tenant-a"

	sub := stompSubscriber(t, ctx, url, "orders")

	conn := dialSubprotocol(t, ctx, url, "amqp")
	write := func(b []byte) {
		t.Helper()
		if err := conn.Write(ctx, websocket.MessageBinary, b); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	read := func() {
		t.Helper()
		if _, _, err := conn.Read(ctx); err != nil {
			t.Fatalf("read: %v", err)
		}
	}
	write([]byte{'A', 'M', 'Q', 'P', 0, 0, 9, 1})
	read()                                   // Connection.Start
	write(amqpTestMethod(20, 10, []byte{0})) // Channel.Open
	read()                                   // Channel.Open-Ok

	body := []byte("amqp body over the mounted endpoint")
	args := &bytes.Buffer{}
	_ = binary.Write(args, binary.BigEndian, uint16(0))
	args.WriteByte(0) // default exchange
	args.WriteByte(byte(len("orders")))
	args.WriteString("orders")
	args.WriteByte(0)
	write(amqpTestMethod(60, 40, args.Bytes())) // Basic.Publish

	header := &bytes.Buffer{}
	_ = binary.Write(header, binary.BigEndian, uint16(60))
	_ = binary.Write(header, binary.BigEndian, uint16(0))
	_ = binary.Write(header, binary.BigEndian, uint64(len(body)))
	_ = binary.Write(header, binary.BigEndian, uint16(0))
	write(amqpTestFrame(2, 1, header.Bytes()))
	write(amqpTestFrame(3, 1, body))

	stompExpectMessage(t, sub, string(body), true, 5*time.Second)
}
