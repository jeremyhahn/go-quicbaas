"""Start the baked-in Kotlin language server and complete an LSP handshake.

The toolchain probe proves only that kotlin-lsp is on the PATH. This proves
the server it names starts, offline, from an empty HOME, as the uid the probe
runs as, and answers the request every client session opens with
(go-quicbaas#1746): it sends `initialize` over stdio, requires the response to
that request carrying the server's capabilities, then sends `shutdown` and
`exit`.

Every read is bounded by one deadline, so a server that never answers fails
the probe instead of hanging it, and a server that exits before it answers
fails it too. The server is killed if it is still running when the probe ends.

Usage: python3 - [DEADLINE_SECONDS] < kotlin-lsp-probe.py
"""

import json
import os
import select
import subprocess
import sys
import tempfile
import time

DEFAULT_DEADLINE_SECONDS = 180.0
INITIALIZE_ID = 1
SHUTDOWN_ID = 2


class ProbeFailure(Exception):
    """The server did not complete the handshake; the message says how."""


def frame(message):
    body = json.dumps(message).encode()
    return b"Content-Length: %d\r\n\r\n" % len(body) + body


class MessageReader:
    """Reads Content-Length framed LSP messages from a pipe, by a deadline."""

    def __init__(self, stream, deadline):
        self.fd = stream.fileno()
        self.deadline = deadline
        self.buffer = b""

    def _fill(self, waiting_for):
        remaining = self.deadline - time.monotonic()
        if remaining <= 0:
            raise ProbeFailure("kotlin-lsp did not answer %s before the deadline" % waiting_for)
        ready, _, _ = select.select([self.fd], [], [], remaining)
        if not ready:
            raise ProbeFailure("kotlin-lsp did not answer %s before the deadline" % waiting_for)
        chunk = os.read(self.fd, 65536)
        if not chunk:
            raise ProbeFailure("kotlin-lsp exited before it answered %s" % waiting_for)
        self.buffer += chunk

    def _next(self, waiting_for):
        while b"\r\n\r\n" not in self.buffer:
            self._fill(waiting_for)
        header, self.buffer = self.buffer.split(b"\r\n\r\n", 1)
        length = None
        for line in header.split(b"\r\n"):
            name, _, value = line.partition(b":")
            if name.strip().lower() == b"content-length":
                length = int(value.strip())
        if length is None:
            raise ProbeFailure("kotlin-lsp sent a message with no Content-Length: %r" % header)
        while len(self.buffer) < length:
            self._fill(waiting_for)
        body, self.buffer = self.buffer[:length], self.buffer[length:]
        return json.loads(body)

    def response(self, request_id, waiting_for):
        """Skips the server's notifications and requests until the response."""
        while True:
            message = self._next(waiting_for)
            if "method" not in message and message.get("id") == request_id:
                return message


def send(server, waiting_for, *messages):
    try:
        for message in messages:
            server.stdin.write(frame(message))
        server.stdin.flush()
    except BrokenPipeError:
        raise ProbeFailure("kotlin-lsp exited before it answered %s" % waiting_for)


def handshake(server, deadline):
    reader = MessageReader(server.stdout, deadline)
    send(server, "initialize", {"jsonrpc": "2.0", "id": INITIALIZE_ID, "method": "initialize",
                                "params": {"processId": os.getpid(), "rootUri": None, "capabilities": {}}})
    initialized = reader.response(INITIALIZE_ID, "initialize")
    if "error" in initialized:
        raise ProbeFailure("kotlin-lsp refused initialize: %s" % json.dumps(initialized["error"]))
    capabilities = (initialized.get("result") or {}).get("capabilities")
    if not isinstance(capabilities, dict):
        raise ProbeFailure("kotlin-lsp answered initialize without capabilities: %s" % json.dumps(initialized))

    send(server, "shutdown", {"jsonrpc": "2.0", "method": "initialized", "params": {}},
         {"jsonrpc": "2.0", "id": SHUTDOWN_ID, "method": "shutdown", "params": None})
    reader.response(SHUTDOWN_ID, "shutdown")
    send(server, "exit", {"jsonrpc": "2.0", "method": "exit", "params": None})
    server.stdin.close()
    server.wait(timeout=max(deadline - time.monotonic(), 0.1))
    return sorted(capabilities)


def main():
    seconds = float(sys.argv[1]) if len(sys.argv) > 1 else DEFAULT_DEADLINE_SECONDS
    deadline = time.monotonic() + seconds
    home = tempfile.mkdtemp()
    log = tempfile.TemporaryFile()
    server = subprocess.Popen(["kotlin-lsp", "--stdio"], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                              stderr=log, env=dict(os.environ, HOME=home))
    try:
        capabilities = handshake(server, deadline)
    except (ProbeFailure, OSError, ValueError, subprocess.TimeoutExpired) as failure:
        print("kotlin-lsp failed the LSP handshake: %s" % failure)
        log.seek(0)
        print(log.read()[-2000:].decode(errors="replace"))
        return 1
    finally:
        if server.poll() is None:
            server.kill()
            server.wait()
    print("kotlin-lsp answered initialize with %d capabilities and shut down" % len(capabilities))
    return 0


if __name__ == "__main__":
    sys.exit(main())
