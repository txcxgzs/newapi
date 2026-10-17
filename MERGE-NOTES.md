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
