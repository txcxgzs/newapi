# Liquid Inference Order-Book 实时订单簿使用指南

本平台面向买家与算力服务商（商家）提供实时订单簿（Order-Book）竞价路由与透明结算能力。每一次推理请求都会基于实时报价（`$/1M tokens`）、实测首字延迟（`TTFT`）、吞吐量（`TPS`）与健康度（`Success EMA`）实时竞价派单，并在首字返回前锁定结算价格。

---

## 一、买家使用指南

### 1. 在 API 令牌（Token）中持久化默认策略

在控制台 **「令牌管理」** 新建或编辑令牌时，展开 **「Order-Book 路由与风控约束」**，可直接使用快捷预设或精细配置以下规则：

| 配置项 | 说明 | 示例值 |
| --- | --- | --- |
| 路由策略覆盖 | 覆盖全局默认策略：`price_ttft`（性价比竞价）、`lowest_price`（最低报价）、`lowest_ttft`（最低首字延迟）、`merchant_first` / `round_robin` / `order` | `price_ttft` |
| 最大 TTFT (ms) | 仅保留实测首字延迟不高于该阈值的渠道（`0` 为不限制） | `800` |
| 最低 TPS (tok/s) | 仅保留流式输出速率不低于该阈值的渠道（`0` 为不限制） | `25` |
| 最高单价 ($/1M) | 限制输入/输出综合单价上限（`0` 为不限制） | `5.0` |
| 服务商白名单 | 仅允许指定服务商承接，支持服务商编码、名称或 `Platform`（逗号分隔） | `m1, Platform` |
| 服务商黑名单 | 排除指定服务商（逗号分隔） | `m2` |

### 2. 单次请求通过 HTTP 请求头临时覆盖

单次调用携带以下请求头时，优先级高于 Token 默认配置，方便同一把 Key 在不同业务场景切换 SLA：

```bash
curl -sS -D - https://your-gateway.example.com/v1/chat/completions \
  -H "Authorization: Bearer sk-your-token" \
  -H "Content-Type: application/json" \
  -H "X-Routing-Strategy: lowest_ttft" \
  -H "X-Max-TTFT-Ms: 800" \
  -H "X-Min-TPS: 30" \
  -H "X-Max-Price: 5.0" \
  -H "X-Merchant-Allow: m1, Platform" \
  -H "X-Merchant-Deny: m2" \
  -d '{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"Hello"}]}'
```

### 3. 响应头与消耗日志透明验真

每次请求响应头（在流式首包发送前写回）均包含实际撮合结果与锁定单价，同时永久写入 **「使用日志」**：

- `X-Relay-Provider`：实际承接本次请求的服务商名称（平台自营为 `Platform`）
- `X-Relay-Provider-Code`：服务商唯一编码（如 `m1` / `platform`）
- `X-Relay-Group`：实际命中的号池分组
- `X-Relay-Strategy`：本次生效的路由策略
- `X-Relay-Quote-Source`：结算报价来源（`merchant` / `auto` / `manual` / `platform`）
- `X-Relay-Price-Input` / `X-Relay-Price-Output`：按本次命中渠道锁定的输入/输出单价（`$/1M tokens`）

### 4. 重试与降级切换保护

当首选节点在首字返回前失败并触发重试时，系统会自动将计费上下文重置为基准价格并严格排除已尝试渠道，再为备选渠道重新匹配其自身真实报价与响应头，确保低价节点的挂牌价不会串用到更高价的备用节点。

---

## 二、服务商（商家）实时竞价指南

### 1. 报价优先级与实时生效

- **直接覆盖平台基准倍率**：`merchant`（商家实时出价）、`auto`（上游价格定时同步）、`manual`（管理员锁定）三种盘口在渠道被选中后会直接覆盖静态模型倍率，按实际挂牌单价实时结算。
- **冷启动与实时性能评估**：节点每成功或失败一次，系统都会记录实测 `TTFT`、`TPS` 与 `Success EMA`，并每 15 秒增量刷入数据库 `channel_live_metrics`，重启后无需重新预热即可保留历史口碑评分；成功率低于 `85%` 的节点会自动受到排名惩罚。

### 2. 通过商家门户或自动化 API 实时改价

1. 登录 **「商家门户 (`/merchant`) -> 模型报价」** 点击 **「改价」**，无需人工审核即可即时调整单模型 `input_price` / `output_price`（范围 `0.001 ~ 10000 USD/1M`）。
2. 如需自动化动态竞价，可在个人安全设置生成 Access Token，使用以下接口：
   - 查询对手最低价与自身排名：`GET /api/merchant/portal/bidding`
   - 实时批量推送单模型报价：`PUT /api/merchant/portal/quotes`
   - 调整分组倍率与是否加入 `auto` 路由池：`PUT /api/merchant/portal/rate`

---

## 三、实时行情与订单簿看板

访问公开页面 **`/market`** 或调用匿名接口 `GET /api/market/orderbook`（支持 `?pool=...&model=...&strategy=price_ttft|lowest_price|lowest_ttft`）：
- 可按策略实时查看全网或指定模型下各服务商脱敏挂单排名（排名 `#1` 标注为 `Best`）、输入/输出 `$/1M` 报价、实测/冷启动 `TTFT`、`TPS`、近样本成功率及报价来源。
