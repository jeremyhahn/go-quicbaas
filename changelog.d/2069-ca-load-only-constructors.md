### Added

- **sdk/go/xkms/ca re-exports the load-only CA constructors
  (go-quicbaas#2069).** `NewParentCA` builds the root uninitialized, so a
  consumer can `Load()` an existing CA instead of minting a new root;
  `NewIntermediateCA` builds the intermediate under an initialized parent.
