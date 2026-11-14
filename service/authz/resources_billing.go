package authz

const ResourceBilling = "billing"

var (
	BillingRead  = Permission{Resource: ResourceBilling, Action: ActionRead}
	BillingWrite = Permission{Resource: ResourceBilling, Action: ActionWrite}
)

func init() {
	RegisterResource(ResourceDefinition{
		Resource: ResourceBilling,
		LabelKey: "Billing management",
		Actions: []ActionDefinition{
			{
				Action:         ActionRead,
				LabelKey:       "View billing & subscriptions",
				DescriptionKey: "View all top-up records, subscription plans, and user subscriptions.",
				DefaultRoles:   []string{BuiltInRoleFinance, BuiltInRoleAdmin},
			},
			{
				Action:             ActionWrite,
				LabelKey:           "Manage top-ups & subscriptions",
				DescriptionKey:     "Complete top-up orders and manage subscription plans and user subscriptions.",
				Sensitive:          true,
				DefaultAuditReason: true,
				DefaultRoles:       []string{BuiltInRoleAdmin},
			},
		},
	})
}
