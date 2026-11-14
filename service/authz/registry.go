package authz

import "slices"

// ActionDefinition describes a single action exposed by a resource. DefaultRoles
// lists the role keys that receive this action as part of their baseline grants.
type ActionDefinition struct {
	Action             string   `json:"action"`
	LabelKey           string   `json:"label_key"`
	DescriptionKey     string   `json:"description_key"`
	Sensitive          bool     `json:"sensitive,omitempty"`
	DefaultAuditReason bool     `json:"default_audit_reason,omitempty"`
	DefaultRoles       []string `json:"-"`
}

// ResourceDefinition describes a resource and the actions it exposes.
type ResourceDefinition struct {
	Resource string             `json:"resource"`
	LabelKey string             `json:"label_key"`
	Actions  []ActionDefinition `json:"actions"`
}

var registry []ResourceDefinition

// RegisterResource adds a resource definition to the permission registry.
func RegisterResource(resource ResourceDefinition) {
	registry = append(registry, resource)
}

// Catalog returns a copy of the registered resource definitions.
func Catalog() []ResourceDefinition {
	result := make([]ResourceDefinition, 0, len(registry))
	for _, resource := range registry {
		result = append(result, ResourceDefinition{
			Resource: resource.Resource,
			LabelKey: resource.LabelKey,
			Actions:  append([]ActionDefinition(nil), resource.Actions...),
		})
	}
	return result
}

// AllPermissions returns every registered permission.
func AllPermissions() []Permission {
	permissions := make([]Permission, 0)
	for _, resource := range registry {
		for _, action := range resource.Actions {
			permissions = append(permissions, Permission{
				Resource: resource.Resource,
				Action:   action.Action,
			})
		}
	}
	return permissions
}

// PermissionsForRole returns the permissions whose DefaultRoles include roleKey.
func PermissionsForRole(roleKey string) []Permission {
	permissions := make([]Permission, 0)
	for _, resource := range registry {
		for _, action := range resource.Actions {
			if actionHasRole(action, roleKey) {
				permissions = append(permissions, Permission{
					Resource: resource.Resource,
					Action:   action.Action,
				})
			}
		}
	}
	return permissions
}

// DefaultAuditReasonBaseline returns the catalog's default audit-reason flags
// per resource/action.
func DefaultAuditReasonBaseline() PermissionsMap {
	result := make(PermissionsMap, len(registry))
	for _, resource := range registry {
		actions := make(map[string]bool, len(resource.Actions))
		for _, action := range resource.Actions {
			actions[action.Action] = action.DefaultAuditReason
		}
		result[resource.Resource] = actions
	}
	return result
}

// ResolveAuditReasonRules merges explicit user overrides with catalog defaults
// for the user's granted actions. Root users are exempt unless explicitly needed.
func ResolveAuditReasonRules(systemRole int, isRootRole int, explicit PermissionsMap) PermissionsMap {
	if systemRole >= isRootRole {
		return PermissionsMap{}
	}
	result := DefaultAuditReasonBaseline()
	for resource, actions := range explicit {
		if !isKnownResource(resource) {
			continue
		}
		if result[resource] == nil {
			result[resource] = make(map[string]bool)
		}
		for action, required := range actions {
			if isKnownPermission(Permission{Resource: resource, Action: action}) {
				result[resource][action] = required
			}
		}
	}
	return result
}

func actionHasRole(action ActionDefinition, roleKey string) bool {
	return slices.Contains(action.DefaultRoles, roleKey)
}

func isKnownResource(resource string) bool {
	for _, known := range registry {
		if known.Resource == resource {
			return true
		}
	}
	return false
}

func catalogActions(resource string) []ActionDefinition {
	for _, known := range registry {
		if known.Resource == resource {
			return known.Actions
		}
	}
	return nil
}

func isKnownPermission(permission Permission) bool {
	for _, resource := range registry {
		if resource.Resource != permission.Resource {
			continue
		}
		for _, action := range resource.Actions {
			if action.Action == permission.Action {
				return true
			}
		}
	}
	return false
}
