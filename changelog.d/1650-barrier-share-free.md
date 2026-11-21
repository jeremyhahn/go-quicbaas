- barrier: gRPC's init-shamir, rekey and recovery-generate answers carry
  go-qrdb's `share_count`; their never-filled `shares` / `keys` fields are
  reserved. The TS SDK's three response types are go-qrdb's share-free ones
  (`share_count`, no `shares` / `keys`) (#1650).
