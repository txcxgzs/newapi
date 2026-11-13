# MERGE-NOTES：与上游 new-api 合并的笔记

本仓库是 new-api 的魔改版。每次与上游同步后，把"手工解决点"记录在这里，下次合并直接照抄。

## 标准流程

1. 工作区干净时打快照：`git tag snapshot-YYYY-MM-DD`
2. 沙箱预演（不碰主仓库工作区）：
   `git worktree add --detach "$env:TEMP\opencode\na-merge" snapshot 标签对应的提交`
   在沙箱里 `git fetch origin` → `git merge origin/main`，解决冲突并验证
3. 验证三件套（沙箱和真仓都跑）：
   - `$env:GOTOOLCHAIN="go1.26.0"; go build ./...`
   - `go test ./service/ -run 'Gift|Merchant' -count=1`
   - `go test ./controller/ -run 'TestSecurityAccount|UserManage|UserUpdate|Quota' -count=1`
   - 全仓测试编译扫描：`go test ./... -run zzz_none_zzz -count=1`（只看 build failed）
4. 真仓合入：`git merge --ff-only 沙箱最终提交哈希`（或直接 `git merge origin/main` 后照抄下面的解决点）
5. 重建：`cd web; npm install; npm run build` → 回根目录 `go build -o new-api-pro.exe .`
6. 冒烟：启动 exe，确认登录 / 赠金 / 商家页面正常，然后回滚数据库备份如不需保留

## 回滚

`git reset --hard snapshot-YYYY-MM-DD`，然后重编 exe。

## 命令速查

- 工具链：`$env:GOTOOLCHAIN="go1.26.0"`（go.mod 要求 go 1.26，裸写 go1.26 会下载失败）
- 代理：`$env:GOPROXY="https://goproxy.cn,direct"`（sum.golang.google.cn 偶发 EOF，重试即可）
- 前端用 npm（本机未装 bun）：`npm install && npm run build`
- 旧 exe 参数：CGO_ENABLED=0，纯 `go build -o new-api-pro.exe .`，无 ldflags
- 备份数据库：关闭程序后复制 `one-api.db`（含 -wal/-shm，如有）

## 2026-10-17 合并 upstream 09109ec（落后 94 个提交）

### 总原则

功能逻辑保留我们的；写法/文案跟随上游（`LogText`、`ApiErrorT`、`[]*common.Message`），让魔改代码越来越像上游，下次冲突越来越小。

### 冲突 6 处（手动解决）

| 文件 | 处理 |
|---|---|
| controller/midjourney.go | 保留"SUCCESS 不杀单 + 商家收益"，文案用上游英文串 |
| model/log.go | 保留 RecordConsumeLog 返回 logID + 商家函数；采用上游消息结构与 LogText/Stream |
| relay/helper/price.go | import 两边都要（常量 + i18n） |
| service/billing_session.go | import 两边都要 |
| service/midjourney.go | 保留赠金退款分支；钱包分支用上游 LogText |
| web/package.json | 保留 @visactor/vchart-extension ^2.1.7、ai ^7.0.136 |

### 编译/测试修复 4 处

1. `controller/system_task_handlers.go`：补 `"fmt"` import（上游 i18n 重构删了它，我们的价格同步还在用）
2. `controller/user_quota.go`：4 个已删除的 `i18n.Msg*` 常量 → `common.ApiErrorT(c, "英文原句")`
3. `controller/user_manage_test.go:340`：`GetAllLogs` 旧 12 参数签名 → `model.GetAllLogs(model.LogFilter{Type: model.LogTypeTopup}, 0, 20)`
4. `controller/access_token_audit_test.go`：SQLite 测试库补 `t.Cleanup` 关连接（上游此处在 Windows 上 TempDir 删除会失败，Linux CI 无感，不影响程序）

### npm 兼容调整 2 处

- `web/package.json`：`dependencies.katex` 由 `^0.18.2` 改为 `0.18.2`（必须与 `overrides.katex` 完全一致 npm 才允许安装；上游用 bun 不报此错，改用 bun 后可不再需要）
- `web/package.json`：`devDependencies.oxlint` 由 `^1.72.0` 钉为 `1.74.0`（对齐上游 bun.lock 的锁定版本；`^` 会浮到 1.86 引入 300+ 条新规则报错，而魔改文件在 1.74 下 lint 干净。上游换 oxlint 版本时跟着调这里）

### 本次顺手减债

- 商家门户查询函数（MerchantLogQueryParams、GetMerchantChannelLogs、GetMerchantChannelLogStats、GetMerchantChannelTrafficByGroup、GetMerchantChannelHealth、FormatMerchantLogs）从 model/log.go 移到 model/merchant_log.go，以后 log.go 再重构不再连带冲突

### 待办（下次继续减债）

- [ ] controller/midjourney.go 的商家收益逻辑抽到 service 小函数（上游若 relaykit 化重写 midjourney，冲突会变大）
- [ ] 评估用上游插件机制承载部分魔改（可把这类债清零）

### 验证结果

- `go build ./...` → 通过
- `go test ./model/ -run 'Merchant|Gift|Log'` → ok
- `go test ./service/ -run 'Gift|Merchant'` → ok
- `go test ./controller/ -run 'TestSecurityAccount|UserManage|UserUpdate|Quota'` → ok（63s，含全部赠金测试）

## 2026-11-13 合并 upstream b20e056（落后 128 个提交）

### 总原则

本次起冲突优先采用上游结构与文案，魔改逻辑只做最小叠加（后续继续减债）。

### 冲突 24 处（手动解决）

| 文件 | 处理 |
|---|---|
| types/price_data.go | 采用上游字段集（含 CacheCreation12hRatio），追加 ChannelRate |
| dto/channel_constraints.go | GiftBilling 与 TaskPluginRelayOnly 并存 |
| controller/channel_authz.go | merchant_id 与 balance_detail 都纳入只读/清零 |
| controller/relay.go | 保留商家分润块，接入上游 RecordImmediateTaskResult |
| controller/system_task_handlers.go | 保留 priceSync/routingOptimizer/candleAgg，并注册上游 systemTaskCleanupHandler |
| controller/user.go | 保留内存渠道缓存刷新，接入上游 BumpSyncTopic |
| model/channel.go | 保留 InsertWithTx，采用上游 Update 注释 |
| model/log.go | 保留 RecordConsumeLog 返回 logID，接入上游 SetContextKey(ContextKeyConsumeLog) |
| model/system_task.go | 自定义任务类型 + 上游 channel_balance/system_task_cleanup 合并 |
| relay/helper/price.go | 上游字段集 + ChannelRate |
| relay/relay_task.go | 采用上游 tryRealtimeFetch（去掉 ctx 参数与本地 FinalizeTerminalTaskOnFetch 路径，终态交给轮询） |
| service/quota.go | 保留赠金余额校验与文案，改用上游 (int,error) 签名 |
| service/task_polling.go | 保留商家分润，接入上游 reportFinalTask/bulkFailedTasks |
| service/channel_affinity_usage_cache_test.go | 全取上游（上游 #8009 已修复同类 flaky） |
| THIRD-PARTY-LICENSES.md | 两段都保留（CuzTeam 归属 + moejs notices） |
| web/package.json | 取上游依赖升级（vitest 5 / motion 13 / nanoid 6 等），保留 @visactor/vchart-extension、katex 0.18.2 精确钉、oxlint 1.74.0 精确钉 |
| web/src/hooks/use-sidebar-view.ts | 合并两套 import，接入 useModelPricingIssueBadge |
| web/src/hooks/__tests__/sidebar-config.test.tsx | 两套 describe 都保留 |
| channel-configuration.test.tsx | 赠金测试 + 上游 passthrough 测试并存 |
| channel-mutate-drawer.tsx | 保留赠金字段块，采用上游 modelFetchable |
| web/src/routeTree.gen.ts | 两个新路由都保留（构建会重生成） |
| web/src/i18n/locales/*.json（7 个） | 以上游为基底，补回我们缺失的 324 个键；同名键取上游文案，键排序 |

### 编译/验证

- 工具链升级：go.mod → 1.26.10（CVE-2026-111943），需 `GOTOOLCHAIN=go1.26.10`
- `go build ./...` 通过；relaykit `GOWORK=off go build ./...` 通过
- 全仓测试编译扫描 `go test ./... -run zzz_none_zzz` 通过
- 三件套均 ok（controller 80s）
- 前端 `npm install`（registry.npmmirror.com）+ `npm run build` 通过；`npx vitest run` 238 文件 2887/2890 通过
  - 3 个失败在 `priority-order.test.tsx`（上游新增文件，与上游 diff 为零，Windows 环境问题，非本次合并引入）
  - `npx oxlint` 1.74.0 下报上游文件 2 个 error + 若干 warning（配置与锁和上游一致，非本次引入）

### 待办

- [ ] 跟进 priority-order 测试在 Windows 的 3 个失败（上游同样失败）
- [ ] 商家/赠金逻辑继续按上游重构减债
