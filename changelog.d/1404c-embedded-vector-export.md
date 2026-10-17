### Fixed

- **The embedded transport serves vector Export and Import (go-quicbaas#1404).**
  `quicbaas.Vector/Export` and `quicbaas.Vector/Import` now work in process.
  The batch codec (JSON batches, optional gzip, CRC32 checksum) moved from
  the gRPC server into `pkg/service` (`ExportVectors`, `ImportVectors`), and
  gRPC and embedded both call it. There is one wire format and it has not
  changed. Import used to skip every vector whenever the lookup returned no
  error. It now skips only vectors that already exist.

### Changed

- **gRPC vector Export and Import refuse an unknown format or compression.**
  A format other than `json`, or a compression other than `none` or `gzip`,
  now returns `InvalidArgument` (on Import, the batch counts as failed).
  Before, it fell back to json with no compression without saying so.
