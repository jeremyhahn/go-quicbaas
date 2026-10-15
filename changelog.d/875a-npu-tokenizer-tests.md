### Added

- **NPU sidecar device tier exercises the real tokenizer and startup (go-quicbaas#875).**
  `make test-npu-sidecar-device` now loads `HuggingFaceTokenizer` from a tiny
  WordPiece tokenizer built on disk at test time (no network, no download)
  and pins its load errors, special-token ids, untruncated body encoding,
  windowing at the model maximum and the padded attention mask. It also runs
  `python -m npu_embedding`'s `main` end to end over the tiny IR plus that
  tokenizer: warmup compiles every default bucket, and the served app embeds,
  enforces its token, and reports a missing NPU driver. With both tiers
  together `tokenizer.py` and `__main__.py` are above 90% coverage.
