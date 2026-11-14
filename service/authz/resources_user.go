package authz

const ResourceUser = "user"

var (
	UserRead           = Permission{Resource: ResourceUser, Action: ActionRead}
	UserWrite          = Permission{Resource: ResourceUser, Action: ActionWrite}
	UserSensitiveWrite = Permission{Resource: ResourceUser, Action: ActionSensitiveWrite}
	UserSecretView     = Permission{Resource: ResourceUser, Action: ActionSecretView}
)

func init() {
	RegisterResource(ResourceDefinition{
		Resource: ResourceUser,
		LabelKey: "Users",
		Actions: []ActionDefinition{
			{
				Action:         ActionRead,
				LabelKey:       "Read users",
				DescriptionKey: "View user list and basic profile summaries.",
				DefaultRoles:   []string{BuiltInRoleAdmin},
			},
			{
				Action:             ActionWrite,
				LabelKey:           "Edit users",
				DescriptionKey:     "Create or edit basic user information and notes.",
				Sensitive:          true,
				DefaultAuditReason: true,
				DefaultRoles:       []string{BuiltInRoleAdmin},
			},
			{
				Action:             ActionSensitiveWrite,
				LabelKey:           "Manage user credentials & quotas",
				DescriptionKey:     "Reset user passwords, adjust quota balances, and enable/disable accounts.",
				Sensitive:          true,
				DefaultAuditReason: true,
				DefaultRoles:       []string{BuiltInRoleAdmin},
			},
			{
				Action:             ActionSecretView,
				LabelKey:           "View user details & bindings",
				DescriptionKey:     "Open detailed user profiles, sign-in bindings, and permission configurations.",
				Sensitive:          true,
				DefaultAuditReason: true,
				DefaultRoles:       []string{BuiltInRoleAdmin},
			},
		},
	})
}
