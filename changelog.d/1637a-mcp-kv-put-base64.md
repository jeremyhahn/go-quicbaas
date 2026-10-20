### Fixed

- The MCP `qrdb_put` and `quicbaas_batch_put` tools now decode their `data` argument (and its `value` alias) from standard base64, as their schemas declare, so a binary value put over MCP reads back as the same bytes; bad base64 is refused with a typed error naming `data`, and a batch with one bad operation stores nothing (#1637).
