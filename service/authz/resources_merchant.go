package authz

const (
	ResourceMerchant = "merchant"

	ActionReview = "review"
)

var (
	MerchantRead   = Permission{Resource: ResourceMerchant, Action: ActionRead}
	MerchantReview = Permission{Resource: ResourceMerchant, Action: ActionReview}
	MerchantWrite  = Permission{Resource: ResourceMerchant, Action: ActionWrite}
)

func init() {
	RegisterResource(ResourceDefinition{
		Resource: ResourceMerchant,
		LabelKey: "Merchant Management",
		Actions: []ActionDefinition{
			{
				Action:         ActionRead,
				LabelKey:       "Read merchants & applications",
				DescriptionKey: "View merchant profiles, website credentials, and application queue.",
				DefaultRoles:   []string{BuiltInRoleAdmin},
			},
			{
				Action:             ActionReview,
				LabelKey:           "Review merchant applications",
				DescriptionKey:     "Approve or reject onboarding, shop name change, and channel expansion requests.",
				Sensitive:          true,
				DefaultAuditReason: true,
				DefaultRoles:       []string{BuiltInRoleAdmin},
			},
			{
				Action:             ActionWrite,
				LabelKey:           "Edit merchant policies",
				DescriptionKey:     "Modify merchant commission split, T+N settlement hold days, min withdrawal, or status.",
				Sensitive:          true,
				DefaultAuditReason: true,
				DefaultRoles:       []string{BuiltInRoleAdmin},
			},
		},
	})
}
