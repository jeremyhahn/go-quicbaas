### Fixed

- `UpdateNamespace` no longer deletes and recreates the namespace, which destroyed every vector in it. gRPC now refuses it with `Unimplemented`, REST with 501 and MCP with a typed refusal, until go-qrdb offers an in-place namespace update (#2040).
