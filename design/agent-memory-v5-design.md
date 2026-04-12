# Agent Memory System v5 — Final Design

## Context

经过 10+ 轮设计讨论，综合了三个来源的最佳实践：
- **Google Always-On Memory Agent**: 多 agent 编排、工具函数 API 边界、后台定时合并、无状态请求、LLM 重要性打分
- **Karpathy LLM Wiki**: raw/wiki 分离、编译式 Markdown、index 路由、来源链接、可重建、**query 时也整理 wiki**
- **人脑记忆神经科学**: episodic/semantic 分离、上下文编码、前瞻记忆、蒸馏式巩固、多维标记、再巩固

v3（21 表 SQL-first）被 Codex 证明过度工程。v4（纯 JSONL + wiki）被 Codex 证明隐含数据库问题。本方案是最终综合版，目标是 **v0 原型验证模型可行性**，存储层接口化，先不选具体存储。

---

## 系统定位：完整 LLM Agent 服务

### 定位决策

agent-memory 是一个**完整的 LLM agent 服务**，自带 ingest/query/compile 三个 LLM agent，对外暴露高层 API。

v0 全部使用完整 LLM 能力，先验证效果，后续再按需优化（降级为规则引擎、缓存热路由等）。

### 对外 API

```
# 高层 API（对 OpenClaw、web app 等调用方）
remember(text, context?)         → 由 ingest_agent 处理
recall(question, context?)       → 由 query_agent 处理
compile()                        → 由 compile_agent 处理（也可定时自动触发）
status()                         → 返回系统统计

# 低层 API（给高级 LLM 调用方直接使用，可选）
read_wiki_index()                → 直接读 wiki 路由表
read_wiki_page(path)             → 直接读 wiki 页面
search_wiki(query)               → 直接搜索 wiki
append_event(...)                → 直接追加事件
```

### Context 参数

context 是可选的 JSON 对象，提供当前上下文以提升编码/检索质量：

```json
{
    "project": "db9",
    "task": "schema design",
    "session_id": "sess_abc123"
}
```

所有字段都可选。调用方传什么就用什么，不传也能工作（ingest_agent 会尽量从 text 自身推断）。

### v0 服务形态

v0 提供 **HTTP API**，作为独立服务运行，供 OpenClaw 等 AI agent 通过网络调用。

```
POST /remember    { "text": "...", "context": {...} }
POST /recall      { "question": "..." , "context": {...} }
POST /compile     {}
GET  /status
```

后续演进：
- **v1**: + MCP Server（让 Claude Code 等原生 MCP agent 直接连接）
- **v1**: + CLI 客户端（方便调试和手动测试）

### 三 Agent 都能写 Wiki

恢复 Karpathy 原始模式：ingest、query、compile 三个 agent **都能直接读写 wiki**。

Karpathy 原文明确指出：
> "好的回答可以作为新页面归档回 Wiki！比较分析、发现的联系 — 这些不应该消失在聊天历史里，而是应该沉淀下来继续复利。"

v0 是单进程，不存在多消费者并发写的问题。不需要 hint/signal 间接层。

---

## 核心设计原则：三时机整理模型

### 人脑记忆的工作方式

人脑**没有**"后台定时编译"这种单一机制。人脑的记忆整理发生在**三个时机**，每个时机做不同粒度的整理：

1. **编码时（Encoding）** — 听到新信息的瞬间，海马体立即将它与已有知识网络关联。不是存一个孤立事件等后面处理，而是**编码时就织入已有知识网络**。

2. **回忆时（Retrieval）** — 每次回忆不是"读取"，而是**重构**。从多个碎片重新组装答案，过程中会强化相关联系、发现新联系。Nader (2000) 的再巩固理论：回忆后记忆变得不稳定，重新存储时会被更新。经常回忆的知识越来越牢固、越来越结构化。

3. **睡眠时（Consolidation）** — 海马体向新皮层"回放"，多个 episodic 经历被压缩成 semantic 知识。这是唯一真正异步的环节。

### 对应到系统设计

| 时机 | 人脑做什么 | 系统做什么 | 谁做 | 整理粒度 |
|------|-----------|-----------|------|---------|
| **编码时** | 海马体立即关联到已有知识网络 | ingest_agent 读 wiki，把新信息织入关联页面 | ingest_agent | 局部（1-3 个页面） |
| **回忆时** | 重构 + 强化联系 + 再巩固 | query_agent 重构式回答 + 修复错误/补链接/归档新知识 | query_agent | 发现式（跨页面联系、修错、归档） |
| **睡眠时** | episodic → semantic 蒸馏 | compile_agent 全局蒸馏 + 矛盾检测 + 重建 index | compile_agent | 全局（所有未处理事件） |

### 为什么不能只靠异步编译

如果只靠"睡眠时"编译，相当于一个人：
- 听到信息 → 原样记下，不思考，不关联
- 被问问题 → 只翻笔记本，不产生新理解
- 只有睡觉时 → 才整理所有内容

这不是记忆，这是**录音机 + 定时整理员**。真正的记忆系统需要三个时机都在整理。

---

## 架构总览

```
外部调用方（OpenClaw plugin / web app / CLI）
    │
    ▼
┌─────────────────────────────────────────────────────────┐
│                 agent-memory 服务                         │
│                                                          │
│  高层 API: remember() / recall() / compile() / status()  │
│                                                          │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐   │
│  │ ingest_agent │  │ query_agent  │  │compile_agent │   │
│  │ (LLM)       │  │ (LLM)       │  │ (LLM)       │   │
│  │             │  │             │  │             │   │
│  │ 读写: raw   │  │ 读写: wiki  │  │ 读: raw     │   │
│  │ 读写: wiki  │  │             │  │ 读写: wiki  │   │
│  │             │  │             │  │ 写: archive │   │
│  └──────┬───────┘  └──────┬──────┘  └──────┬──────┘   │
│         │                 │                 │           │
│         ▼                 ▼                 ▼           │
│      ┌────────────────────────────────────────┐        │
│      │  raw/（append-only 事件日志）            │        │
│      │  wiki/（LLM 编译的 Markdown 页面）       │        │
│      └────────────────────────────────────────┘        │
└─────────────────────────────────────────────────────────┘
```

### Wiki 目录结构

```
wiki/
├── index.md                          # 自动生成，带页面描述
├── .meta/                            # sidecar 元数据（镜像目录结构）
│   ├── semantic/projects/db9.json
│   ├── episodic/2026-04-12/db9-schema-discussion.json
│   └── ...
├── semantic/                         # 语义记忆（去上下文化的知识）
│   ├── index.md                      # semantic 子路由
│   ├── preferences/
│   │   ├── ui.md
│   │   └── communication.md
│   ├── people/
│   │   ├── alice.md
│   │   └── bob.md
│   └── projects/
│       ├── db9.md
│       └── drive9.md
├── episodic/                         # 情景记忆（带完整上下文的经历）
│   ├── index.md                      # 按时间的子路由
│   └── 2026-04-12/
│       ├── db9-schema-discussion.md
│       └── drive9-ui-review.md
├── procedural/                       # 程序记忆（怎么做）
│   ├── index.md
│   ├── deploy-drive9.md
│   └── debug-fuse-mount.md
├── prospective/                      # 前瞻记忆（意图+触发条件）
│   ├── index.md
│   └── notify-alice-on-drive9-release.md
└── archive/                          # 归档区（被遗忘的页面，仍可搜索恢复）
    ├── episodic/
    ├── procedural/
    └── semantic/
```

---

## 核心设计决策

### 1. 记忆类型分层（P0）

来源：Tulving 五种记忆系统（1972）。人脑对不同类型记忆有不同的编码和检索机制。

| 类型 | 内容 | 人脑对应 | 编码方式 | 检索方式 |
|------|------|---------|---------|---------|
| **semantic** | 去上下文的事实、偏好、知识 | 语义记忆（"巴黎是法国首都"） | ingest 时织入；compile 时蒸馏 | index 路由 → 读页面 |
| **episodic** | 带完整上下文的经历 | 情景记忆（"2019年和Alice去巴黎，下着雨"） | ingest 时直接创建 | 按时间/场景检索 |
| **procedural** | 操作步骤、工作流 | 程序记忆（"怎么骑自行车"） | compile 识别 how-to 类内容 | index 路由 → 读页面 |
| **prospective** | 未来意图 + 触发条件 | 前瞻记忆（"见到Alice时记得告诉她"） | ingest 时识别意图；compile 提取 | 每次对话扫描触发条件 |

### 2. Raw 事件带客观上下文（P0）

来源：Tulving 编码特异性原则（1973）。记忆回忆成功取决于检索上下文是否匹配编码上下文。所以编码时必须保存上下文。

```json
{
  "id": "evt_20260412_143022_a7f3",
  "timestamp": "2026-04-12T14:30:22Z",
  "actor": "user",
  "content": "Alice 建议 db9 用分区表",
  "source": "conversation",
  "session_id": "sess_abc123",
  "active_project": "db9",
  "active_task": "schema design",
  "durability": "long-term",
  "actionability": "actionable",
  "source_type": "user",
  "evidence_kind": "user_statement",
  "trust_tier": 1
}
```

只存客观字段。不存推测字段（emotion, intent）。

### 3. 两维标记（P0）

来源：杏仁核和海马体的多维度标记。人脑不用单一"重要性"分数，而是多个维度独立评估。v0 简化为两个最有用的维度。

- **durability**: ephemeral / session / long-term / permanent
  - 决定是否编译进 wiki（ephemeral 不编译）
- **actionability**: none / informational / actionable / urgent
  - 决定编译到哪个类型（actionable/urgent → prospective 候选）

### 4. 三时机整理的具体行为（P0 — 核心验证点）

#### 时机一：编码时整理（Ingest）

来源：海马体在编码时就将新信息与已有记忆网络关联，不是存完就走。

ingest_agent 收到新信息时：

1. **append_event()** — 写入 raw 事件
2. **read_wiki_index()** — 了解已有知识结构
3. **定位关联页面** — 根据内容判断与哪些已有页面相关
4. **read_wiki_page()** — 读取关联页面，理解已有上下文
5. **write_wiki_page()** — 把新信息织入关联页面，标注来源 `[evt_xxx]`
6. **创建 episodic 页面** — 如果是一段有完整上下文的经历
7. **创建 prospective 页面** — 如果包含明确的未来意图和触发条件

整理范围：**局部**，只处理这条新信息涉及的 1-3 个页面。

#### 时机二：回忆时整理（Query）

来源：
- Bartlett 重构记忆（1932）：回忆不是读取原文，是从碎片在当前情境下重新组装。
- Nader 再巩固理论（2000）：每次回忆后记忆变得不稳定，重新存储时可被更新/强化。
- Karpathy LLM Wiki：好的回答应该归档回 wiki，继续复利。

query_agent 回答问题时：

1. **read_wiki_index()** — 读路由表
2. **read_wiki_page()** — 读取相关页面
3. **search_wiki()** — 必要时关键词搜索
4. **重构式回答** — 综合多个页面 + 当前上下文重构答案（不是复制粘贴 wiki 内容）
5. **修复错误** — 发现 wiki 内容有误时直接修正
6. **修复链接** — 发现断链或缺少交叉引用时直接补上
7. **归档新知识** — 跨页面综合分析产生了新知识（比较、联系、洞察）时，写为新页面

触发条件：**不是每次 query 都写 wiki**。只有发现以下情况才写：
- wiki 内容有事实错误
- 交叉引用缺失或断链
- 产生了 wiki 中没有的综合分析/新知识

#### 时机三：睡眠时整理（Compile）

来源：睡眠中海马体向新皮层"回放"，进行全局整合和压缩。

compile_agent 定时后台运行，做全局整理：

1. **蒸馏** — 多条 episodic 事件压缩为 semantic 知识：
   ```
   episodic:
     "4月10号 Alice 说用分区表" [evt_042]
     "4月11号看文档确认分区优势" [evt_055]
     "4月12号测试结果: 快 3x" [evt_061]

       ↓ 蒸馏

   semantic/projects/db9.md:
     ## 表设计
     - 采用分区表 [evt_042, evt_055, evt_061]
     - 验证结果: 大表查询快 3x [evt_061]
     - 由 Alice 首先提出 [evt_042]
   ```

2. **矛盾检测** — 扫描 wiki，发现不一致标注 `⚠️ CONFLICT`
3. **交叉引用补全** — 找到应该互相链接但没有链接的页面
4. **index.md 全量重建** — 重新扫描所有页面生成路由表（不是增量追加）
5. **页面拆分** — 超过 ~2000 字的页面拆分为子页面
6. **孤儿检测** — 没有入链的页面标记为需要关注

编译规则：
- 新信息与已有 wiki 内容**一致** → 补充到已有页面
- 新信息与已有 wiki 内容**矛盾** → 在两处都标注矛盾
- 新信息是全新主题 → 创建新页面
- 新信息包含意图 → 提取到 prospective/

#### 三时机协调

关键问题：ingest 时已经把信息织入了 wiki，compile 再处理同一事件时怎么办？

- **ingest 做局部整理**：只处理新信息与已有页面的直接关联（1-3 个页面）
- **compile 做全局整理**：蒸馏（多条 → 一条）、矛盾检测、交叉引用补全、页面拆分
- 不重复：ingest 写入的内容已经在 wiki 中了，compile 读到后将其作为已有内容来处理
- compile 处理的不是"把事件搬到 wiki"，而是"从全局视角优化 wiki 的结构和质量"

### 5. 前瞻记忆（P0）

来源：大脑前额叶维持"未来意图"，在遇到匹配线索时自动激活。不是一个你会去查的清单，而是一个在对的时刻自己弹出来的触发器。

```markdown
# prospective/notify-alice-on-drive9-release.md
<!-- compiled_from: evt_042 -->

## 意图
当 drive9 发布 v1.0 时，通知 Alice

## 触发条件
- 对话中提到 "drive9 release" / "drive9 v1.0" / "drive9 发布"

## 动作
提醒用户: "你之前说过要在 drive9 发布时通知 Alice"

## 来源
[evt_042] 2026-04-10 对话
```

每次 query 时，query_agent 扫描 prospective/ 目录检查是否有触发条件匹配当前上下文。

### 6. Wiki 页面约定

每个页面：
- 顶部元数据: `<!-- compiled_from: evt_042, evt_055 -->` `<!-- last_compiled: 2026-04-12T15:00:00Z -->`
- 每个事实标注来源: `[evt_xxx]`
- 跨页链接: `[[semantic/people/alice.md]]`
- 矛盾标注: `⚠️ CONFLICT: 与 [[semantic/projects/db9.md#表设计]] 矛盾`

### 7. Source Trust（P0）

来源：记忆系统需要知道信息来源的可信度。用户直接说的 > 工具推断的 > 二手信息。

每个 event 带 trust 字段：
- **source_type**: user / assistant / tool / system / compiler
- **evidence_kind**: direct_observation / user_statement / inferred / compiler_synthesis
- **trust_tier**: 1（高，用户直接陈述）/ 2（中，工具输出/推断）/ 3（低，二手信息）

Wiki 页面的每条 semantic fact 保留来源 trust 信息：
```markdown
- 采用分区表 [evt_042 T1] [evt_055 T2] [evt_061 T1]
  （3 个来源，trust 范围 T1-T2）
```

### 8. Metamemory（P2）

来源："系统知道自己为什么知道"。不是模仿人脑自我感知，而是给调用方提供记忆质量信号。

元信息分两处存储：

**wiki 页面 frontmatter**（静态，不频繁变化）：
```markdown
<!-- compiled_from: evt_042, evt_055, evt_061 -->
<!-- last_compiled: 2026-04-12T15:00:00Z -->
```

**sidecar（`.meta/`）**（动态，随检索和编译变化）：
```json
{
  "created_at": "2026-04-01T10:00:00Z",
  "last_accessed": "2026-04-12T15:00:00Z",
  "access_dates": ["2026-04-01", "2026-04-03", "2026-04-08", "2026-04-12"],
  "source_events": ["evt_042", "evt_055", "evt_061"],
  "trust_tier_max": 1,
  "memory_type": "semantic"
}
```

调用方通过 `read_wiki_page()` 同时获得内容和 sidecar，能判断"这条记忆靠不靠谱（trust_tier）、活不活跃（access_dates）、有几条来源支撑（source_events）"。

v0 实现 compiled_from frontmatter + 完整 sidecar。confidence / open_conflicts 等高级字段 v1 再加。

### 9. Prediction-Error Driven Revision（P2）

来源：人脑不是每次回忆都改记忆，只有"惊讶"（新信息与预期不符）才触发深度重编码。

规则：
- 新 event 与稳定 semantic page 冲突 → 标注矛盾，提高下次 compile 优先级
- 用户显式更正 → 高优先级处理
- 常规新信息 → 正常 compile 周期处理

v0 通过 compile_agent 的矛盾检测实现基础版本，不需要额外机制。

### 10. 记忆衰减与遗忘（P0）

如果 wiki 只增不减，几个月后会有几百个页面，index 路由变得巨大，query_agent 在噪声中找不到重点。记忆系统必须有遗忘机制。

神经科学告诉我们一个反直觉的事实：**遗忘不是记忆的失败，而是记忆系统正常运作的核心特征。**

#### 10.1 神经科学基础

人脑有多种遗忘机制协同工作：

**时间衰减与检索强化**：Ebbinghaus（1885）发现记忆随时间指数衰减，但 Roediger & Karpicke（2006）发现每次成功检索都能显著强化记忆（Testing Effect）。间隔检索（spacing effect）效果更好——分散在多天的 3 次检索，比集中在 1 天的 3 次效果强得多。

**主动遗忘**：Davis & Zhong（2017）发现大脑有专门的遗忘机制——多巴胺能神经元通过 Rac1 信号通路**主动**拆除突触连接。遗忘不是被动衰减，而是主动擦除。当新信息取代旧信息（干扰遗忘）、旧信息被证明错误（反向学习遗忘）时，旧记忆被主动清除。

**语义化**：长期记忆靠 episodic → semantic 转化存活。细节丢失，核心意义保留。70 岁老人记得婚礼很美好，但不记得吃了什么——情节被修剪，语义存活。

**睡眠修剪**：Tononi & Cirelli（2003）的突触稳态假说——白天学习导致突触强度净增长，睡眠时全局下调回到可持续基线。下调不均匀：最强的突触受保护，弱突触被优先修剪。结果是信噪比提升。

#### 10.2 设计原则：LLM 判断驱动，而非公式驱动

**关键设计决策**：不用 Ebbinghaus 公式或机械评分来计算衰减。wiki 页面不是突触——它不会因为你不看就自动降低质量。真正需要的是 **compile_agent（LLM）基于事实数据做判断**，决定哪些该保留、压缩、归档。

理由：
- Ebbinghaus 曲线是对人脑无意义音节记忆的*描述性*模型，不应直接搬来做系统的*规定性*策略
- compile_agent 本身就是 LLM，它能理解内容的语义价值，比任何公式都更准确
- v0 阶段用 LLM 判断 + 简单规则足够验证遗忘机制是否 work

#### 10.3 Sidecar 元数据（与 wiki 内容分离）

每个 wiki 页面的遥测数据存在独立的 sidecar 文件中，**不污染 wiki markdown 内容**。

```
wiki/
├── .meta/                            # sidecar 集中存放（镜像 wiki 目录结构）
│   └── semantic/projects/db9.json
├── semantic/
│   └── projects/
│       └── db9.md                    # wiki 内容（纯 markdown）
```

选择集中存放而非同目录隐藏文件，理由：不会让每个目录都多隐藏文件，rebuild_index 扫描时无需过滤，git diff 更干净。

Sidecar 内容：

```json
{
  "created_at": "2026-04-01T10:00:00Z",
  "last_accessed": "2026-04-12T15:00:00Z",
  "access_dates": ["2026-04-01", "2026-04-03", "2026-04-08", "2026-04-12"],
  "source_events": ["evt_042", "evt_055", "evt_061"],
  "trust_tier_max": 1,
  "memory_type": "semantic"
}
```

wiki 页面本身只保留静态元数据（不会频繁变化的）：

```markdown
<!-- compiled_from: evt_042, evt_055, evt_061 -->
<!-- last_compiled: 2026-04-12T15:00:00Z -->
```

query_agent 每次读取页面时，更新 sidecar 的 `last_accessed` 和 `access_dates`。这个操作是轻量的 JSON 写入，不会修改 wiki 内容。

#### 10.4 按记忆类型的遗忘规则

不同记忆类型有根本不同的遗忘逻辑，不能用同一套公式：

**semantic/**（语义记忆）— **永不因不活跃自动归档**

"巴黎是法国首都"你可能一年不检索，但它不该被归档。semantic 记忆的价值不取决于检索频率。

归档条件（仅限以下情况）：
- 被新信息**明确取代/否定** → compile_agent 标注 `⚠️ SUPERSEDED by [[新页面]]` 并归档
- 用户显式要求遗忘
- compile_agent 蒸馏时发现内容已被合并到另一个页面（去重）

**episodic/**（情景记忆）— **加速衰减，核心通过蒸馏存活**

这是最需要遗忘机制的类型。海马体存储容量有限，episodic 记忆快速衰减是自然规律。

compile_agent 修剪时判断：
- 该 episodic 页面的核心知识是否已蒸馏进 semantic 页面？
  - 是 → 归档 episodic 页面（核心已存活于 semantic 中）
  - 否 → 先蒸馏，再归档
- 例外：特别重要的里程碑事件（用户标记 permanent）保留

**procedural/**（程序记忆）— **极其持久，几乎不衰减**

人脑中 procedural 编码在基底神经节和小脑，不依赖海马体，"骑自行车"几十年不忘。

归档条件：
- 工具/流程已被废弃（compile_agent 发现引用的工具不存在）
- 被新版本的 procedural 页面取代

**prospective/**（前瞻记忆）— **不衰减，只有完成/取消**

前额叶维持意图直到执行或放弃，不存在时间衰减。

移除条件：
- 意图已完成 → 标记完成并归档
- 用户显式取消
- compile_agent 判断意图已过时（如"drive9 v1.0 发布时通知 Alice"但 drive9 已经发布了 v2.0）

#### 10.5 检索强化（简化版）

保留 Testing Effect 的核心洞察，但用 LLM 判断而非公式：

- query_agent 读取页面时 → sidecar 的 `access_dates` 追加当天日期
- compile_agent 修剪时参考 `access_dates` 判断页面活跃度
- **间隔检索比集中检索更有说服力**：如果一个 episodic 页面在过去 30 天内被跨 5 天检索过，compile_agent 应倾向保留；如果只在一天内被检索 5 次后再无检索，倾向蒸馏后归档

这些不是硬编码的数值规则，而是 compile_agent prompt 中的判断指南。LLM 在看到具体数据后做出具体判断。

#### 10.6 归档而非删除

人脑的遗忘也不是真正的擦除——在适当的线索下，"遗忘"的记忆经常可以被恢复。

```
wiki/
├── semantic/          # 活跃 wiki
├── episodic/
├── procedural/
├── prospective/
└── archive/           # 归档区
    ├── episodic/      # 主要是蒸馏后的 episodic 页面
    ├── procedural/    # 被废弃的操作步骤
    └── semantic/      # 被取代的旧知识（极少）
```

归档规则：
- 被归档的页面从 index.md 中移除（不再出现在路由中）
- query_agent 正常检索不会命中，但 search_wiki() 可以搜到
- 如果被显式查询到并使用了 → 恢复到活跃 wiki
- archive/ 页面保留原始的 compiled_from 和 sidecar 元数据

#### 10.7 睡眠修剪的完整流程

compile_agent 在"睡眠时整理"阶段，除了蒸馏和矛盾检测外，增加修剪步骤：

```
compile_agent 睡眠修剪:
  1. 读取所有活跃 wiki 页面及其 sidecar 元数据
  2. 对每个页面，compile_agent（LLM）综合以下信息做判断:
     - memory_type（semantic/episodic/procedural/prospective）
     - 创建时间、最后访问时间
     - access_dates 的分布（频率 + 间隔）
     - source_events 数量和 trust_tier
     - 页面内容本身的语义价值
  3. 按记忆类型分别处理:

     episodic/:
       → 核心已蒸馏进 semantic → 归档
       → 未蒸馏 → 先蒸馏，再归档
       → permanent 标记 → 保留

     semantic/:
       → 被新信息取代 → 标注 SUPERSEDED，归档
       → 内容重复（多页合一） → 合并，归档被合并的页面
       → 其他 → 保留（semantic 不因不活跃归档）

     procedural/:
       → 引用的工具/流程已废弃 → 归档
       → 被新版本取代 → 归档
       → 其他 → 保留

     prospective/:
       → 意图已完成/过时 → 归档
       → 其他 → 保留

  4. 执行归档操作（移入 archive/）
  5. rebuild_index()
  6. 记录修剪日志（哪些页面被归档/压缩/合并，原因）
```

#### 10.8 设计决策记录

本节设计经过 Claude 与 Codex（mem:2）的跨模型辩论，最终综合如下：

| 议题 | Claude 原方案 | mem:2 批评 | 最终决策 |
|------|-------------|-----------|---------|
| 衰减模型 | 单一 stability 分数 + Ebbinghaus 公式 | 公式是描述性的不应做规定性策略 | **接受批评**：去掉公式，用 LLM 判断 |
| 遥测存储 | wiki frontmatter (HTML comment) | 频繁变化的数据不应污染内容 | **接受批评**：sidecar JSON 文件 |
| semantic 衰减 | 和其他类型一样按 stability 衰减 | semantic 不应因不活跃自动归档 | **接受批评**：semantic 永不自动归档 |
| 检索诱导遗忘 | query 读 A 时降低竞争者 B | 太激进，易误伤 | **接受批评**：v0 不实现 RIF |
| 替代方案 | — | 4 独立指标（activation/evidence/freshness/retention） | **不采用**：过重，v0 用 LLM 判断更简单有效 |
| 检索强化 | spacing effect 公式 | — | **简化**：保留概念，用 access_dates 数据+LLM 判断 |

---

## 工具函数接口（9 个）

```
# 写入 raw
append_event(content, actor, source, session_id, active_project, active_task,
             durability, actionability, source_type, evidence_kind, trust_tier) → event_id

# 读取 raw（供 compile_agent）
read_events_since(cursor) → {events, new_cursor}

# 读取 wiki（供所有 agent）
read_wiki_index() → index.md 内容
read_wiki_page(path) → 页面内容 + sidecar 元数据（自动更新 last_accessed）
search_wiki(query) → grep 匹配结果

# 写入 wiki（供所有 agent）
write_wiki_page(path, content) → status（自动创建/更新 sidecar）
archive_wiki_page(path, reason) → status（移入 archive/，更新 sidecar，从 index 移除）
rebuild_index() → status  # 全量重建 index.md（跳过 archive/ 和 .meta/）

# 统计
get_memory_stats() → {event_count, uncompiled_count, wiki_page_count, archived_page_count}
```

### Sidecar 行为（嵌入已有工具，对 agent 透明）

- **read_wiki_page(path)**: 返回 `{content, meta}` — content 是 markdown 内容，meta 是 sidecar JSON。同时自动更新 sidecar 的 `last_accessed` 和追加当天到 `access_dates`。
- **write_wiki_page(path, content)**: 写入 markdown 内容。如果 sidecar 不存在则创建（设置 created_at, memory_type）；如果已存在则保留不动。
- **archive_wiki_page(path, reason)**: 将页面从 `wiki/{type}/` 移入 `wiki/archive/{type}/`，sidecar 追加 `archived_at` 和 `archive_reason`，从 index 中移除。

agent 不需要直接操作 sidecar — 通过已有工具自动处理。

### Agent 工具权限

| Agent | 可用工具 | 读 | 写 |
|-------|---------|---|---|
| **ingest_agent** | append_event, read_wiki_index, read_wiki_page, write_wiki_page | wiki | raw + wiki |
| **query_agent** | read_wiki_index, read_wiki_page, search_wiki, write_wiki_page | wiki | wiki（修错/补链接/归档新知识） |
| **compile_agent** | read_events_since, read_wiki_index, read_wiki_page, write_wiki_page, archive_wiki_page, rebuild_index | raw + wiki | wiki + archive |
| **orchestrator** | get_memory_stats | stats | — |

### 存储层接口化

v0 实现：
- **raw 层**: 内存缓存 + JSONL 文件持久化。append_event 同步追加到 `raw/events.jsonl`，启动时加载到内存。
- **wiki 层**: 本地文件系统。read/write 直接操作 markdown 文件。
- **sidecar 层**: `.meta/` 目录下的 JSON 文件，read_wiki_page 自动读取和更新。

工具函数接口是稳定的抽象层。接口不变的情况下，v1 可以换 SQLite（raw 层）或任何存储后端。

---

## 三条核心 Flow

### Flow 1: Ingest（编码时整理）

```
调用方: remember("Alice 建议 db9 用分区表", context={project: "db9"})
    → ingest_agent (LLM):
      1. 理解内容，提取: actor=user, active_project=db9,
         durability=long-term, actionability=actionable,
         source_type=user, evidence_kind=user_statement, trust_tier=1
      2. append_event() 写入 raw
      3. read_wiki_index() 读路由表
      4. 判断: 与 semantic/projects/db9.md 和 semantic/people/alice.md 相关
      5. read_wiki_page("semantic/projects/db9.md") 读已有内容
      6. 将"Alice 建议用分区表 [evt_xxx T1]"织入 db9.md 的表设计章节
      7. write_wiki_page("semantic/projects/db9.md", updated_content)
      8. 如果是完整经历，创建 episodic/2026-04-12/db9-partition-suggestion.md
    → 返回确认 + 告知更新了哪些页面
```

### Flow 2: Query（回忆时整理）

```
调用方: recall("Alice 和 Bob 对 db9 架构有什么不同看法？")
    → query_agent (LLM):
      1. read_wiki_index() 读路由表
      2. 判断: alice.md, bob.md, db9.md 相关
      3. 扫描 prospective/index.md 检查触发条件
      4. read_wiki_page() 读取三个页面
      5. 重构式回答（综合多页面 + 当前上下文，不是复制粘贴）
      6. 发现: 两人观点对立但 wiki 中未记录比较
         → write_wiki_page("semantic/projects/db9-architecture-debate.md", analysis)
      7. 发现: alice.md 缺少到 db9.md 的交叉引用
         → write_wiki_page("semantic/people/alice.md", 补上 [[db9.md]] 链接)
    → 返回综合回答 + 引用来源 [evt_xxx] + 告知更新了哪些页面
```

### Flow 3: Compile（睡眠时整理）

```
定时触发（每 N 分钟）
    → compile_agent (LLM):

      === 阶段一：蒸馏新事件 ===
      1. read_events_since(cursor) 读未处理事件
      2. 对每个事件:
         a. 判断记忆类型（semantic/episodic/procedural/prospective）
         b. 定位目标 wiki 页面
         c. read_wiki_page() 读现有内容（含 sidecar）
         d. 蒸馏: 多条 episodic → semantic 知识点
         e. 矛盾检测: 新信息是否与已有内容冲突
         f. write_wiki_page() 写回
      3. 交叉引用补全: 扫描页面发现应有但缺失的链接

      === 阶段二：睡眠修剪 ===
      4. 扫描所有活跃 wiki 页面，read_wiki_page() 读内容 + sidecar
      5. 按记忆类型判断每个页面的处置:
         - episodic: 核心已蒸馏 → archive_wiki_page()
         - semantic: 被取代/重复 → archive_wiki_page()（不因不活跃归档）
         - procedural: 已废弃 → archive_wiki_page()
         - prospective: 已完成/过时 → archive_wiki_page()
      6. 记录修剪日志

      === 阶段三：收尾 ===
      7. rebuild_index() 重建路由表
      8. 更新 cursor
```

---

## 人脑记忆优化点总结

| # | 优化点 | 人脑原理 | 系统对应 | 优先级 | 状态 |
|---|--------|---------|---------|--------|------|
| 1 | 存储上下文线索 | Tulving 编码特异性（1973） | raw event 带 active_project, active_task, session_id | P0 | 已设计 |
| 2 | 按记忆类型分区 wiki | Tulving 五种记忆系统（1972） | semantic/ episodic/ procedural/ prospective/ | P0 | 已设计 |
| 3 | 前瞻记忆 | 前额叶维持意图 + 触发条件 | prospective/ 目录 + 每次 query 扫描触发 | P0 | 已设计 |
| 4 | 两维标记 | 杏仁核/海马体多维度标记 | durability + actionability | P0 | 已设计 |
| 5 | 回忆是重构不是回放 | Bartlett 重构记忆（1932） | query_agent 综合多页面 + 当前上下文重构答案 | P0 | 已设计 |
| 6 | 再巩固 | Nader（2000）回忆后记忆可被更新 | query_agent 发现错误/缺失/新知识时直接修 wiki | P0 | 已设计 |
| 7 | Source Trust | 来源可信度影响记忆权重 | event 带 trust_tier, evidence_kind | P0 | 已设计 |
| 8 | Metamemory | 系统知道自己为什么知道 | wiki 页面 frontmatter 带元信息 | P2 | 概念设计 |
| 9 | Prediction-Error Revision | 惊讶驱动深度重编码 | 矛盾检测 + 优先 compile | P2 | 概念设计 |
| 10 | 记忆衰减与遗忘 | 主动遗忘(Rac1) + 检索强化 + 睡眠修剪(SHY) + 语义化 | LLM 判断驱动 + 按类型规则 + sidecar 遥测 + 归档机制 | P0 | 已设计（综合 Claude + Codex 辩论） |

---

## 实现计划

### 模块划分

| 模块 | 职责 |
|------|------|
| **存储层** | 9 个工具函数实现（raw 事件存储、wiki 读写、sidecar 管理、归档操作） |
| **Agent 层** | 4 个 agent 定义（ingest/query/compile/orchestrator 的 prompt + tool 绑定） |
| **编译逻辑** | compile_agent 的蒸馏、矛盾检测、睡眠修剪流程 |
| **HTTP API 层** | 对外 REST API（remember/recall/compile/status） |

### 数据目录结构

```
data/
├── raw/                              # 原始事件日志（持久化存储）
│   └── events.jsonl                  # append-only，每行一个 JSON 事件
└── wiki/                             # 编译输出
    ├── index.md                      # 自动生成的路由表
    ├── .meta/                        # sidecar 元数据（镜像 wiki 目录结构）
    ├── archive/                      # 归档区（被遗忘的页面）
    ├── semantic/
    ├── episodic/
    ├── procedural/
    └── prospective/
```

### 实现步骤

**Step 1: 存储层**
- 实现 9 个工具函数（含 archive_wiki_page）
- read_wiki_page 自动读取并更新 sidecar
- write_wiki_page 自动创建 sidecar
- archive_wiki_page 移入 archive/ + 更新 sidecar + 从 index 移除
- rebuild_index 跳过 archive/ 和 .meta/
- raw 事件持久化到 JSONL 文件，启动时加载

**Step 2: Agent 定义**
- ingest_agent: prompt + append_event + read_wiki_* + write_wiki_page
- query_agent: prompt + read_wiki_* + search_wiki + write_wiki_page
- compile_agent: prompt + read_events_since + read_wiki_* + write_wiki_page + archive_wiki_page + rebuild_index
- orchestrator: get_memory_stats + 路由到子 agent

**Step 3: 编译逻辑**
- 蒸馏: episodic → semantic
- 记忆类型判断
- 矛盾检测
- 睡眠修剪: 读 sidecar → LLM 判断 → 归档/保留
- index.md 全量重建

**Step 4: HTTP API + 测试**
- REST API: `POST /remember`, `POST /recall`, `POST /compile`, `GET /status`
- 手动测试流程:
  1. remember 几条 → 检查 raw events + wiki 更新 + sidecar 创建
  2. recall → 检查重构式回答 + wiki 修复/归档 + sidecar last_accessed 更新
  3. compile → 检查蒸馏质量 + index 重建 + 睡眠修剪
- 验证三时机整理是否都 work

### 关键验证点

v0 需要回答的核心问题：

1. **编码时整理是否可靠？** — ingest_agent 能否正确定位关联页面并织入新信息
2. **重构式回答质量** — query_agent 是否真的在重构而不是复制粘贴
3. **回忆时修复/归档质量** — query_agent 能否准确判断何时该修 wiki、修的对不对
4. **蒸馏质量** — compile_agent 能否将多条 episodic 压缩为有结构的 semantic
5. **index.md 路由准确性** — 所有 agent 都能通过 index 找到正确页面
6. **矛盾检测** — 新信息与已有 wiki 冲突时能否被发现和标注
7. **前瞻记忆触发** — prospective 页面的触发条件能否被正确扫描和匹配
8. **三时机协调** — ingest 写了页面后 compile 不会重复处理

---

## 设计演进记录

| 版本 | 关键变化 | 原因 |
|------|---------|------|
| v3 | 21 表 SQL-first | 过度工程 |
| v4 | 纯 JSONL + wiki | 隐含数据库问题 |
| v5-early | 异步 compile 唯一整理 | 录音机 + 定时整理员，不像记忆 |
| v5-三时机 | ingest/query 也整理，直接写 wiki | Karpathy 原始模式 |
| v5-signal | 三时机但 hint/signal 间接层 | mem:2 单写者建议，但过度间接 |
| **v5-final** | **三时机 + 三 agent 都直接写 wiki + 完整 LLM** | 恢复 Karpathy，v0 单进程无并发 |

---

## 关键参考文件

- Google Always-On Memory Agent: `/tmp/google-memory-agent.py`
- Karpathy LLM Wiki 方法论: `llm-wiki-pattern.md`
- v4 设计文档: `/tmp/agent-memory-v4-hybrid.md`
- 人脑记忆研究分析: `/tmp/brain-inspired-memory-design.md`
