### Fixed

- sdk/go gRPC transport: `GetSessionHistory` returns `NextSeq` 0 when a full page ends the history, as documented, instead of a sequence that pages to an empty result. The `FromSequence` comment now says 0 replays from the first event, as every transport does (#1616).
