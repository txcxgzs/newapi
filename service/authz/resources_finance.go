package authz

const (
	ResourceFinance = "finance"

	ActionSettle = "settle"
	ActionPayout = "payout"
)

var (
	FinanceRead   = Permission{Resource: ResourceFinance, Action: ActionRead}
	FinanceSettle = Permission{Resource: ResourceFinance, Action: ActionSettle}
	FinancePayout = Permission{Resource: ResourceFinance, Action: ActionPayout}
)

func init() {
	RegisterResource(ResourceDefinition{
		Resource: ResourceFinance,
		LabelKey: "Finance Center",
		Actions: []ActionDefinition{
			{
				Action:         ActionRead,
				LabelKey:       "View financial reports & ledgers",
				DescriptionKey: "View revenue overview, top-up orders, reconciliation, and export ledgers.",
				DefaultRoles:   []string{BuiltInRoleFinance, BuiltInRoleAdmin},
			},
			{
				Action:             ActionSettle,
				LabelKey:           "Settle & reverse earnings",
				DescriptionKey:     "Manually settle due T+N merchant earnings and reverse erroneous earnings.",
				Sensitive:          true,
				DefaultAuditReason: true,
				DefaultRoles:       []string{BuiltInRoleFinance},
			},
			{
				Action:             ActionPayout,
				LabelKey:           "Review & pay withdrawals",
				DescriptionKey:     "Approve or reject merchant withdrawal requests and confirm payout completion.",
				Sensitive:          true,
				DefaultAuditReason: true,
				DefaultRoles:       []string{BuiltInRoleFinance},
			},
		},
	})
}
