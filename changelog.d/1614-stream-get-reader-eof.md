### Fixed

- `objectstorage.StreamGetReader.Read` returns `io.EOF` at the end of the
  object, so `io.ReadAll` and `io.Copy` finish without an error (#1614).
  A stream that ends before the object's advertised size has arrived returns
  `io.ErrUnexpectedEOF`, so a truncated download is never read as complete.
