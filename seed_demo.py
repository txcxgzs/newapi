# -*- coding: utf-8 -*-
"""临时脚本：为商家门户/行情页灌入演示数据（仅本地试用，可删除）。
不使用任何外部依赖，直接写 SQLite；服务端未开内存缓存，DB 写入即时可见。
"""
import json
import math
import random
import sqlite3
import time

DB_PATH = r"c:\Users\Administrator\Desktop\newapipro\new-api\one-api.db"
now = int(time.time())

con = sqlite3.connect(DB_PATH, timeout=30)
cur = con.cursor()


def uid(username):
    cur.execute("SELECT id FROM users WHERE username=?", (username,))
    row = cur.fetchone()
    if not row:
        raise SystemExit(f"用户 {username} 不存在")
    return row[0]


m1 = uid("merchant1")
m2 = uid("merchant2")

# ---- 0) 幂等清理：先删掉这两个商家的历史演示数据 --------------------------
old_channels = [r[0] for r in cur.execute(
    "SELECT id FROM channels WHERE merchant_id IN (?,?)", (m1, m2)).fetchall()]
if old_channels:
    qs = ",".join("?" * len(old_channels))
    for sql in (
        f"DELETE FROM logs WHERE channel_id IN ({qs})",
        f"DELETE FROM abilities WHERE channel_id IN ({qs})",
        f"DELETE FROM channel_quotes WHERE channel_id IN ({qs})",
    ):
        cur.execute(sql, old_channels)
cur.execute("DELETE FROM channels WHERE merchant_id IN (?,?)", (m1, m2))
cur.execute("DELETE FROM merchant_earnings WHERE merchant_user_id IN (?,?)", (m1, m2))
cur.execute("DELETE FROM merchant_wallets WHERE merchant_user_id IN (?,?)", (m1, m2))
cur.execute("DELETE FROM merchant_profiles WHERE user_id IN (?,?)", (m1, m2))
con.commit()

# ---- 1) 商家资料 + 钱包 ----------------------------------------------------
profiles = [
    (m1, "OpenAI 直连一号店", "https://upstream-a.example.com", 0.95, 1),
    (m2, "Claude/Kiro 混合店", "https://upstream-b.example.com", 0.90, 1),
]
for p in profiles:
    cur.execute(
        "INSERT INTO merchant_profiles (user_id, site_name, site_url, profit_ratio, status, created_at, updated_at) "
        "VALUES (?,?,?,?,?,?,?)",
        (*p, now, now),
    )

wallets = [(m1, 128.50, 386.20, 0.0, now), (m2, 62.30, 154.80, 0.0, now)]
cur.executemany(
    "INSERT INTO merchant_wallets (merchant_user_id, balance, total_earned, total_withdrawn, updated_at) VALUES (?,?,?,?,?)",
    wallets,
)

# ---- 2) 渠道（挂在商家名下，归属到预设分组）--------------------------------
# (merchant_id, name, group, models, priority, weight, rate_multiplier)
# rate_multiplier = 商家填的渠道倍率（相对平台标准价的折扣系数），是渠道唯一的价格输入
channels = [
    (m1, "OA-Pro-A", "pro", ["gpt-6o", "gpt-6o-mini"], 10, 100, 0.85),
    (m1, "Claude-CCMAX-A", "ccmax", ["claude-opus-4-6", "claude-sonnet-4-6"], 10, 100, 1.05),
    (m2, "OA-Pro-B", "pro", ["gpt-6o", "gpt-6o-mini"], 5, 80, 0.92),
    (m2, "OA-Plus-B", "plus", ["gpt-6", "gpt-6-mini"], 10, 100, 0.78),
    (m2, "Kiro-A", "kiro", ["kiro-2"], 10, 100, 1.10),
]

channel_ids = []
for m, name, grp, models, prio, weight, rate in channels:
    cur.execute(
        "INSERT INTO channels (type, key, status, name, weight, created_time, base_url, models, \"group\", "
        "priority, auto_ban, merchant_id, auto_routing, price_sync, rate_multiplier, balance_updated_time) "
        "VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
        (1, "sk-demo-" + name, 1, name, weight, now, "https://upstream.example.com",
         # 渠道的 models/group 字段系统约定是逗号分隔（见 ReviewMerchantApplication 的 strings.Join），
         # 不能写成 JSON 数组，否则盘口派生与分组归拢都读不到模型。
         ",".join(models), grp, prio, 1, m, 1, 1, rate, now),
    )
    cid = cur.lastrowid
    channel_ids.append((cid, grp, models, rate))

# ---- 3) 派生盘口 + 能力表（用户侧按分组看模型，多渠道静默路由）-------------
# 平台标准价 $/1M（倍率 1.0 = $2/1M，所以模型倍率 = 标准输入价 / 2）。
# 盘口价 = 平台标准价 × 渠道倍率，与后端 RebuildChannelQuotes 的口径完全一致。
price_table = {
    "gpt-6o": (2.5, 10.0),
    "gpt-6o-mini": (0.6, 2.4),
    "gpt-6": (1.25, 5.0),
    "gpt-6-mini": (0.3, 1.2),
    "claude-opus-4-6": (5.0, 25.0),
    "claude-sonnet-4-6": (3.0, 15.0),
    "kiro-2": (1.0, 4.0),
}
RATIO_BASE = 2.0
# 与 common.QuotaPerUnit 对齐：500000 quota = $1
QUOTA_PER_UNIT = 500000
for cid, grp, models, rate in channel_ids:
    for mdl in models:
        base_in, base_out = price_table.get(mdl, (1.0, 4.0))
        effective = base_in / RATIO_BASE * rate  # 有效倍率 = 平台标准倍率 × 渠道倍率
        cur.execute(
            "INSERT INTO channel_quotes (channel_id, pool_group, model_name, rate_multiplier, input_price, "
            "output_price, currency, source, status, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)",
            (cid, grp, mdl, round(effective, 6), round(base_in * rate, 4), round(base_out * rate, 4),
             "USD", "derived", 1, now, now),
        )
        cur.execute(
            "INSERT INTO abilities (\"group\", model, channel_id, enabled, priority, weight, tag) VALUES (?,?,?,?,?,?,?)",
            (grp, mdl, cid, 1, 10, 100, ""),
        )

# ---- 4) 调用日志（供“按分组调用量”和“调用记录”展示）-----------------------
# quota 必须按模型真实单价折算，否则 K 线聚合出来的"混合成交价"会离谱
# （历史版本用了写死的 2/8 每千 token，结果算出 $4000/1M 这种数字）。
models_pool = [(cid, grp, mdl) for cid, grp, models, _rate in channel_ids for mdl in models]
log_rows = []
for i in range(260):
    cid, grp, mdl = random.choice(models_pool)
    created = now - random.randint(0, 23 * 3600)
    failed = random.random() < 0.08
    pt = random.randint(200, 4000)
    ct = random.randint(100, 3000)
    pin, pout = price_table.get(mdl, (1.0, 4.0))
    quota = int((pt / 1e6 * pin + ct / 1e6 * pout) * QUOTA_PER_UNIT)
    log_rows.append((
        0, created, 5 if failed else 2,
        (f"上游返回错误：upstream 5xx（{mdl}）" if failed else f"模型 {mdl} 消耗"),
        "", "", "demo-token", mdl, 0 if failed else quota, pt, ct,
        random.randint(1, 30), 1, cid, "", grp, f"req-{i:05d}", "", "",
    ))
cur.executemany(
    "INSERT INTO logs (user_id, created_at, type, content, username, channel_name, token_name, model_name, quota, "
    "prompt_tokens, completion_tokens, use_time, is_stream, channel_id, ip, \"group\", request_id, upstream_request_id, other) "
    "VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
    log_rows,
)

# ---- 5) 分润流水 ----------------------------------------------------------
earn_rows = []
earn_seq = 1
for cid, grp, models, _rate in channel_ids[:4]:
    owner = m2 if cid in [c for c, _, _, _ in channel_ids[2:]] else m1
    for k in range(4):
        earn_rows.append((owner, cid, earn_seq, round(random.uniform(0.5, 8.0), 4), models[0], now - k * 3600))
        earn_seq += 1
cur.executemany(
    "INSERT INTO merchant_earnings (merchant_user_id, channel_id, log_id, amount, model_name, created_at) VALUES (?,?,?,?,?,?)",
    earn_rows,
)

# ---- 6) 分组与模型倍率配置 -------------------------------------------------
# 平台必须先给模型配好"标准倍率"，商家的渠道倍率才有可乘的基准
# （倍率 = 标准输入价 / 2；补全倍率 = 标准输出价 / 标准输入价）。
def upsert_option(key, mutate):
    cur.execute("SELECT value FROM options WHERE \"key\"=?", (key,))
    found = cur.fetchone()
    data = json.loads(found[0]) if found and found[0] else {}
    mutate(data)
    if found:
        cur.execute("UPDATE options SET value=? WHERE \"key\"=?", (json.dumps(data), key))
    else:
        cur.execute("INSERT INTO options (\"key\", value) VALUES (?, ?)", (key, json.dumps(data)))

upsert_option("GroupRatio", lambda d: [d.setdefault(g, 1) for g in ["pro", "plus", "ccmax", "kiro"]])

model_ratios = {mdl: round(pin / RATIO_BASE, 6) for mdl, (pin, _pout) in price_table.items()}
completion_ratios = {mdl: round(pout / pin, 6) for mdl, (pin, pout) in price_table.items()}
upsert_option("ModelRatio", lambda d: d.update(model_ratios))
upsert_option("CompletionRatio", lambda d: d.update(completion_ratios))

# ---- 7) 分钟级 K 线行情 ----------------------------------------------------
# 目标：让行情页看起来像真正的股票大屏，且 1m / 5m / 1h 三个周期彼此自洽。
#
# 口径必须与后端 candle_aggregate.go 完全一致：
#   混合成交价($/1M) = quota ÷ (prompt_tokens + completion_tokens) × 1e6 ÷ QuotaPerUnit
#   所以这里反过来由"目标价"推算 quota，写进去的价格才对得上。
#   5m / 1h 不是独立造数，而是对同一条分钟曲线按周期聚合，避免不同周期各画各的。
#
# 保留策略（operation_setting.CandleRetentionDays 默认 30）：
#   1m 只保留 30 小时，所以 1m 只灌最近 CANDLE_1M_HOURS 小时；
#   5m / 1h 保留 30 天，于是灌满 CANDLE_LONG_DAYS 天，长周期图才有东西看。

DAY = 86400
CANDLE_1M_HOURS = 24  # 必须小于 30 小时，否则会被聚合任务的清理逻辑删掉
CANDLE_LONG_DAYS = 30
# 每分钟调用次数（各周期必须用同一个值）：否则 1h 的成交量不会是 60 根 1m 之和，
# 行情页的"24h 成交笔数/成交额"（取自 1h）与 1m 图就对不上。
CANDLE_CALLS_PER_MIN = 2


def seed_of(key):
    """稳定的字符串散列（不能用内置 hash，它每个进程的结果都不一样）。"""
    s = 2166136261
    for ch in key:
        s = ((s ^ ord(ch)) * 16777619) & 0xFFFFFFFF
    return s


def det_noise(seed, n):
    """确定性噪声 0..1：脚本重跑生成同一条曲线，方便反复演示与比对。"""
    x = (seed + (n + 1) * 2654435761) & 0xFFFFFFFF
    x ^= x >> 15
    x = (x * 2246822519) & 0xFFFFFFFF
    x ^= x >> 13
    return x / 4294967295.0


def noise_lerp(seed, x):
    """把噪声当连续曲线做线性插值。

    直接用整数索引会让同一分钟内的 open 与 close 取到同一个噪声值，
    K 线就全成了"十字星"；插值后分钟内才有价格位移，蜡烛才有实体。
    """
    i = int(math.floor(x))
    f = x - i
    return det_noise(seed, i) * (1 - f) + det_noise(seed, i + 1) * f


def blend_anchor(pin, pout):
    """混合成交价的锚点：调用以 prompt 为主，所以偏输入价一侧。"""
    return pin * 0.62 + pout * 0.38


def price_at(symbol, base, ts):
    """连续时间的成交价随机游走。不同周期采样同一条曲线，保证互相自洽。"""
    phase = (seed_of(symbol) % 1000) / 1000.0 * 2 * math.pi
    trend = (
        0.055 * math.sin(2 * math.pi * ts / (3 * DAY) + phase)
        + 0.028 * math.sin(2 * math.pi * ts / DAY + phase * 1.7)
        + 0.018 * math.sin(2 * math.pi * ts / (7 * DAY) + phase * 2.3)
    )
    # 分钟级微观波动：制造 K 线该有的毛刺与蜡烛实体
    micro = 0.02 * (noise_lerp(seed_of(symbol + "|micro"), ts / 60.0) - 0.5)
    return base * (1.0 + trend + micro)


def gen_candle(pool, model, pin, pout, ts, size, calls_per_min=CANDLE_CALLS_PER_MIN):
    """生成一根 K 线。内部逐分钟采样，size=300/3600 时天然就是 1m 的严格聚合。"""
    symbol = pool + "/" + model
    base = blend_anchor(pin, pout)
    seed_c, seed_h, seed_l = (
        seed_of(symbol + "|calls"),
        seed_of(symbol + "|high"),
        seed_of(symbol + "|low"),
    )
    o = h = l = c_close = None
    calls = 0
    tokens = 0
    for i in range(size // 60):
        m_ts = ts + i * 60
        c = 1 + int(calls_per_min * 1.6 * det_noise(seed_c, m_ts // 60))
        p_open = price_at(symbol, base, m_ts)
        p_close = price_at(symbol, base, m_ts + 59)
        p_high = max(p_open, p_close) * (1 + 0.004 * det_noise(seed_h, m_ts // 60))
        p_low = min(p_open, p_close) * (1 - 0.004 * det_noise(seed_l, m_ts // 60))
        if o is None:
            o, h, l, c_close = p_open, p_high, p_low, p_close
        else:
            h = max(h, p_high)
            l = min(l, p_low)
            c_close = p_close
        calls += c
        tokens += c * 4200  # 每次调用约 3k prompt + 1.2k completion
    if o is None:
        return None
    # 由目标价反推 quota，保证 price = quota/tokens 的等式在读取时成立
    quota = int(c_close / 1e6 * tokens * QUOTA_PER_UNIT)
    return (pool, model, ts, o, h, l, c_close, calls, quota)


# 待灌行情的 (号池, 模型) 组合。同一号池下多个渠道可能上架同一款模型，
# 按 (号池, 模型) 去重，否则会对同一批桶重复造数、白白拉长脚本耗时。
candle_targets = []
_seen_targets = set()
for cid, grp, models, _rate in channel_ids:
    for mdl in models:
        if (grp, mdl) in _seen_targets:
            continue
        _seen_targets.add((grp, mdl))
        pin, pout = price_table.get(mdl, (1.0, 4.0))
        candle_targets.append((grp, mdl, pin, pout))

# 重新灌之前先清掉这些模型的旧 K 线，保证脚本可反复执行
for grp, mdl, _pin, _pout in candle_targets:
    cur.execute(
        "DELETE FROM market_candles WHERE pool_group=? AND model_name=?", (grp, mdl)
    )

candle_rows = []
now_bucket = now // 60 * 60  # 只灌"已走完"的分钟桶，与聚合任务口径一致

# 7.1) 1m：最近 24 小时（短周期图才连续）
from_ts = now_bucket - CANDLE_1M_HOURS * 3600
for grp, mdl, pin, pout in candle_targets:
    for ts in range(from_ts, now_bucket, 60):
        row = gen_candle(grp, mdl, pin, pout, ts, 60)
        if row:
            candle_rows.append(("1m",) + row)

# 7.2) 5m / 1h：最近 30 天（长周期图与 24h 涨跌统计都依赖它）
long_from = now_bucket - CANDLE_LONG_DAYS * DAY
for interval, size in (("5m", 300), ("1h", 3600)):
    # 桶起点必须对齐周期边界，否则前端时间轴会错位；
    # 且只灌"已走完"的桶，否则最新一根会是残缺的半截桶。
    start = long_from // size * size
    end = now_bucket // size * size
    for grp, mdl, pin, pout in candle_targets:
        for ts in range(start, end, size):
            row = gen_candle(grp, mdl, pin, pout, ts, size)
            if row:
                candle_rows.append((interval,) + row)

cur.executemany(
    "INSERT OR REPLACE INTO market_candles "
    "(interval, pool_group, model_name, bucket_ts, open, high, low, close, volume_calls, volume_quota) "
    "VALUES (?,?,?,?,?,?,?,?,?,?)",
    candle_rows,
)

con.commit()
con.close()
candle_1m = sum(1 for r in candle_rows if r[0] == "1m")
print("seed ok: channels=%d logs=%d earnings=%d groups=%s models=%d candles=%d(1m=%d)" % (
    len(channel_ids), len(log_rows), len(earn_rows),
    ",".join(["pro", "plus", "ccmax", "kiro"]), len(model_ratios), len(candle_rows), candle_1m))
