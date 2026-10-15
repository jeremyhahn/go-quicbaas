### Fixed

- **A closed CLI agent session no longer leaks its stream reader
  (go-quicbaas#1329).** When a persistent CLI session was closed while its
  stdout reader was waiting on a full record buffer that nobody would drain
  again, the reader goroutine blocked forever, holding its scanner buffer and
  every buffered record. Closing the session now stops the reader, which
  ends with `SessionClosedError`. While the session is open, the reader
  still waits for the consumer and never drops a record.
