### Changed

- Without `--transport`, the CLI picks the transport from `--address`
  (go-quicbaas#185): an `http://` or `https://` URL selects REST, anything
  else the default `unix`. An explicit `--transport` always wins. Before
  this, a URL address with the default transport was used as a Unix socket
  path.

### Fixed

- The scheduler commands select their transport by the same rule as every
  other command, so `--transport grpc` with a `.sock` address dials the
  socket, and an unknown transport returns the typed
  `UnsupportedTransportError` naming the transports the scheduler supports.
- The root help names the real default socket, `$HOME/quicbaas.sock`, not
  `/tmp/quicbaas.sock`, and its remote-server example passes the transport.
