# Allocating Ports in Tests

Tests that hand a port number to a server which binds it later must draw the
number from `pkg/testutil/netport`. Do not listen on `:0` and close the
listener.

## Why not `:0`

Listening on `127.0.0.1:0`, reading the kernel's choice and closing the listener
is check-then-act where the check *releases* the resource. From the close until
the server binds, the port is genuinely free and anything on the host may take
it. Nothing in the caller can shrink that interval to zero.

The interval is crossed constantly here: `pkg/server` builds a test config at
roughly seven hundred call sites, ~75 of which boot a standalone qrdb node, and
several worktrees gate at once on one machine. The result was tests failing with
`bind: address already in use` on packages their diff never touched — including
another lane's gate (go-quicbaas#706), and go-quicbaas#497 before it.

## What `netport` guarantees

`netport.Reserve` hands back a port that is

1. **outside the kernel's ephemeral range** (read from
   `/proc/sys/net/ipv4/ip_local_port_range`), so no `:0` bind and no outbound
   connection anywhere on the host can be assigned it;
2. **locked host-wide** with `flock(2)` on a per-port file under a per-user temp
   directory, so no other cooperating test binary draws it. The kernel drops the
   lock when the process dies, so a killed run leaves nothing stale; and
3. **proven free on both TCP and UDP** at draw time — a qrdb REST listener takes
   TCP *P* and its HTTP/3 sibling takes UDP *P*, and the two namespaces are
   independent.

A reservation lasts until `Release`. `MustReserve` registers the release as a
test cleanup, which runs *after* the test's own cleanups — that is, after the
server holding the port has shut down.

What remains is a process deliberately binding a specific port in the reserved
window mid-run. That is not a race decided by chance.

## Using it

```go
port := netport.MustReserveOne(t)              // one port, released at test end
ports := netport.MustReserve(t, 3)             // n distinct ports
addr := netport.MustReserveLoopbackAddr(t)     // "127.0.0.1:<port>"

ports, err := netport.Reserve(1)               // no *testing.T available
defer netport.Release(ports...)
```

Inside `pkg/server`, `getFreePort` / `getFreePorts` already delegate here; keep
using them.

## When you do not need it

A server that binds `127.0.0.1:0` **itself** and reports its address afterwards
has no window to lose — there is no interval in which the port is unheld. Prefer
that shape when the API allows it, and reserve a number only when the port has
to be known before the bind.

## Bounds

The window is 8192 ports immediately below the ephemeral range (24576–32767 on a
default Linux kernel), falling back to the space above the range on a host whose
ephemeral range starts too low. `ExhaustedError` splits its count into ports
already reserved (a leak, or too many tests in flight) and ports occupied by
something outside the mechanism (the window overlaps a service range on this
host).

The package is unix-only: the cross-process reservation is `flock(2)`.
