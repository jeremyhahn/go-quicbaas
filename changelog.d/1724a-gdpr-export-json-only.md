### Fixed

- A GDPR data export request in a format other than JSON is now refused as invalid input (REST 400, gRPC `InvalidArgument`, MCP invalid input) by the GDPR manager that every transport calls; an empty format still means JSON. The export generator writes JSON only, so a `csv` or `xml` request was accepted before and its JSON download was labelled `.csv` / `text/csv` or `.xml` / `application/xml`. The REST download is now always `user_data_export.json` with `application/json`, whatever format a stored request names, and the MCP tool schema offers `json` only (#1724).
