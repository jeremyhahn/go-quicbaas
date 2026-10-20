### Fixed

- The batch client's Put, Get and Delete now refuse a server result list whose length differs from the requests, returning a typed Internal error that names both counts. Delete no longer panics with an index out of range when the server returns more results than requests, and Put and Get no longer return results with an empty entity type or silently drop results (#1638).
