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

//go:build quicbaas_amqp && quicbaas_mqtt && quicbaas_stomp

package subprotocol

import (
	"bytes"
	"context"
	"encoding/binary"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

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

// TestTenantMessagingServer_AMQPExchangeBindingReachesConsumer: a queue one
// AMQP connection binds to a direct exchange receives what another
// connection of the same tenant publishes to that exchange and key.
func TestTenantMessagingServer_AMQPExchangeBindingReachesConsumer(t *testing.T) {
	_, url := tenantTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	url += "?tenant=tenant-a"

	open := func() *websocket.Conn {
		t.Helper()
		conn := dialSubprotocol(t, ctx, url, "amqp")
		if err := conn.Write(ctx, websocket.MessageBinary, []byte{'A', 'M', 'Q', 'P', 0, 0, 9, 1}); err != nil {
			t.Fatalf("write protocol header: %v", err)
		}
		if _, _, err := conn.Read(ctx); err != nil { // Connection.Start
			t.Fatalf("read Connection.Start: %v", err)
		}
		amqpRoundTrip(t, ctx, conn, amqpTestMethod(20, 10, []byte{0}), "Channel.Open")
		return conn
	}
	method := func(build func(*bytes.Buffer)) []byte {
		args := &bytes.Buffer{}
		_ = binary.Write(args, binary.BigEndian, uint16(0)) // reserved-1
		build(args)
		return args.Bytes()
	}

	consumer := open()
	amqpRoundTrip(t, ctx, consumer, amqpTestMethod(40, 10, method(func(b *bytes.Buffer) {
		writeShortString(b, "events")
		writeShortString(b, "direct")
		b.WriteByte(0)
		_ = binary.Write(b, binary.BigEndian, uint32(0))
	})), "Exchange.Declare")
	amqpRoundTrip(t, ctx, consumer, amqpTestMethod(50, 10, method(func(b *bytes.Buffer) {
		writeShortString(b, "orders")
		b.WriteByte(0)
		_ = binary.Write(b, binary.BigEndian, uint32(0))
	})), "Queue.Declare")
	amqpRoundTrip(t, ctx, consumer, amqpTestMethod(50, 20, method(func(b *bytes.Buffer) {
		writeShortString(b, "orders")
		writeShortString(b, "events")
		writeShortString(b, "order.created")
		b.WriteByte(0)
		_ = binary.Write(b, binary.BigEndian, uint32(0))
	})), "Queue.Bind")
	amqpRoundTrip(t, ctx, consumer, amqpTestMethod(60, 20, method(func(b *bytes.Buffer) {
		writeShortString(b, "orders")
		writeShortString(b, "")
		b.WriteByte(0x02) // no-ack
		_ = binary.Write(b, binary.BigEndian, uint32(0))
	})), "Basic.Consume")

	publisher := open()
	body := []byte("hello")
	publish := amqpTestMethod(60, 40, method(func(b *bytes.Buffer) {
		writeShortString(b, "events")
		writeShortString(b, "order.created")
		b.WriteByte(0)
	}))
	header := &bytes.Buffer{}
	_ = binary.Write(header, binary.BigEndian, uint16(60))
	_ = binary.Write(header, binary.BigEndian, uint16(0))
	_ = binary.Write(header, binary.BigEndian, uint64(len(body)))
	_ = binary.Write(header, binary.BigEndian, uint16(0))
	publish = append(publish, amqpTestFrame(2, 1, header.Bytes())...)
	publish = append(publish, amqpTestFrame(3, 1, body)...)
	if err := publisher.Write(ctx, websocket.MessageBinary, publish); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// Basic.Deliver (class 60, method 60) and its body frame, in one
	// WebSocket message or several.
	deliverCtx, deliverCancel := context.WithTimeout(ctx, 5*time.Second)
	defer deliverCancel()
	var got []byte
	deliver := amqpTestMethod(60, 60, nil)[7:11]
	bodyFrame := amqpTestFrame(3, 1, body)
	for !bytes.Contains(got, deliver) || !bytes.Contains(got, bodyFrame) {
		_, data, err := consumer.Read(deliverCtx)
		if err != nil {
			t.Fatalf("no Basic.Deliver with body %q within 5s (read %x): %v", body, got, err)
		}
		got = append(got, data...)
	}
}

// amqpRoundTrip writes one method frame and reads its reply, failing on a
// Channel.Close or Connection.Close.
func amqpRoundTrip(t *testing.T, ctx context.Context, conn *websocket.Conn, frame []byte, what string) {
	t.Helper()
	if err := conn.Write(ctx, websocket.MessageBinary, frame); err != nil {
		t.Fatalf("write %s: %v", what, err)
	}
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read %s reply: %v", what, err)
	}
	if len(data) >= 11 && data[0] == 1 {
		classID := binary.BigEndian.Uint16(data[7:9])
		methodID := binary.BigEndian.Uint16(data[9:11])
		if (classID == 20 || classID == 10) && methodID == 40 {
			t.Fatalf("%s: server closed the channel or connection: %x", what, data)
		}
	}
}
