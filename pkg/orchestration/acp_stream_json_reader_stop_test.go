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

package orchestration

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"testing/synctest"
)

// errScriptedStreamEnded is the process-gone report the readers below are
// opened with.
var errScriptedStreamEnded = errors.New("scripted stream ended")

// parkedReader starts a reader on a pipe and writes one record more than its
// buffer holds, with nobody consuming. It returns once every goroutine in the
// bubble is durably blocked: the writer has finished and the reader is parked
// on its send of the last record. Call it inside a synctest bubble.
func parkedReader(t *testing.T) (*streamJSONReader, *io.PipeWriter) {
	t.Helper()
	stdoutR, stdoutW := io.Pipe()
	reader := newStreamJSONReader(stdoutR, func() error { return errScriptedStreamEnded })

	records := bytes.Repeat([]byte("{\"type\":\"assistant\"}\n"), streamJSONReaderBuffer+1)
	go func() {
		if _, err := stdoutW.Write(records); err != nil {
			t.Errorf("write the records: %v", err)
		}
	}()
	synctest.Wait()

	if len(reader.records) != streamJSONReaderBuffer {
		t.Fatalf("buffered records = %d, want a full buffer of %d", len(reader.records), streamJSONReaderBuffer)
	}
	return reader, stdoutW
}

func readerDone(r *streamJSONReader) bool {
	select {
	case <-r.done:
		return true
	default:
		return false
	}
}

// TestStreamJSON_StopEndsAReaderParkedOnAFullBuffer pins the exit a closed
// session needs: a reader parked on its full buffer returns when stopped,
// closes done, and says the session closed. synctest.Test itself fails if the
// goroutine is still blocked when the bubble ends.
func TestStreamJSON_StopEndsAReaderParkedOnAFullBuffer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reader, _ := parkedReader(t)

		reader.stop("sess-1")
		synctest.Wait()

		if !readerDone(reader) {
			t.Fatal("the reader is still running after stop")
		}
		var closed *SessionClosedError
		if !errors.As(reader.err, &closed) {
			t.Fatalf("err = %v, want SessionClosedError", reader.err)
		}
		if closed.SessionID != "sess-1" {
			t.Errorf("SessionID = %q, want sess-1", closed.SessionID)
		}
		// The buffered records are still there for anyone who asks, then
		// the reason.
		for range streamJSONReaderBuffer {
			if _, err := reader.next(context.Background()); err != nil {
				t.Fatalf("next() before the end = %v, want a record", err)
			}
		}
		if _, err := reader.next(context.Background()); !errors.Is(err, ErrSessionClosed) {
			t.Errorf("next() at the end = %v, want ErrSessionClosed", err)
		}
	})
}

// TestStreamJSON_AParkedReaderOutlivesEOFWithoutStop is the leak itself: a
// reader parked on its full buffer never reads stdout again, so even the
// stream's end does not free it. Only stop does.
func TestStreamJSON_AParkedReaderOutlivesEOFWithoutStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reader, stdoutW := parkedReader(t)

		if err := stdoutW.Close(); err != nil {
			t.Fatalf("close stdout: %v", err)
		}
		synctest.Wait()
		if readerDone(reader) {
			t.Fatal("the parked reader exited without stop; the leak this guards is gone or unobserved")
		}

		reader.stop("sess-1")
		synctest.Wait()
		if !readerDone(reader) {
			t.Fatal("the reader is still running after stop")
		}
	})
}

// TestStreamJSON_StopAfterTheStreamEndedKeepsItsReason checks stop is safe on
// a reader that already finished, and more than once: the reason it ended is
// not overwritten.
func TestStreamJSON_StopAfterTheStreamEndedKeepsItsReason(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stdoutR, stdoutW := io.Pipe()
		reader := newStreamJSONReader(stdoutR, func() error { return errScriptedStreamEnded })
		if err := stdoutW.Close(); err != nil {
			t.Fatalf("close stdout: %v", err)
		}
		<-reader.done

		reader.stop("sess-1")
		reader.stop("sess-1")

		if !errors.Is(reader.err, errScriptedStreamEnded) {
			t.Errorf("err = %v, want the process-gone report", reader.err)
		}
	})
}

// TestACPBackend_CLI_CloseStopsAParkedReader pins the wiring: closing a CLI
// session whose reader is parked on a full buffer ends the reader.
func TestACPBackend_CLI_CloseStopsAParkedReader(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reader, _ := parkedReader(t)
		stdinR, stdinW := io.Pipe()
		defer stdinR.Close()

		backend := NewACPBackend(ACPBackendConfig{Command: "claude"})
		backend.sessions.Store("sess-1", &acpSession{id: "sess-1", stdin: stdinW, reader: reader})

		if err := backend.Close(context.Background(), "sess-1"); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
		synctest.Wait()

		if !readerDone(reader) {
			t.Fatal("the reader is still running after Close")
		}
		if !errors.Is(reader.err, ErrSessionClosed) {
			t.Errorf("err = %v, want ErrSessionClosed", reader.err)
		}
	})
}
