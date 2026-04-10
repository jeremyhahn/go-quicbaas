// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package iam

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	groupServiceName = "quicbaas.IAM.Group"
)

// GroupClient provides group management operations.
type GroupClient interface {
	// Create creates a new group in a tenant.
	Create(ctx context.Context, tenantID string, req *CreateGroupRequest) (*Group, error)

	// Get retrieves a group by ID.
	Get(ctx context.Context, tenantID, groupID string) (*Group, error)

	// Update updates a group.
	Update(ctx context.Context, tenantID, groupID string, req *UpdateGroupRequest) (*Group, error)

	// Delete deletes a group.
	Delete(ctx context.Context, tenantID, groupID string) error

	// List lists groups in a tenant with optional filtering.
	List(ctx context.Context, tenantID string, req *ListGroupsRequest) (*ListGroupsResponse, error)

	// AddMember adds a user to a group.
	AddMember(ctx context.Context, tenantID, groupID, userID string) error

	// RemoveMember removes a user from a group.
	RemoveMember(ctx context.Context, tenantID, groupID, userID string) error

	// ListMembers lists members of a group.
	ListMembers(ctx context.Context, tenantID, groupID string) (*ListMembersResponse, error)
}

// CreateGroupRequest is a request to create a group.
type CreateGroupRequest struct {
	// ID is the client-provided group ID.
	// If empty, the server will generate a UUID.
	ID string `json:"id,omitempty"`

	// Name is the unique name within the tenant.
	Name string `json:"name"`

	// DisplayName is the human-readable display name.
	DisplayName string `json:"display_name,omitempty"`

	// Description is an optional description.
	Description string `json:"description,omitempty"`

	// Roles are initial role IDs to assign to the group.
	Roles []string `json:"roles,omitempty"`

	// Metadata contains additional group metadata.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// UpdateGroupRequest is a request to update a group.
type UpdateGroupRequest struct {
	// Name is the unique name within the tenant.
	Name string `json:"name,omitempty"`

	// DisplayName is the human-readable display name.
	DisplayName string `json:"display_name,omitempty"`

	// Description is an optional description.
	Description string `json:"description,omitempty"`

	// Roles are role IDs to assign to the group (replaces existing).
	Roles []string `json:"roles,omitempty"`

	// Metadata contains additional group metadata.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// ListGroupsRequest is a request to list groups.
type ListGroupsRequest struct {
	// PageSize is the maximum number of groups to return.
	PageSize int `json:"page_size,omitempty"`

	// PageToken is the token for the next page.
	PageToken string `json:"page_token,omitempty"`

	// Filter is an optional filter expression.
	Filter string `json:"filter,omitempty"`

	// UserID filters groups that contain this user.
	UserID string `json:"user_id,omitempty"`
}

// ListGroupsResponse is the response from listing groups.
type ListGroupsResponse struct {
	// Groups is the list of groups.
	Groups []Group `json:"groups"`

	// Pagination contains pagination information.
	Pagination PaginationInfo `json:"pagination"`
}

// ListMembersResponse is the response from listing group members.
type ListMembersResponse struct {
	// Members is the list of group members.
	Members []GroupMember `json:"members"`

	// Pagination contains pagination information.
	Pagination PaginationInfo `json:"pagination"`
}

// Internal request/response types for transport.

type createGroupRequestInternal struct {
	TenantID string              `json:"tenant_id"`
	Request  *CreateGroupRequest `json:"request"`
}

type getGroupRequest struct {
	TenantID string `json:"tenant_id"`
	GroupID  string `json:"group_id"`
}

type updateGroupRequestInternal struct {
	TenantID string              `json:"tenant_id"`
	GroupID  string              `json:"group_id"`
	Request  *UpdateGroupRequest `json:"request"`
}

type deleteGroupRequest struct {
	TenantID string `json:"tenant_id"`
	GroupID  string `json:"group_id"`
}

type deleteGroupResponse struct{}

type listGroupsRequestInternal struct {
	TenantID string             `json:"tenant_id"`
	Request  *ListGroupsRequest `json:"request"`
}

type groupMemberRequest struct {
	TenantID string `json:"tenant_id"`
	GroupID  string `json:"group_id"`
	UserID   string `json:"user_id"`
}

type groupMemberResponse struct{}

type listMembersRequest struct {
	TenantID string `json:"tenant_id"`
	GroupID  string `json:"group_id"`
}

// convertToGroup converts a response to a Group type.
// It handles both *Group pointers and map[string]interface{} (from REST/gRPC transports).
func convertToGroup(resp interface{}) (*Group, bool) {
	if resp == nil {
		return nil, false
	}

	// If already a *Group, return it directly.
	if group, ok := resp.(*Group); ok {
		return group, true
	}

	// If it's a map, try to convert it.
	respMap, ok := resp.(map[string]interface{})
	if !ok {
		return nil, false
	}

	// Check for nested group field (server response format: {group: {...}, timestamp: "..."})
	if groupData, ok := respMap["group"].(map[string]interface{}); ok {
		respMap = groupData
	}

	group := &Group{}

	if v, ok := respMap["id"].(string); ok {
		group.ID = v
	}
	if v, ok := respMap["tenant_id"].(string); ok {
		group.TenantID = v
	}
	if v, ok := respMap["name"].(string); ok {
		group.Name = v
	}
	if v, ok := respMap["display_name"].(string); ok {
		group.DisplayName = v
	}
	// If DisplayName is still empty, use Name as fallback.
	if group.DisplayName == "" && group.Name != "" {
		group.DisplayName = group.Name
	}
	if v, ok := respMap["description"].(string); ok {
		group.Description = v
	}

	// Handle member_count (can be int, int32, int64, or float64 from JSON).
	if v, ok := respMap["member_count"].(float64); ok {
		group.MemberCount = int(v)
	} else if v, ok := respMap["member_count"].(int); ok {
		group.MemberCount = v
	} else if v, ok := respMap["member_count"].(int32); ok {
		group.MemberCount = int(v)
	} else if v, ok := respMap["member_count"].(int64); ok {
		group.MemberCount = int(v)
	}

	// Handle roles slice.
	if rolesRaw, ok := respMap["roles"]; ok {
		switch roles := rolesRaw.(type) {
		case []string:
			group.Roles = roles
		case []interface{}:
			for _, r := range roles {
				if s, ok := r.(string); ok {
					group.Roles = append(group.Roles, s)
				}
			}
		}
	}

	// Handle metadata map.
	if metaRaw, ok := respMap["metadata"]; ok {
		switch meta := metaRaw.(type) {
		case map[string]string:
			group.Metadata = meta
		case map[string]interface{}:
			group.Metadata = make(map[string]string, len(meta))
			for k, v := range meta {
				if s, ok := v.(string); ok {
					group.Metadata[k] = s
				}
			}
		}
	}

	// Handle timestamps (either string or time.Time).
	if v, ok := respMap["created_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			group.CreatedAt = t
		}
	} else if v, ok := respMap["created_at"].(time.Time); ok {
		group.CreatedAt = v
	}
	if v, ok := respMap["updated_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			group.UpdatedAt = t
		}
	} else if v, ok := respMap["updated_at"].(time.Time); ok {
		group.UpdatedAt = v
	}

	return group, true
}

// isSuccessResponse checks if a response indicates success.
// It handles map[string]bool and map[string]interface{} response types.
func isSuccessResponse(resp interface{}) bool {
	if resp == nil {
		return true // nil response is considered success for delete operations
	}

	// Check for *deleteGroupResponse type.
	if _, ok := resp.(*deleteGroupResponse); ok {
		return true
	}

	// Check for *groupMemberResponse type.
	if _, ok := resp.(*groupMemberResponse); ok {
		return true
	}

	// Check for map[string]bool response.
	if m, ok := resp.(map[string]bool); ok {
		if success, exists := m["success"]; exists {
			return success
		}
		return true // Map exists but no success key
	}

	// Check for map[string]interface{} response.
	if m, ok := resp.(map[string]interface{}); ok {
		if success, exists := m["success"]; exists {
			if b, ok := success.(bool); ok {
				return b
			}
		}
		return true // Map exists but no success key
	}

	return false
}

// groupClient implements the GroupClient interface.
type groupClient struct {
	transport transport.Transport
}

// NewGroupClient creates a new group client.
func NewGroupClient(t transport.Transport) GroupClient {
	if t == nil {
		return nil
	}
	return &groupClient{
		transport: t,
	}
}

// Create creates a new group in a tenant.
func (c *groupClient) Create(ctx context.Context, tenantID string, req *CreateGroupRequest) (*Group, error) {
	if err := validateCreateGroupRequest(tenantID, req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.group.Create", "context is required")
	}

	resp, err := c.transport.Request(ctx, groupServiceName, "Create", &createGroupRequestInternal{
		TenantID: tenantID,
		Request:  req,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("iam.group.Create", "nil response from server")
	}

	group, ok := convertToGroup(resp)
	if !ok {
		return nil, errors.NewInternal("iam.group.Create", "invalid response type")
	}

	return group, nil
}

// Get retrieves a group by ID.
func (c *groupClient) Get(ctx context.Context, tenantID, groupID string) (*Group, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.group.Get", "tenant ID is required")
	}

	if groupID == "" {
		return nil, errors.NewInvalidInput("iam.group.Get", "group ID is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.group.Get", "context is required")
	}

	resp, err := c.transport.Request(ctx, groupServiceName, "Get", &getGroupRequest{
		TenantID: tenantID,
		GroupID:  groupID,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewNotFound("iam.group.Get", "group not found")
	}

	group, ok := convertToGroup(resp)
	if !ok {
		return nil, errors.NewInternal("iam.group.Get", "invalid response type")
	}

	return group, nil
}

// Update updates a group.
func (c *groupClient) Update(ctx context.Context, tenantID, groupID string, req *UpdateGroupRequest) (*Group, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.group.Update", "tenant ID is required")
	}

	if groupID == "" {
		return nil, errors.NewInvalidInput("iam.group.Update", "group ID is required")
	}

	if req == nil {
		return nil, errors.NewInvalidInput("iam.group.Update", "request is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.group.Update", "context is required")
	}

	resp, err := c.transport.Request(ctx, groupServiceName, "Update", &updateGroupRequestInternal{
		TenantID: tenantID,
		GroupID:  groupID,
		Request:  req,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("iam.group.Update", "nil response from server")
	}

	group, ok := convertToGroup(resp)
	if !ok {
		return nil, errors.NewInternal("iam.group.Update", "invalid response type")
	}

	return group, nil
}

// Delete deletes a group.
func (c *groupClient) Delete(ctx context.Context, tenantID, groupID string) error {
	if tenantID == "" {
		return errors.NewInvalidInput("iam.group.Delete", "tenant ID is required")
	}

	if groupID == "" {
		return errors.NewInvalidInput("iam.group.Delete", "group ID is required")
	}

	if ctx == nil {
		return errors.NewInvalidInput("iam.group.Delete", "context is required")
	}

	resp, err := c.transport.Request(ctx, groupServiceName, "Delete", &deleteGroupRequest{
		TenantID: tenantID,
		GroupID:  groupID,
	})
	if err != nil {
		return err
	}

	// Accept nil response or success response.
	if !isSuccessResponse(resp) {
		return errors.NewInternal("iam.group.Delete", "delete operation failed")
	}

	return nil
}

// List lists groups in a tenant with optional filtering.
func (c *groupClient) List(ctx context.Context, tenantID string, req *ListGroupsRequest) (*ListGroupsResponse, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.group.List", "tenant ID is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.group.List", "context is required")
	}

	if req == nil {
		req = &ListGroupsRequest{}
	}

	resp, err := c.transport.Request(ctx, groupServiceName, "List", &listGroupsRequestInternal{
		TenantID: tenantID,
		Request:  req,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListGroupsResponse{Groups: []Group{}}, nil
	}

	// If already a *ListGroupsResponse, return it directly.
	if listResp, ok := resp.(*ListGroupsResponse); ok {
		return listResp, nil
	}

	// Handle map[string]interface{} response from REST/gRPC transports.
	respMap, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("iam.group.List", "invalid response type")
	}

	result := &ListGroupsResponse{Groups: []Group{}}

	// Parse groups array.
	if groupsRaw, ok := respMap["groups"]; ok {
		switch groups := groupsRaw.(type) {
		case []interface{}:
			for _, g := range groups {
				if group, ok := convertToGroup(g); ok {
					result.Groups = append(result.Groups, *group)
				}
			}
		case []*Group:
			for _, g := range groups {
				if g != nil {
					result.Groups = append(result.Groups, *g)
				}
			}
		case []Group:
			result.Groups = groups
		}
	}

	// Parse pagination info.
	if paginationRaw, ok := respMap["pagination"]; ok {
		if pagination, ok := paginationRaw.(map[string]interface{}); ok {
			if v, ok := pagination["total_count"].(float64); ok {
				result.Pagination.TotalCount = int(v)
			} else if v, ok := pagination["total_count"].(int32); ok {
				result.Pagination.TotalCount = int(v)
			} else if v, ok := pagination["total_count"].(int64); ok {
				result.Pagination.TotalCount = int(v)
			}
			if v, ok := pagination["page_size"].(float64); ok {
				result.Pagination.PageSize = int(v)
			}
			if v, ok := pagination["next_page_token"].(string); ok {
				result.Pagination.NextPageToken = v
			}
		}
	}

	// Also handle direct limit/offset from REST API.
	if v, ok := respMap["limit"].(float64); ok {
		result.Pagination.PageSize = int(v)
	}
	if v, ok := respMap["total"].(float64); ok {
		result.Pagination.TotalCount = int(v)
	} else if v, ok := respMap["total"].(int32); ok {
		result.Pagination.TotalCount = int(v)
	} else if v, ok := respMap["total"].(int64); ok {
		result.Pagination.TotalCount = int(v)
	}

	return result, nil
}

// AddMember adds a user to a group.
func (c *groupClient) AddMember(ctx context.Context, tenantID, groupID, userID string) error {
	if tenantID == "" {
		return errors.NewInvalidInput("iam.group.AddMember", "tenant ID is required")
	}

	if groupID == "" {
		return errors.NewInvalidInput("iam.group.AddMember", "group ID is required")
	}

	if userID == "" {
		return errors.NewInvalidInput("iam.group.AddMember", "user ID is required")
	}

	if ctx == nil {
		return errors.NewInvalidInput("iam.group.AddMember", "context is required")
	}

	resp, err := c.transport.Request(ctx, groupServiceName, "AddMember", &groupMemberRequest{
		TenantID: tenantID,
		GroupID:  groupID,
		UserID:   userID,
	})
	if err != nil {
		return err
	}

	// Accept nil response or success response.
	if !isSuccessResponse(resp) {
		return errors.NewInternal("iam.group.AddMember", "add member operation failed")
	}

	return nil
}

// RemoveMember removes a user from a group.
func (c *groupClient) RemoveMember(ctx context.Context, tenantID, groupID, userID string) error {
	if tenantID == "" {
		return errors.NewInvalidInput("iam.group.RemoveMember", "tenant ID is required")
	}

	if groupID == "" {
		return errors.NewInvalidInput("iam.group.RemoveMember", "group ID is required")
	}

	if userID == "" {
		return errors.NewInvalidInput("iam.group.RemoveMember", "user ID is required")
	}

	if ctx == nil {
		return errors.NewInvalidInput("iam.group.RemoveMember", "context is required")
	}

	resp, err := c.transport.Request(ctx, groupServiceName, "RemoveMember", &groupMemberRequest{
		TenantID: tenantID,
		GroupID:  groupID,
		UserID:   userID,
	})
	if err != nil {
		return err
	}

	// Accept nil response or success response.
	if !isSuccessResponse(resp) {
		return errors.NewInternal("iam.group.RemoveMember", "remove member operation failed")
	}

	return nil
}

// ListMembers lists members of a group.
func (c *groupClient) ListMembers(ctx context.Context, tenantID, groupID string) (*ListMembersResponse, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.group.ListMembers", "tenant ID is required")
	}

	if groupID == "" {
		return nil, errors.NewInvalidInput("iam.group.ListMembers", "group ID is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.group.ListMembers", "context is required")
	}

	resp, err := c.transport.Request(ctx, groupServiceName, "ListMembers", &listMembersRequest{
		TenantID: tenantID,
		GroupID:  groupID,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListMembersResponse{Members: []GroupMember{}}, nil
	}

	// If already a *ListMembersResponse, return it directly.
	if listResp, ok := resp.(*ListMembersResponse); ok {
		return listResp, nil
	}

	// Handle map[string]interface{} response from REST/gRPC transports.
	respMap, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("iam.group.ListMembers", "invalid response type")
	}

	result := &ListMembersResponse{Members: []GroupMember{}}

	// Extract members array.
	if membersRaw, ok := respMap["members"]; ok {
		switch members := membersRaw.(type) {
		case []interface{}:
			for _, m := range members {
				if memberMap, ok := m.(map[string]interface{}); ok {
					member := GroupMember{}
					if v, ok := memberMap["user_id"].(string); ok {
						member.UserID = v
					}
					if v, ok := memberMap["username"].(string); ok {
						member.Username = v
					}
					if v, ok := memberMap["email"].(string); ok {
						member.Email = v
					}
					if v, ok := memberMap["display_name"].(string); ok {
						member.DisplayName = v
					}
					result.Members = append(result.Members, member)
				} else if memberStr, ok := m.(string); ok {
					// Proto returns members as string IDs.
					result.Members = append(result.Members, GroupMember{UserID: memberStr})
				}
			}
		case []string:
			// Proto returns members as string IDs.
			for _, memberID := range members {
				result.Members = append(result.Members, GroupMember{UserID: memberID})
			}
		}
	}

	return result, nil
}

// Validation functions.

func validateCreateGroupRequest(tenantID string, req *CreateGroupRequest) error {
	if tenantID == "" {
		return errors.NewInvalidInput("iam.group.Create", "tenant ID is required")
	}
	if req == nil {
		return errors.NewInvalidInput("iam.group.Create", "request is required")
	}
	if req.Name == "" {
		return errors.NewInvalidInput("iam.group.Create", "name is required")
	}
	return nil
}
