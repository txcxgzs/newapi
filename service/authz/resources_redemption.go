package authz

const ResourceRedemption = "redemption"

var (
	RedemptionRead  = Permission{Resource: ResourceRedemption, Action: ActionRead}
	RedemptionWrite = Permission{Resource: ResourceRedemption, Action: ActionWrite}
)

func init() {
	RegisterResource(ResourceDefinition{
		Resource: ResourceRedemption,
		LabelKey: "Redemption codes",
		Actions: []ActionDefinition{
			{
				Action:         ActionRead,
				LabelKey:       "View redemption codes",
				DescriptionKey: "View generated redemption codes and usage status.",
				DefaultRoles:   []string{BuiltInRoleAdmin},
			},
			{
				Action:             ActionWrite,
				LabelKey:           "Manage redemption codes",
				DescriptionKey:     "Create, edit, and delete redemption codes.",
				Sensitive:          true,
				DefaultAuditReason: false,
				DefaultRoles:       []string{BuiltInRoleAdmin},
			},
		},
	})
}
