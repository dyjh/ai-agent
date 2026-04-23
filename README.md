# Local Agent

个人本地部署的 Codex-like Agent 系统，技术栈为 Go + CloudWeGo Eino。目标是单用户、本地优先、可审计、可审批、可扩展，不引入租户、多用户或 RBAC 复杂度。

## 项目目标

- 提供 HTTP API、WebSocket Chat API 和 Cobra CLI
- 提供会话、消息、usage、JSONL 事件日志
- 使用 CloudWeGo Eino 封装 ChatModel 入口
- 使用 Tool Proposal -> Effect Inference -> Policy Engine -> Approval Center -> Executor 的安全主链路
- 提供 Markdown Memory、知识库、Skill/MCP 管理的 MVP 能力

## 架构说明

核心模块：

- `internal/agent/`: Runtime、Planner、ContextBuilder、EffectInference、Policy、Approval
- `internal/einoapp/`: Eino ChatModel 封装、Tool Adapter、Callbacks 占位
- `internal/tools/`: ToolRegistry、Router、shell/code/memory/kb/skills/mcp
- `internal/db/`: goose migration、pgx 仓储、内存仓储 fallback
- `internal/api/`: HTTP Router、Handlers、WebSocket
- `internal/events/`: run/audit JSONL

主链路：

1. 用户消息进入 Runtime
2. Planner 产出直接回答或 ToolProposal
3. ToolRouter 调用 EffectInference 和 PolicyEngine
4. 读操作高置信自动执行
5. 写操作、敏感读、未知效果进入 ApprovalCenter
6. Executor 只执行审批通过的 `input_snapshot`
7. 事件写入 WebSocket、`runs/*.jsonl`、`audit/*.jsonl`

## 为什么模型不直接操作外部世界

如果让模型直接 `exec.Command`、直接写文件或直接调用外部系统，系统会失去最重要的三个边界：

- 可审计性：用户无法知道模型到底执行了什么
- 可复现性：审批前后输入可能漂移
- 可控性：高风险动作无法在执行前被拦住

本项目要求模型只生成结构化 `ToolProposal`。真正执行必须经过效果推断、策略判定、审批和快照执行。

## Eino 在本系统中的角色

当前 MVP 中，Eino 负责：

- ChatModel 封装：`internal/einoapp/chat_model.go`
- Runtime 的 LLM 入口：`internal/einoapp/agent_runner.go`
- ToolProposal 适配器：`internal/einoapp/tools_adapter.go`
- 后续 Graph / Workflow / Callback / Retriever 扩展点预留

当前没有让 Eino Tool 直接执行外部动作，所有外部 effect 仍由本系统 ToolRouter 控制。

## 安装步骤

1. 准备 Go 1.23+，并允许自动下载较新的 Go toolchain
2. 复制环境变量模板：

```bash
cp .env.example .env
```

3. 填写模型相关配置：

```env
OPENAI_BASE_URL=
OPENAI_API_KEY=
OPENAI_MODEL=
DATABASE_URL=postgresql://agent:agent@localhost:5432/local_agent
QDRANT_URL=http://localhost:6333
QDRANT_API_KEY=
```

4. 选择向量后端模式：

- `vector.backend=memory`：本地 smoke / 测试优先，不依赖 Qdrant
- `vector.backend=qdrant`：走真实 Qdrant 索引；如果 `fallback_to_memory=true`，Qdrant 不可用时会自动降级到内存索引并输出 warning

Qdrant 只作为向量索引，不是事实源。长期记忆事实源仍然是 `memory/*.md`，知识库事实源仍然是上传的 KB 文档内容。

## 启动 PostgreSQL 和 Qdrant

```bash
docker compose up -d
```

## 运行数据库迁移

```bash
export DATABASE_URL=postgresql://agent:agent@localhost:5432/local_agent
make migrate-up
```

迁移文件位于 [internal/db/migrations/001_init.sql](/www/wwwroot/ai-agent/internal/db/migrations/001_init.sql)。

## 启动 Agent Server

```bash
make run
```

默认监听：

- `GET /v1/health`
- `GET /v1/kbs/health`
- `POST /v1/conversations`
- `POST /v1/conversations/{conversation_id}/messages`
- `GET /v1/approvals/pending`
- `GET /v1/conversations/{conversation_id}/ws`

## 使用 CLI

```bash
make cli
go run ./cmd/agent health
go run ./cmd/agent ask "你好"
go run ./cmd/agent chat
go run ./cmd/agent approvals list
go run ./cmd/agent memory list
go run ./cmd/agent memory search "偏好"
go run ./cmd/agent skills list
go run ./cmd/agent mcp list
```

## WebSocket 示例

连接：

```text
WS /v1/conversations/{conversation_id}/ws
```

发送用户消息：

```json
{
  "type": "user.message",
  "content": "帮我看一下 CPU 占用第三高的进程"
}
```

服务端会按顺序发送：

```json
{ "type": "run.started", "run_id": "run_xxx" }
{ "type": "assistant.delta", "content": "我会先查询当前进程的 CPU 使用情况。" }
{ "type": "tool.output", "payload": { "stdout": "..." } }
{ "type": "assistant.message", "content": "工具执行完成，输出如下：..." }
{ "type": "run.completed" }
```

审批响应：

```json
{
  "type": "approval.respond",
  "approval_id": "apr_xxx",
  "approved": true
}
```

## 知识库上传示例

向量后端配置示例：

```yaml
kb:
  enabled: true
  registry_path: ./knowledge/registry.yaml

vector:
  backend: qdrant # memory | qdrant
  fallback_to_memory: true
  embedding_dimension: 1536
  distance: cosine

qdrant:
  url: http://localhost:6333
  api_key: ${QDRANT_API_KEY}
  timeout_seconds: 10
  collections:
    kb: kb_chunks
    memory: memory_chunks
    code: code_chunks
```

1. 创建 KB：

```bash
curl -X POST http://127.0.0.1:8765/v1/kbs \
  -H 'Content-Type: application/json' \
  -d '{"name":"docs","description":"local docs"}'
```

2. 上传文档：

```bash
curl -X POST http://127.0.0.1:8765/v1/kbs/<kb_id>/documents/upload \
  -H 'Content-Type: application/json' \
  -d '{"filename":"intro.md","content":"# Title\n\nhello world"}'
```

3. 搜索：

```bash
curl -X POST http://127.0.0.1:8765/v1/kbs/<kb_id>/search \
  -H 'Content-Type: application/json' \
  -d '{"query":"hello","limit":5}'
```

4. 查看 KB / 向量后端健康状态：

```bash
curl http://127.0.0.1:8765/v1/kbs/health
```

当前 `kb.search` 已接入真实 ToolRegistry executor，并通过 `ToolRouter -> EffectInference -> PolicyEngine -> Executor` 链路执行。该工具默认 effects 为 `kb.read`，高置信只读查询会自动执行。

## Memory Markdown 说明

- Memory 文件位于 `memory/`
- `Markdown` 是长期记忆事实源，PostgreSQL 不存正文
- `memory.patch` 会把候选 patch 落到 `memory/pending/`
- `POST /v1/memory/reindex` 会把 Markdown 内容重新送入向量索引

## Skill 上传说明

当前 Skill 管理支持：

- `POST /v1/skills/upload`
- `GET /v1/skills`
- `GET /v1/skills/{id}`
- `POST /v1/skills/{id}/enable`
- `POST /v1/skills/{id}/disable`
- `POST /v1/skills/{id}/test`
- `POST /v1/skills/{id}/run`

当前 Skill Runtime 已支持通过 `skill.yaml` 声明本地 skill，并通过 `skill.run -> ToolRouter -> EffectInference -> PolicyEngine -> Executor` 链路执行。

- 当前支持的 runtime 类型：`executable`、`script`
- 当前支持的输入模式：`json_stdin`、`args`、`env`
- 当前支持的输出模式：`json_stdout`、`text`
- manifest `effects` 会参与 effect inference 和 policy 判定
- 只读高置信 skill 可自动执行
- 写入、敏感、未知 effect，或 `approval.default=require` 的 skill 会进入审批
- `POST /v1/skills/{id}/run` 会走与 Agent 内部相同的 ToolRouter / Approval 链路

当前上传接口接受本地 skill 目录或 `skill.yaml` 路径；zip 解包和更强的执行沙箱仍留待后续阶段。

## MCP 配置说明

当前 MCP runtime 支持：

- `GET /v1/mcp/servers`
- `POST /v1/mcp/servers`
- `PATCH /v1/mcp/servers/{id}`
- `POST /v1/mcp/servers/{id}/refresh`
- `POST /v1/mcp/servers/{id}/test`
- `POST /v1/mcp/servers/{id}/tools/{tool_name}/call`
- `GET /v1/mcp/tools`
- `PATCH /v1/mcp/tools/{id}/policy`

MCP server 配置从 `config/mcp.servers.yaml` 加载，tool policy override 从 `config/mcp.tool-policies.yaml` 加载，并支持环境变量展开。当前支持两种 transport：

- `stdio`：启动本地 MCP 子进程，通过 stdin/stdout 做 line-delimited JSON-RPC 调用
- `http`：向配置的 MCP URL 发送 JSON-RPC HTTP 请求，并支持 headers、timeout 和 health check

`mcp.call_tool` 已接入统一安全链路：

```text
ToolProposal -> EffectInference -> PolicyEngine -> ApprovalCenter -> MCP Executor
```

本地 tool policy override 会优先参与调用前决策，例如：

```yaml
tools:
  mcp.filesystem.read_file:
    effects:
      - fs.read
    approval: auto

  mcp.filesystem.write_file:
    effects:
      - fs.write
    approval: require
```

判定规则：

- 本地 override 优先于 server 返回的 schema / metadata
- unknown MCP tool 默认 `unknown.effect`，必须审批
- `fs.write`、`network.post`、敏感读、危险操作必须审批
- 全只读且高置信 MCP tool 可自动执行
- `POST /v1/mcp/servers/{id}/tools/{tool_name}/call` 必须走 ToolRouter，不能绕过审批链路

已知限制：当前 stdio/http transport 已有可测试 MVP 实现，但 MCP 协议细节仍按 JSON-RPC `tools/list` / `tools/call` 的最小封装处理；后续如接入更多 MCP server 方言，应只扩展 transport 层，不应把协议分支散落到 handler 或 router。

## 审批机制说明

- 读操作且高置信：自动执行
- 写操作、敏感读、未知效果、低置信：生成 approval
- approval 绑定的是精确 `input_snapshot`
- 审批通过后，Executor 只会执行批准过的那个 snapshot
- 如果命令、参数、目录、目标文件变化，必须重新审批

## Shell 执行安全机制说明

`shell.exec` 不会直接从模型普通文本进入执行器，而是：

1. Planner 产生结构化 `ToolProposal`
2. Shell parser 做结构分析
3. EffectInference 判定 `read / write / sensitive / unknown`
4. PolicyEngine 决定自动执行或审批
5. ApprovalCenter 保存快照
6. Shell Executor 执行审批后的确切输入

已实现的结构识别包括：

- pipeline
- redirect write
- possible file target
- 敏感路径识别
- 常见进程查询、git 读写、package install、filesystem mutation 分类

## 测试命令

```bash
go test ./...
```

当前已覆盖：

- policy decisions
- effect inference
- shell parser
- markdown memory
- qdrant store / vector factory / kb.search tool
- skill manifest / runner / policy
- mcp config / transport / policy / call_tool / API
- approval snapshot behavior
- API smoke flows

## 当前 MVP 限制和后续 TODO

- 当前支持 `memory` 和 `qdrant` 两种向量后端；本地测试默认更偏向 `memory`，生产部署可切到 `qdrant`
- Eino Graph / Workflow / interrupt-resume 目前保留了封装入口，尚未做完整编排
- Skill Runtime 已支持本地 manifest + executable/script 执行，但 zip 上传、运行时沙箱和更细粒度权限隔离仍待继续收口
- MCP runtime 已支持 stdio/http 与 `mcp.call_tool`，但更完整的 MCP 协议兼容矩阵仍待后续集成测试扩展
- CLI 已走 HTTP API，但仍是轻量控制台，不包含富交互 TUI

## 任务清单

当前任务进度记录在 [task.json](/www/wwwroot/ai-agent/task.json)。
