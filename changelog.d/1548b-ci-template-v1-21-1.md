### Changed

- CI adopts ci-template v1.21.1 (#1548, #1383): every stage runs in the
  devcontainer image, the image and integration jobs mask the forge
  credentials first, `make stage-gomodauth` runs silent, and the source
  checks report path and line only. `make docker-build` and
  `make integration-test` honour the template's `MODULE_ENV_FILE`,
  `IT_IMAGE` and `IT_PROJECT`, and `make lint` runs `actionlint`.
