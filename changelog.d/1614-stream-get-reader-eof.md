### Fixed

- `objectstorage.StreamGetReader.Read` returns `io.EOF` at the end of the
  object, so `io.ReadAll` and `io.Copy` finish without an error (#1614).
