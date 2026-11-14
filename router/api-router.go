package router

import (
	"net/http"

	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/service/authz"

	// Import oauth package to register providers via init()
	_ "github.com/QuantumNous/new-api/oauth"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

func SetApiRouter(router *gin.Engine) {
	apiRouter := router.Group("/api")
	apiRouter.Use(middleware.RouteTag("api"))
	apiRouter.Use(gzip.Gzip(gzip.DefaultCompression))
	apiRouter.Use(middleware.AccessTokenAudit())
	apiRouter.Use(middleware.BodyStorageCleanup()) // 清理请求体存储
	apiRouter.Use(middleware.GlobalAPIRateLimit())
	anonymousRequestBodyLimit := middleware.AnonymousRequestBodyLimit()
	{
		apiRouter.GET("/setup", controller.GetSetup)
		apiRouter.POST("/setup", anonymousRequestBodyLimit, controller.PostSetup)
		apiRouter.GET("/status", controller.GetStatus)
		apiRouter.GET("/uptime/status", controller.GetUptimeKumaStatus)
		apiRouter.GET("/models", middleware.UserAuth(), controller.DashboardListModels)
		apiRouter.GET("/status/test", middleware.AdminAuth(), controller.TestStatus)
		apiRouter.GET("/notice", controller.GetNotice)
		apiRouter.GET("/user-agreement", controller.GetUserAgreement)
		apiRouter.GET("/privacy-policy", controller.GetPrivacyPolicy)
		apiRouter.GET("/about", controller.GetAbout)
		//apiRouter.GET("/midjourney", controller.GetMidjourney)
		apiRouter.GET("/home_page_content", controller.GetHomePageContent)
		apiRouter.GET("/pricing", middleware.HeaderNavModuleAuth("pricing"), controller.GetPricing)
		perfMetricsRoute := apiRouter.Group("/perf-metrics")
		perfMetricsRoute.Use(middleware.HeaderNavModulePublicOrUserAuth("pricing"))
		{
			perfMetricsRoute.GET("/summary", controller.GetPerfMetricsSummary)
			perfMetricsRoute.GET("", controller.GetPerfMetrics)
		}
		apiRouter.GET("/rankings", middleware.HeaderNavModuleAuth("rankings"), controller.GetRankings)
		// New API Pro：公开行情（K线与盘口总览）
		marketRoute := apiRouter.Group("/market")
		marketRoute.Use(middleware.HeaderNavModuleAuth("market"))
		{
			marketRoute.GET("/overview", controller.GetMarketOverview)
			marketRoute.GET("/kline", controller.GetMarketKline)
			marketRoute.GET("/orderbook", controller.GetMarketOrderBook)
		}
		apiRouter.GET("/verification", middleware.EmailVerificationRateLimit(), middleware.TurnstileCheck(), controller.SendEmailVerification)
		apiRouter.GET("/reset_password", middleware.CriticalRateLimit(), middleware.TurnstileCheck(), controller.SendPasswordResetEmail)
		apiRouter.POST("/user/reset", middleware.CriticalRateLimit(), anonymousRequestBodyLimit, controller.ResetPassword)
		// OAuth routes - specific routes must come before :provider wildcard
		apiRouter.POST("/oauth/state", middleware.CriticalRateLimit(), middleware.DisableCache(), middleware.TryUserAuth(), anonymousRequestBodyLimit, controller.GenerateOAuthCode)
		apiRouter.POST("/oauth/email/bind/start", middleware.UserAuth(), middleware.CriticalRateLimit(), middleware.UserCriticalRateLimit("account-security"), middleware.EmailVerificationRateLimit(), middleware.DisableCache(), controller.EmailBindStart)
		apiRouter.POST("/oauth/email/bind/resend", middleware.UserAuth(), middleware.CriticalRateLimit(), middleware.UserCriticalRateLimit("account-security"), middleware.EmailVerificationRateLimit(), middleware.DisableCache(), controller.EmailBindResend)
		apiRouter.POST("/oauth/email/bind", middleware.UserAuth(), middleware.CriticalRateLimit(), middleware.UserCriticalRateLimit("account-security"), middleware.DisableCache(), controller.EmailBind)
		// WeChat uses its existing authorization-code service.
		apiRouter.GET("/oauth/wechat", middleware.CriticalRateLimit(), middleware.DisableCache(), controller.WeChatAuth)
		apiRouter.POST("/oauth/wechat/bind", middleware.UserAuth(), middleware.CriticalRateLimit(), controller.WeChatBind)
		apiRouter.GET("/oauth/telegram/login", middleware.CriticalRateLimit(), middleware.DisableCache(), controller.TelegramLegacyAuth)
		apiRouter.POST("/oauth/telegram/bind/start", middleware.UserAuth(), middleware.CriticalRateLimit(), middleware.DisableCache(), controller.TelegramLegacyAuth)
		apiRouter.GET("/oauth/telegram/bind/:flow_token", middleware.CriticalRateLimit(), middleware.DisableCache(), controller.TelegramLegacyAuth)
		// Standard OAuth providers (GitHub, Discord, OIDC, LinuxDO, Telegram) - unified route
		apiRouter.GET("/oauth/:provider", middleware.CriticalRateLimit(), middleware.DisableCache(), middleware.TryUserAuth(), controller.HandleOAuth)
		apiRouter.GET("/ratio_config", middleware.CriticalRateLimit(), controller.GetRatioConfig)

		apiRouter.POST("/stripe/webhook", anonymousRequestBodyLimit, controller.StripeWebhook)
		apiRouter.POST("/creem/webhook", anonymousRequestBodyLimit, controller.CreemWebhook)
		apiRouter.POST("/waffo/webhook", anonymousRequestBodyLimit, controller.WaffoWebhook)
		// :env separates test vs prod URLs so the operator can register each
		// in Pancake's matching webhook slot; handler enforces env match.
		apiRouter.POST("/waffo-pancake/webhook/:env", anonymousRequestBodyLimit, controller.WaffoPancakeWebhook)

		// Universal secure verification routes
		apiRouter.GET("/verify/methods", middleware.UserAuth(), middleware.DisableCache(), controller.GetVerificationMethods)
		apiRouter.POST("/verify", middleware.UserAuth(), middleware.CriticalRateLimit(), middleware.UserCriticalRateLimit("security-verification"), middleware.DisableCache(), controller.UniversalVerify)

		userRoute := apiRouter.Group("/user")
		{
			userRoute.POST("/auth/refresh", middleware.SessionCookieOriginGuard(), middleware.CriticalRateLimit(), middleware.DisableCache(), controller.RefreshAuth)
			userRoute.POST("/auth/logout", middleware.SessionCookieOriginGuard(), middleware.CriticalRateLimit(), middleware.DisableCache(), controller.AuthLogout)
			userRoute.POST("/register", middleware.CriticalRateLimit(), anonymousRequestBodyLimit, middleware.TurnstileCheck(), controller.Register)
			userRoute.GET("/login/encryption-key", middleware.DisableCache(), controller.GetPasswordEncryptionKey)
			userRoute.POST("/login", middleware.CriticalRateLimit(), middleware.DisableCache(), anonymousRequestBodyLimit, middleware.TurnstileCheck(), controller.Login)
			userRoute.POST("/login/2fa", middleware.CriticalRateLimit(), middleware.DisableCache(), anonymousRequestBodyLimit, controller.Verify2FALogin)
			userRoute.POST("/login/verify", middleware.CriticalRateLimit(), middleware.DisableCache(), anonymousRequestBodyLimit, controller.VerifyLogin)
			userRoute.POST("/login/passkey/begin", middleware.CriticalRateLimit(), middleware.DisableCache(), anonymousRequestBodyLimit, controller.LoginPasskeyBegin)
			userRoute.POST("/login/passkey/finish", middleware.CriticalRateLimit(), middleware.DisableCache(), anonymousRequestBodyLimit, controller.LoginPasskeyFinish)
			userRoute.POST("/passkey/login/begin", middleware.CriticalRateLimit(), middleware.DisableCache(), anonymousRequestBodyLimit, controller.PasskeyLoginBegin)
			userRoute.POST("/passkey/login/finish", middleware.CriticalRateLimit(), middleware.DisableCache(), anonymousRequestBodyLimit, controller.PasskeyLoginFinish)
			//userRoute.POST("/tokenlog", middleware.CriticalRateLimit(), controller.TokenLog)
			userRoute.POST("/epay/notify", anonymousRequestBodyLimit, controller.EpayNotify)
			userRoute.GET("/epay/notify", controller.EpayNotify)
			userRoute.GET("/groups", controller.GetUserGroups)

			selfRoute := userRoute.Group("/")
			selfRoute.Use(middleware.DisableCache(), middleware.UserAuth())
			{
				selfRoute.GET("/sessions", middleware.DisableCache(), controller.GetLoginSessions)
				selfRoute.DELETE("/sessions/:sid", middleware.DisableCache(), controller.DeleteLoginSession)
				selfRoute.POST("/sessions/revoke-others", middleware.DisableCache(), controller.RevokeOtherLoginSessions)
				selfRoute.GET("/self/groups", controller.GetUserGroups)
				selfRoute.GET("/self", controller.GetSelf)
				selfRoute.GET("/models", controller.GetUserModels)
				selfRoute.PUT("/self", middleware.CriticalRateLimit(), middleware.DisableCache(), controller.UpdateSelf)
				selfRoute.DELETE("/self", middleware.DisableCache(), controller.DeleteSelf)
				accessTokenRoute := selfRoute.Group("/access_tokens")
				accessTokenRoute.Use(middleware.DisableCache())
				{
					accessTokenRoute.GET("", controller.ListAccessTokens)
					accessTokenRoute.GET("/catalog", controller.GetAccessTokenCatalog)
					accessTokenRoute.GET("/scopes", controller.GetAccessTokenScopes)
					accessTokenRoute.POST("", middleware.CriticalRateLimit(), middleware.UserCriticalRateLimit("access-token"), controller.CreateAccessToken)
					accessTokenRoute.PATCH("/:id", middleware.CriticalRateLimit(), middleware.UserCriticalRateLimit("access-token"), controller.UpdateAccessToken)
					accessTokenRoute.DELETE("/:id", middleware.CriticalRateLimit(), middleware.UserCriticalRateLimit("access-token"), controller.DeleteAccessToken)
					accessTokenRoute.DELETE("/legacy", middleware.CriticalRateLimit(), middleware.UserCriticalRateLimit("access-token"), controller.RevokeLegacyAccessToken)
				}
				selfRoute.GET("/passkey", controller.PasskeyStatus)
				selfRoute.POST("/passkey/register/begin", middleware.UserCriticalRateLimit("security-verification"), middleware.DisableCache(), controller.PasskeyRegisterBegin)
				selfRoute.POST("/passkey/register/finish", middleware.UserCriticalRateLimit("security-verification"), middleware.DisableCache(), controller.PasskeyRegisterFinish)
				selfRoute.POST("/passkey/verify/begin", middleware.UserCriticalRateLimit("security-verification"), middleware.DisableCache(), controller.PasskeyVerifyBegin)
				selfRoute.POST("/passkey/verify/finish", middleware.UserCriticalRateLimit("security-verification"), middleware.DisableCache(), controller.PasskeyVerifyFinish)
				selfRoute.DELETE("/passkey", middleware.DisableCache(), controller.PasskeyDelete)
				selfRoute.GET("/aff", controller.GetAffCode)
				selfRoute.GET("/topup/info", controller.GetTopUpInfo)
				selfRoute.GET("/topup/self", controller.GetUserTopUps)
				selfRoute.POST("/topup", middleware.CriticalRateLimit(), controller.TopUp)
				selfRoute.POST("/pay", middleware.CriticalRateLimit(), controller.RequestEpay)
				selfRoute.POST("/amount", controller.RequestAmount)
				selfRoute.POST("/stripe/pay", middleware.CriticalRateLimit(), controller.RequestStripePay)
				selfRoute.POST("/stripe/amount", controller.RequestStripeAmount)
				selfRoute.POST("/creem/pay", middleware.CriticalRateLimit(), controller.RequestCreemPay)
				selfRoute.POST("/waffo/amount", controller.RequestWaffoAmount)
				selfRoute.POST("/waffo/pay", middleware.CriticalRateLimit(), controller.RequestWaffoPay)
				selfRoute.POST("/waffo-pancake/amount", controller.RequestWaffoPancakeAmount)
				selfRoute.POST("/waffo-pancake/pay", middleware.CriticalRateLimit(), controller.RequestWaffoPancakePay)
				selfRoute.POST("/aff_transfer", middleware.UserCriticalRateLimit("aff-transfer"), controller.TransferAffQuota)
				selfRoute.PUT("/setting", controller.UpdateUserSetting)

				// 2FA routes
				selfRoute.GET("/2fa/status", controller.Get2FAStatus)
				selfRoute.POST("/2fa/setup", middleware.UserCriticalRateLimit("security-verification"), middleware.DisableCache(), controller.Setup2FA)
				selfRoute.POST("/2fa/enable", middleware.UserCriticalRateLimit("security-verification"), middleware.DisableCache(), controller.Enable2FA)
				selfRoute.POST("/2fa/disable", middleware.DisableCache(), controller.Disable2FA)
				selfRoute.POST("/2fa/backup_codes", middleware.DisableCache(), controller.RegenerateBackupCodes)

				// Check-in routes
				selfRoute.GET("/checkin", controller.GetCheckinStatus)
				selfRoute.POST("/checkin", middleware.TurnstileCheck(), controller.DoCheckin)

				// Custom OAuth bindings
				selfRoute.GET("/oauth/bindings", controller.GetUserOAuthBindings)
				selfRoute.DELETE("/oauth/bindings/:provider_id", controller.UnbindCustomOAuth)
			}

			adminRoute := userRoute.Group("/")
			adminRoute.Use(middleware.AdminAuth())
			{
				adminRoute.GET("/", middleware.RequirePermission(authz.UserRead), controller.GetAllUsers)
				adminRoute.GET("/topup", middleware.RequirePermission(authz.BillingRead), controller.GetAllTopUps)
				adminRoute.POST("/topup/complete", middleware.RequirePermission(authz.BillingWrite), controller.AdminCompleteTopUp)
				adminRoute.GET("/search", middleware.RequirePermission(authz.UserRead), controller.SearchUsers)
				adminRoute.GET("/:id/oauth/bindings", middleware.RequirePermission(authz.UserRead), controller.GetUserOAuthBindingsByAdmin)
				adminRoute.DELETE("/:id/oauth/bindings/:provider_id", middleware.RequirePermission(authz.UserWrite), controller.UnbindCustomOAuthByAdmin)
				adminRoute.DELETE("/:id/bindings/:binding_type", middleware.RequirePermission(authz.UserWrite), controller.AdminClearUserBinding)
				adminRoute.GET("/:id", middleware.RequirePermission(authz.UserRead), middleware.RequireAuditReason(authz.UserSecretView, middleware.DefaultAuditReasonMinLength), controller.GetUser)
				adminRoute.POST("/", middleware.RequirePermission(authz.UserWrite), controller.CreateUser)
				adminRoute.POST("/manage", middleware.RequirePermission(authz.UserSensitiveWrite), controller.ManageUser)
				adminRoute.PUT("/", middleware.RequirePermission(authz.UserWrite), controller.UpdateUser)
				adminRoute.DELETE("/:id", middleware.RequirePermission(authz.UserSensitiveWrite), middleware.RequireAuditReason(authz.UserSensitiveWrite, middleware.DefaultAuditReasonMinLength), controller.DeleteUser)
				adminRoute.DELETE("/:id/reset_passkey", middleware.RequirePermission(authz.UserSensitiveWrite), middleware.RequireAuditReason(authz.UserSensitiveWrite, middleware.DefaultAuditReasonMinLength), controller.AdminResetPasskey)

				// Admin 2FA routes
				adminRoute.GET("/2fa/stats", middleware.RequirePermission(authz.UserRead), controller.Admin2FAStats)
				adminRoute.DELETE("/:id/2fa", middleware.RequirePermission(authz.UserSensitiveWrite), middleware.RequireAuditReason(authz.UserSensitiveWrite, middleware.DefaultAuditReasonMinLength), controller.AdminDisable2FA)
			}
		}

		// Subscription billing (plans, purchase, admin management)
		subscriptionRoute := apiRouter.Group("/subscription")
		subscriptionRoute.Use(middleware.UserAuth())
		{
			subscriptionRoute.GET("/plans", controller.GetSubscriptionPlans)
			subscriptionRoute.GET("/self", controller.GetSubscriptionSelf)
			subscriptionRoute.PUT("/self/preference", controller.UpdateSubscriptionPreference)
			subscriptionRoute.POST("/balance/pay", middleware.CriticalRateLimit(), controller.SubscriptionRequestBalancePay)
			subscriptionRoute.POST("/epay/pay", middleware.CriticalRateLimit(), controller.SubscriptionRequestEpay)
			subscriptionRoute.POST("/stripe/pay", middleware.CriticalRateLimit(), controller.SubscriptionRequestStripePay)
			subscriptionRoute.POST("/creem/pay", middleware.CriticalRateLimit(), controller.SubscriptionRequestCreemPay)
			subscriptionRoute.POST("/waffo-pancake/pay", middleware.CriticalRateLimit(), controller.SubscriptionRequestWaffoPancakePay)
		}
		subscriptionAdminRoute := apiRouter.Group("/subscription/admin")
		subscriptionAdminRoute.Use(middleware.AdminAuth())
		{
			subscriptionAdminRoute.GET("/plans", middleware.RequirePermission(authz.BillingRead), controller.AdminListSubscriptionPlans)
			subscriptionAdminRoute.POST("/plans", middleware.RequirePermission(authz.BillingWrite), controller.AdminCreateSubscriptionPlan)
			subscriptionAdminRoute.PUT("/plans/:id", middleware.RequirePermission(authz.BillingWrite), controller.AdminUpdateSubscriptionPlan)
			subscriptionAdminRoute.PATCH("/plans/:id", middleware.RequirePermission(authz.BillingWrite), controller.AdminUpdateSubscriptionPlanStatus)
			subscriptionAdminRoute.POST("/bind", middleware.RequirePermission(authz.BillingWrite), controller.AdminBindSubscription)
			subscriptionAdminRoute.POST("/plans/:id/subscriptions/reset", middleware.RequirePermission(authz.BillingWrite), controller.AdminResetPlanSubscriptions)

			// User subscription management (admin)
			subscriptionAdminRoute.GET("/users/:id/subscriptions", middleware.RequirePermission(authz.BillingRead), controller.AdminListUserSubscriptions)
			subscriptionAdminRoute.POST("/users/:id/subscriptions", middleware.RequirePermission(authz.BillingWrite), controller.AdminCreateUserSubscription)
			subscriptionAdminRoute.POST("/users/:id/subscriptions/reset", middleware.RequirePermission(authz.BillingWrite), controller.AdminResetUserSubscriptionsByPlan)
			subscriptionAdminRoute.POST("/user_subscriptions/:id/invalidate", middleware.RequirePermission(authz.BillingWrite), controller.AdminInvalidateUserSubscription)
			subscriptionAdminRoute.DELETE("/user_subscriptions/:id", middleware.RequirePermission(authz.BillingWrite), controller.AdminDeleteUserSubscription)
		}

		// Subscription payment callbacks (no auth)
		apiRouter.POST("/subscription/epay/notify", anonymousRequestBodyLimit, controller.SubscriptionEpayNotify)
		apiRouter.GET("/subscription/epay/notify", controller.SubscriptionEpayNotify)
		apiRouter.GET("/subscription/epay/return", controller.SubscriptionEpayReturn)
		apiRouter.POST("/subscription/epay/return", anonymousRequestBodyLimit, controller.SubscriptionEpayReturn)
		optionRoute := apiRouter.Group("/option")
		optionRoute.Use(middleware.RootAuth())
		{
			optionRoute.GET("/", controller.GetOptions)
			optionRoute.GET("/request_policy", controller.GetRequestPolicy)
			optionRoute.PATCH("/request_policy", controller.UpdateRequestPolicy)
			optionRoute.PUT("/", controller.UpdateOption)
			optionRoute.PUT("/passkey/domains", controller.UpdatePasskeyDomains)
			optionRoute.GET("/model_pricing", controller.GetModelPricingConfig)
			optionRoute.GET("/model_pricing/issues", controller.GetModelPricingIssues)
			optionRoute.PATCH("/model_pricing", controller.UpdateModelPricingConfig)
			optionRoute.POST("/model_pricing/convert", controller.PreviewModelPricingConversion)
			optionRoute.POST("/model_pricing/preview", controller.PreviewModelPricing)
			optionRoute.POST("/payment_compliance", controller.ConfirmPaymentCompliance)
			optionRoute.GET("/channel_affinity_cache", controller.GetChannelAffinityCacheStats)
			optionRoute.DELETE("/channel_affinity_cache", controller.ClearChannelAffinityCache)
			optionRoute.POST("/rest_model_ratio", controller.ResetModelRatio)
			optionRoute.GET("/waffo-pancake/catalog", controller.ListWaffoPancakeCatalog)
			optionRoute.POST("/waffo-pancake/pair", controller.CreateWaffoPancakePair)
			optionRoute.POST("/waffo-pancake/save", controller.SaveWaffoPancake)
			optionRoute.POST("/waffo-pancake/subscription-product", controller.CreateWaffoPancakeSubscriptionProduct)
			optionRoute.GET("/waffo-pancake/subscription-product-options", controller.ListWaffoPancakeSubscriptionProductOptions)
		}

		// Custom OAuth provider management (root only)
		customOAuthRoute := apiRouter.Group("/custom-oauth-provider")
		customOAuthRoute.Use(middleware.RootAuth())
		{
			customOAuthRoute.POST("/discovery", controller.FetchCustomOAuthDiscovery)
			customOAuthRoute.GET("/", controller.GetCustomOAuthProviders)
			customOAuthRoute.GET("/:id", controller.GetCustomOAuthProvider)
			customOAuthRoute.POST("/", controller.CreateCustomOAuthProvider)
			customOAuthRoute.PUT("/:id", controller.UpdateCustomOAuthProvider)
			customOAuthRoute.DELETE("/:id", controller.DeleteCustomOAuthProvider)
		}
		performanceRoute := apiRouter.Group("/performance")
		performanceRoute.Use(middleware.RootAuth())
		{
			performanceRoute.GET("/stats", controller.GetPerformanceStats)
			performanceRoute.DELETE("/disk_cache", controller.ClearDiskCache)
			performanceRoute.POST("/reset_stats", controller.ResetPerformanceStats)
			performanceRoute.POST("/gc", controller.ForceGC)
			performanceRoute.GET("/logs", controller.GetLogFiles)
			performanceRoute.DELETE("/logs", controller.CleanupLogFiles)
		}
		ratioSyncRoute := apiRouter.Group("/ratio_sync")
		ratioSyncRoute.Use(middleware.RootAuth())
		{
			ratioSyncRoute.GET("/channels", controller.GetSyncableChannels)
			ratioSyncRoute.POST("/fetch", controller.FetchUpstreamRatios)
		}
		// New API Pro：盘口报价管理
		channelQuoteRoute := apiRouter.Group("/channel_quotes")
		channelQuoteRoute.Use(middleware.AdminAuth())
		{
			channelQuoteRoute.GET("/", controller.GetChannelQuotes)
			channelQuoteRoute.PUT("/", controller.UpsertManualQuote)
			channelQuoteRoute.DELETE("/:id", controller.DeleteChannelQuote)
			channelQuoteRoute.POST("/sync", controller.SyncChannelQuotes)
		}
		// System Self-Update (Custom Branch / Release)
		systemUpdateRoute := apiRouter.Group("/system_update")
		systemUpdateRoute.Use(middleware.RootAuth())
		{
			systemUpdateRoute.GET("/status", controller.GetSelfUpdateStatus)
			systemUpdateRoute.POST("/apply", controller.ApplySelfUpdate)
		}
		// New API Pro：商家入驻（登录用户）与商家门户（商家角色，数据隔离）
		merchantRoute := apiRouter.Group("/merchant")
		merchantRoute.Use(middleware.UserAuth())
		{
			merchantRoute.POST("/apply", controller.PostMerchantApplication)
			// 外呼探测会以服务器身份访问商家填写的站址，叠加用户级限频防滥用
			//（默认 20 次/20 分钟；另有全局 IP 限频兜底）。
			merchantRoute.POST("/apply/probe", middleware.UserCriticalRateLimit("merchant-probe"), controller.ProbeMerchantApplyUpstream)
			merchantRoute.GET("/platform/models", controller.GetMerchantPlatformModels)
			merchantRoute.GET("/application", controller.GetMyMerchantApplication)
		}
		merchantPortalRoute := apiRouter.Group("/merchant/portal")
		merchantPortalRoute.Use(middleware.MerchantAuth())
		{
			merchantPortalRoute.GET("/summary", controller.GetMerchantPortalSummary)
			merchantPortalRoute.GET("/pools", controller.GetMerchantPortalPools)
			merchantPortalRoute.GET("/earnings", controller.GetMerchantPortalEarnings)
			merchantPortalRoute.GET("/quotes", controller.GetMerchantPortalQuotes)
			merchantPortalRoute.PUT("/quotes", controller.UpdateMerchantPortalQuotes)
			merchantPortalRoute.GET("/bidding", controller.GetMerchantPortalBidding)
			merchantPortalRoute.PUT("/rate", controller.UpdateMerchantPortalRate)
			// 外呼接口：报价同步与渠道探测都以服务器身份访问上游，叠加用户级限频。
			merchantPortalRoute.POST("/sync", middleware.UserCriticalRateLimit("merchant-sync"), controller.SyncMerchantPortalQuotes)
			merchantPortalRoute.POST("/channel/probe", middleware.UserCriticalRateLimit("merchant-probe"), controller.ProbeMerchantChannel)
			merchantPortalRoute.PUT("/channel", controller.UpdateMerchantChannel)
			merchantPortalRoute.GET("/logs", controller.GetMerchantPortalLogs)
			merchantPortalRoute.GET("/withdrawals", controller.GetMerchantWithdrawals)
			merchantPortalRoute.POST("/withdrawals", controller.PostMerchantWithdrawal)
			merchantPortalRoute.GET("/profile", controller.GetMerchantPortalProfile)
			merchantPortalRoute.PUT("/profile", controller.UpdateMerchantPortalProfile)
		}
		// New API Pro：商家管理（管理员）
		merchantAdminRoute := apiRouter.Group("/merchant_manage")
		merchantAdminRoute.Use(middleware.AdminAuth())
		{
			handlePermissionRoute(merchantAdminRoute, http.MethodGet, "/applications", authz.MerchantRead, controller.GetMerchantApplications)
			handlePermissionRoute(merchantAdminRoute, http.MethodPost, "/applications/:id/review", authz.MerchantReview, middleware.RequireAuditReason(authz.MerchantReview, middleware.DefaultAuditReasonMinLength), controller.ReviewMerchantApplication)
			handlePermissionRoute(merchantAdminRoute, http.MethodPost, "/applications/:id/probe", authz.MerchantRead, controller.ProbeMerchantApplication)
			handlePermissionRoute(merchantAdminRoute, http.MethodGet, "/list", authz.MerchantRead, controller.GetMerchants)
			handlePermissionRoute(merchantAdminRoute, http.MethodGet, "/overview", authz.MerchantRead, controller.GetMerchantOverview)
			handlePermissionRoute(merchantAdminRoute, http.MethodPut, "/:id", authz.MerchantWrite, middleware.RequireAuditReason(authz.MerchantWrite, middleware.DefaultAuditReasonMinLength), controller.UpdateMerchant)
			handlePermissionRoute(merchantAdminRoute, http.MethodGet, "/withdrawals", authz.FinanceRead, controller.AdminGetMerchantWithdrawals)
			handlePermissionRoute(merchantAdminRoute, http.MethodPost, "/withdrawals/:id/review", authz.FinancePayout, middleware.RequireAuditReason(authz.FinancePayout, middleware.DefaultAuditReasonMinLength), controller.ReviewMerchantWithdrawal)
		}
		// New API Pro：后台财务中心（财务专员 / 管理员）
		financeRoute := apiRouter.Group("/finance")
		financeRoute.Use(middleware.FinanceAuth())
		{
			handlePermissionRoute(financeRoute, http.MethodGet, "/overview", authz.FinanceRead, controller.GetFinanceOverview)
			handlePermissionRoute(financeRoute, http.MethodGet, "/topups", authz.FinanceRead, controller.GetFinanceTopUps)
			handlePermissionRoute(financeRoute, http.MethodGet, "/subscriptions", authz.FinanceRead, controller.GetFinanceSubscriptions)
			handlePermissionRoute(financeRoute, http.MethodGet, "/redemptions", authz.FinanceRead, controller.GetFinanceRedemptionSummary)
			handlePermissionRoute(financeRoute, http.MethodGet, "/earnings", authz.FinanceRead, controller.GetFinanceMerchantEarnings)
			handlePermissionRoute(financeRoute, http.MethodPost, "/earnings/settle", authz.FinanceSettle, middleware.RequireAuditReason(authz.FinanceSettle, middleware.DefaultAuditReasonMinLength), controller.PostFinanceSettleDueEarnings)
			handlePermissionRoute(financeRoute, http.MethodPost, "/earnings/:id/reverse", authz.FinanceSettle, middleware.RequireAuditReason(authz.FinanceSettle, middleware.DefaultAuditReasonMinLength), controller.PostFinanceReverseEarning)
			handlePermissionRoute(financeRoute, http.MethodGet, "/ledger", authz.FinanceRead, controller.GetFinanceLedgers)
			handlePermissionRoute(financeRoute, http.MethodGet, "/ledger/export", authz.FinanceRead, controller.ExportFinanceLedgersCSV)
			handlePermissionRoute(financeRoute, http.MethodGet, "/reconcile", authz.FinanceRead, controller.GetFinanceReconciliation)
			handlePermissionRoute(financeRoute, http.MethodGet, "/withdrawals", authz.FinanceRead, controller.AdminGetMerchantWithdrawals)
			handlePermissionRoute(financeRoute, http.MethodPost, "/withdrawals/:id/review", authz.FinancePayout, middleware.RequireAuditReason(authz.FinancePayout, middleware.DefaultAuditReasonMinLength), controller.ReviewMerchantWithdrawal)
		}
		taskPluginRoute := apiRouter.Group("/plugin/task")
		taskPluginRoute.Use(middleware.RootAuth())
		{
			taskPluginRoute.GET("", controller.ListTaskPlugins)
			taskPluginRoute.POST("", controller.UploadTaskPlugin)
			taskPluginRoute.PUT("", controller.UploadTaskPlugin)
			taskPluginRoute.GET("/runtime/status", controller.GetTaskPluginRuntime)
			taskPluginRoute.GET("/marketplace/sources", controller.GetTaskPluginMarketplaceSources)
			taskPluginRoute.PUT("/marketplace/sources", controller.UpdateTaskPluginMarketplaceSources)
			taskPluginRoute.GET("/:key", controller.GetTaskPlugin)
			taskPluginRoute.GET("/:key/icon", controller.GetTaskPluginIcon)
			taskPluginRoute.GET("/:key/versions", controller.GetTaskPluginVersions)
			taskPluginRoute.POST("/:key/activate", controller.ActivateTaskPlugin)
			taskPluginRoute.POST("/:key/status", controller.SetTaskPluginStatus)
			taskPluginRoute.POST("/:key/dryrun", controller.DryRunTaskPlugin)
			taskPluginRoute.DELETE("/:key/versions/:version", controller.DeleteTaskPluginVersion)
		}
		handlePermissionRoute(apiRouter.Group("", middleware.AdminAuth()), http.MethodGet, "/task_plugin_options", authz.TaskPluginBind, controller.GetTaskPluginOptions)
		// Channel editors read the instances to bind them; changing them is
		// plugin management, as installing a plugin is.
		handlePermissionRoute(apiRouter.Group("", middleware.AdminAuth()), http.MethodGet, "/request_plugin/instances", authz.TaskPluginBind, controller.GetRequestPluginInstances)
		requestPluginRoute := apiRouter.Group("/request_plugin")
		requestPluginRoute.Use(middleware.RootAuth())
		{
			requestPluginRoute.PUT("/instances", controller.UpdateRequestPluginInstances)
			requestPluginRoute.POST("/dry_run", controller.DryRunRequestPlugins)
		}
		registerChannelRoutes(apiRouter)
		registerAuthzRoutes(apiRouter)
		tokenRoute := apiRouter.Group("/token")
		tokenRoute.Use(middleware.UserAuth())
		tokenRoute.Use(middleware.TokenOperationAudit())
		{
			tokenRoute.GET("/", controller.GetAllTokens)
			tokenRoute.GET("/search", middleware.SearchRateLimit(), controller.SearchTokens)
			tokenRoute.GET("/auto-groups", controller.GetTokenAutoGroups)
			tokenRoute.GET("/:id", controller.GetToken)
			tokenRoute.POST("/:id/key", middleware.CriticalRateLimit(), middleware.DisableCache(), controller.GetTokenKey)
			tokenRoute.POST("/", controller.AddToken)
			tokenRoute.PUT("/", controller.UpdateToken)
			tokenRoute.DELETE("/:id", controller.DeleteToken)
			tokenRoute.POST("/batch", controller.DeleteTokenBatch)
			tokenRoute.POST("/batch/keys", middleware.CriticalRateLimit(), middleware.DisableCache(), controller.GetTokenKeysBatch)
		}

		usageRoute := apiRouter.Group("/usage")
		usageRoute.Use(middleware.CORS(), middleware.CriticalRateLimit())
		{
			tokenUsageRoute := usageRoute.Group("/token")
			tokenUsageRoute.Use(middleware.TokenAuthReadOnly())
			{
				tokenUsageRoute.GET("/", controller.GetTokenUsage)
			}
		}

		redemptionRoute := apiRouter.Group("/redemption")
		redemptionRoute.Use(middleware.AdminAuth())
		{
			redemptionRoute.GET("/", middleware.RequirePermission(authz.RedemptionRead), controller.GetAllRedemptions)
			redemptionRoute.GET("/search", middleware.RequirePermission(authz.RedemptionRead), controller.SearchRedemptions)
			redemptionRoute.GET("/:id", middleware.RequirePermission(authz.RedemptionRead), controller.GetRedemption)
			redemptionRoute.POST("/", middleware.RequirePermission(authz.RedemptionWrite), controller.AddRedemption)
			redemptionRoute.POST("/batch", middleware.RequirePermission(authz.RedemptionWrite), controller.DeleteRedemptionBatch)
			redemptionRoute.PUT("/", middleware.RequirePermission(authz.RedemptionWrite), controller.UpdateRedemption)
			redemptionRoute.DELETE("/invalid", middleware.RequirePermission(authz.RedemptionWrite), controller.DeleteInvalidRedemption)
			redemptionRoute.DELETE("/:id", middleware.RequirePermission(authz.RedemptionWrite), controller.DeleteRedemption)
		}
		handlePermissionRoute(apiRouter.Group("", middleware.DisableCache(), middleware.AdminAuth()), http.MethodGet, "/audit", authz.AuditRead, controller.GetAuditLogs)
		apiRouter.GET("/audit/self", middleware.DisableCache(), middleware.UserAuth(), controller.GetAuditLogs)
		logRoute := apiRouter.Group("/log")
		logRoute.GET("/", middleware.AdminAuth(), controller.GetAllLogs)
		logRoute.GET("/stat", middleware.AdminAuth(), controller.GetLogsStat)
		logRoute.GET("/self/stat", middleware.UserAuth(), controller.GetLogsSelfStat)
		logRoute.GET("/channel_affinity_usage_cache", middleware.AdminAuth(), controller.GetChannelAffinityUsageCacheStats)
		logRoute.GET("/search", middleware.AdminAuth(), controller.SearchAllLogs)
		logRoute.GET("/self", middleware.UserAuth(), controller.GetUserLogs)
		logRoute.GET("/export", middleware.AdminAuth(), controller.ExportAllLogs)
		logRoute.GET("/self/export", middleware.UserAuth(), controller.ExportUserLogs)
		logRoute.GET("/self/search", middleware.UserAuth(), middleware.SearchRateLimit(), controller.SearchUserLogs)

		systemTaskRoute := apiRouter.Group("/system-task")
		systemTaskRoute.Use(middleware.RootAuth())
		{
			systemTaskRoute.POST("/log-cleanup", controller.CreateLogCleanupSystemTask)
			systemTaskRoute.GET("/list", controller.ListSystemTasks)
			systemTaskRoute.DELETE("/history", controller.DeleteSystemTaskHistory)
			systemTaskRoute.GET("/current", controller.GetCurrentSystemTask)
			systemTaskRoute.GET("/:task_id", controller.GetSystemTask)
		}
		systemInfoRoute := apiRouter.Group("/system-info")
		systemInfoRoute.Use(middleware.RootAuth())
		{
			systemInfoRoute.GET("/instances", controller.ListSystemInstances)
			systemInfoRoute.GET("/sync", controller.GetNodeSync)
			systemInfoRoute.DELETE("/stale-instances", controller.DeleteStaleSystemInstances)
			systemInfoRoute.DELETE("/instances/:node_name", controller.DeleteStaleSystemInstance)
		}

		dataRoute := apiRouter.Group("/data")
		dataRoute.GET("/", middleware.AdminAuth(), controller.GetAllQuotaDates)
		dataRoute.GET("/users", middleware.AdminAuth(), controller.GetQuotaDatesByUser)
		dataRoute.GET("/self", middleware.UserAuth(), controller.GetUserQuotaDates)
		dataRoute.GET("/flow", middleware.AdminAuth(), controller.GetAllFlowQuotaDates)
		dataRoute.GET("/flow/self", middleware.UserAuth(), controller.GetUserFlowQuotaDates)

		logRoute.Use(middleware.CORS(), middleware.CriticalRateLimit())
		{
			logRoute.GET("/token", middleware.TokenAuthReadOnly(), controller.GetLogByKey)
		}
		groupRoute := apiRouter.Group("/group")
		groupRoute.Use(middleware.AdminAuth())
		{
			groupRoute.GET("/", controller.GetGroups)
		}

		prefillGroupRoute := apiRouter.Group("/prefill_group")
		prefillGroupRoute.Use(middleware.AdminAuth())
		{
			prefillGroupRoute.GET("/", controller.GetPrefillGroups)
			prefillGroupRoute.POST("/", controller.CreatePrefillGroup)
			prefillGroupRoute.PUT("/", controller.UpdatePrefillGroup)
			prefillGroupRoute.DELETE("/:id", controller.DeletePrefillGroup)
		}

		mjRoute := apiRouter.Group("/mj")
		mjRoute.GET("/self", middleware.UserAuth(), controller.GetUserMidjourney)
		mjRoute.GET("/", middleware.AdminAuth(), controller.GetAllMidjourney)

		taskRoute := apiRouter.Group("/task")
		{
			taskRoute.GET("/self", middleware.UserAuth(), controller.GetUserTask)
			taskRoute.GET("", middleware.AdminAuth(), controller.GetAllTask)
			taskRoute.GET("/:task_id/artifacts", middleware.UserAuth(), controller.GetDashboardTaskArtifacts)
		}

		vendorRoute := apiRouter.Group("/vendors")
		vendorRoute.Use(middleware.AdminAuth())
		{
			vendorRoute.POST("/operations/preview", controller.PreviewVendorOperation)
			vendorRoute.POST("/operations", controller.ApplyVendorOperation)
			vendorRoute.GET("/", controller.GetAllVendors)
			vendorRoute.GET("/search", controller.SearchVendors)
			vendorRoute.GET("/:id", controller.GetVendorMeta)
			vendorRoute.POST("/", controller.CreateVendorMeta)
			vendorRoute.PUT("/", controller.UpdateVendorMeta)
			vendorRoute.DELETE("/:id", controller.DeleteVendorMeta)
		}

		modelsRoute := apiRouter.Group("/models")
		modelsRoute.Use(middleware.AdminAuth())
		{
			modelsRoute.GET("/sync_upstream/preview", controller.SyncUpstreamPreview)
			modelsRoute.POST("/sync_upstream", controller.SyncUpstreamModels)
			modelsRoute.POST("/delete", controller.BatchDeleteModelMeta)
			modelsRoute.GET("/missing", controller.GetMissingModels)
			modelsRoute.GET("/", controller.GetAllModelsMeta)
			modelsRoute.GET("/search", controller.SearchModelsMeta)
			modelsRoute.GET("/:id", controller.GetModelMeta)
			modelsRoute.POST("/", controller.CreateModelMeta)
			modelsRoute.PUT("/", controller.UpdateModelMeta)
			modelsRoute.DELETE("/:id", controller.DeleteModelMeta)
		}

		// Deployments (model deployment management)
		deploymentsRoute := apiRouter.Group("/deployments")
		deploymentsRoute.Use(middleware.AdminAuth())
		{
			deploymentsRoute.GET("/settings", controller.GetModelDeploymentSettings)
			deploymentsRoute.POST("/settings/test-connection", controller.TestIoNetConnection)
			deploymentsRoute.GET("/", controller.GetAllDeployments)
			deploymentsRoute.GET("/search", controller.SearchDeployments)
			deploymentsRoute.POST("/test-connection", controller.TestIoNetConnection)
			deploymentsRoute.GET("/hardware-types", controller.GetHardwareTypes)
			deploymentsRoute.GET("/locations", controller.GetLocations)
			deploymentsRoute.GET("/available-replicas", controller.GetAvailableReplicas)
			deploymentsRoute.POST("/price-estimation", controller.GetPriceEstimation)
			deploymentsRoute.GET("/check-name", controller.CheckClusterNameAvailability)
			deploymentsRoute.POST("/", controller.CreateDeployment)

			deploymentsRoute.GET("/:id", controller.GetDeployment)
			deploymentsRoute.GET("/:id/logs", controller.GetDeploymentLogs)
			deploymentsRoute.GET("/:id/containers", controller.ListDeploymentContainers)
			deploymentsRoute.GET("/:id/containers/:container_id", controller.GetContainerDetails)
			deploymentsRoute.PUT("/:id", controller.UpdateDeployment)
			deploymentsRoute.PUT("/:id/name", controller.UpdateDeploymentName)
			deploymentsRoute.POST("/:id/extend", controller.ExtendDeployment)
			deploymentsRoute.DELETE("/:id", controller.DeleteDeployment)
		}
	}
}
