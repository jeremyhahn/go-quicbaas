### Fixed

- A full vector-namespace scan, as an index rebuild reads it, now re-reads every vector it is about to return and starts over when one was deleted or changed meanwhile, so a write made directly against storage between two pages can no longer leave a deleted vector in, or a live one out of, the rebuilt index (#1670).
