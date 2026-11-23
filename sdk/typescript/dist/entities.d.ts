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
//# sourceMappingURL=entities.d.ts.map