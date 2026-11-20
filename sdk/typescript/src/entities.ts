// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

// ---------------------------------------------------------------------------
// Generic entity domain types.
//
// Mirrors the generic entity routes in pkg/api/rest/routes.go
// (/api/v1/entities/{type}/ and /api/v1/entities/{type}/{id}) and their
// types in pkg/api/rest/types.go. EntitiesClient reads and writes any
// registered entity type by name.
//
// Wire note: entity ids are decimal uint64 values. Responses carry them as
// JSON numbers; arguments accept a number or a decimal string, so an id
// beyond Number.MAX_SAFE_INTEGER can still be addressed exactly. `data` is
// any JSON value. Timestamps arrive as RFC 3339 strings.
//
// Routing note: the collection routes (list and create) end in a trailing
// slash; the item routes (put, get, delete) do not. Each path segment is
// encoded with encodeURIComponent, so a type name containing '/' stays one
// segment.
// ---------------------------------------------------------------------------

/** PutEntityRequest is the body of a create (POST) or put (PUT). */
export interface PutEntityRequest {
  entity_type: string;
  id?: number | string;
  data: unknown;
  consistency_level?: string;
}

/** PutEntityResponse confirms a create or put. */
export interface PutEntityResponse {
  id: number;
  entity_type: string;
  created: boolean;
  timestamp: string;
}

/** EntityRecord is one entity as returned by get. */
export interface EntityRecord {
  id: number;
  entity_type: string;
  data: unknown;
  version?: number;
}

/** DeleteEntityResponse confirms a delete. */
export interface DeleteEntityResponse {
  id: number;
  entity_type: string;
  deleted: boolean;
  timestamp: string;
}

/** EntityItem is one entity in a list page. */
export interface EntityItem {
  id: number;
  entity_type: string;
  data: unknown;
  timestamp?: string;
}

/** EntityList is one page of entities of a type. */
export interface EntityList {
  entities: EntityItem[];
  total?: number;
  next_cursor?: string;
  has_more: boolean;
}

/** EntityListOptions paginates a list and sets its read consistency. */
export interface EntityListOptions {
  limit?: number;
  cursor?: string;
  consistency?: string;
}

/** EntityGetOptions sets a get's read consistency and timeout. */
export interface EntityGetOptions {
  consistency?: string;
  timeout_ms?: number;
}

/** EntityWriteOptions sets a write's mode. */
export interface EntityWriteOptions {
  write_mode?: string;
}

// ---------------------------------------------------------------------------
// EntitiesClient interface
// ---------------------------------------------------------------------------

/**
 * EntitiesClient reads and writes entities of any registered type through
 * the generic entity routes. Maps to /api/v1/entities/* REST endpoints.
 */
export interface EntitiesClient {
  /** list returns one page of entities of a type. GET /api/v1/entities/{type}/ */
  list(type: string, opts?: EntityListOptions): Promise<EntityList>;

  /** create stores a new entity, the server assigning its id. POST /api/v1/entities/{type}/ */
  create(type: string, data: unknown, opts?: EntityWriteOptions): Promise<PutEntityResponse>;

  /** put creates or replaces the entity with the given id. PUT /api/v1/entities/{type}/{id} */
  put(type: string, id: number | string, data: unknown, opts?: EntityWriteOptions): Promise<PutEntityResponse>;

  /** get returns the entity with the given id. GET /api/v1/entities/{type}/{id} */
  get(type: string, id: number | string, opts?: EntityGetOptions): Promise<EntityRecord>;

  /** delete removes the entity with the given id. DELETE /api/v1/entities/{type}/{id} */
  delete(type: string, id: number | string, opts?: EntityWriteOptions): Promise<DeleteEntityResponse>;
}
