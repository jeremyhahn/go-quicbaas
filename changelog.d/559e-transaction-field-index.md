### Fixed

- **Transaction validation errors name the right field (#559).** A REST
  transaction validation error built the field name from the index with
  `string(rune(i))`, so `compare[3].id` came out as `compare[\x03].id`
  and index 12 as a form feed. The index of `compare`, `success` and
  `failure` entries is now written in decimal.
