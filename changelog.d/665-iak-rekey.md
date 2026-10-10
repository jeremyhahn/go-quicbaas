### Changed

- **TPM-backed node provisioning now treats the go-xkms IAK policy update as
  a re-provisioning event (go-quicbaas#665).** Operators must re-provision TPM
  nodes that were provisioned before the go-xkms v0.49.0-alpha bump, and must
  refresh anything that pinned the old IAK public key or certificate.
