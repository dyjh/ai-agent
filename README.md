# Local Agent 使用指南

Local Agent 是一个本地单用户的 Codex-like Agent。它提供 Web UI、TUI、CLI 和 HTTP/WebSocket API，用于日常对话、代码任务、知识库问答、长期记忆、Skill/MCP 扩展、运维排查、审批和回放评测。

项目使用 Go + CloudWeGo Eino 构建。所有会影响外部世界的动作都必须经过：

```text
Tool Proposal -> Effect Inference -> Policy Engine -> Approval Center -> Executor -> Event/Audit Log
```

模型不会直接执行 shell、写文件、调用 Skill/MCP 或操作远端系统。只读且高置信的操作可以按策略自动执行；写入、安装、敏感读取、危险操作、网络写入、未知或低置信操作会要求你审批。

## 适合做什么

- 和本地 Agent 进行流式对话。
- 查看 run timeline、工具输出和审批请求。
- 让 Agent 读取、搜索、检查代码，运行受控测试，预览或申请应用 patch。
- 管理 Markdown 长期记忆和偏好。
- 建立知识库，上传/同步资料，并进行带引用的 RAG 问答。
- 安装和运行本地 Skill，但仍通过审批链路执行。
- 管理 MCP server 和工具策略。
- 做本机、SSH、Docker、Kubernetes 的结构化运维排查。
- 运行 eval/replay，用于安全策略和行为回归检查。

## 环境要求

- Go 1.25+。
- Docker 和 Docker Compose，推荐用于启动 PostgreSQL 与本地 Qdrant；也可以改用 Pinecone 或 OpenAI Vector Stores。
- Node.js/npm，仅在使用 Web UI 时需要。
- 一个 OpenAI-compatible Chat Completions 服务，或本地 Ollama。没有真实模型配置时，后端会回退到 mock 响应，适合 smoke test，不适合真实使用。

## 快速开始

1. 准备配置文件：

```bash
cp .env.example .env
```

2. 编辑 `.env`，至少配置一种对话模型。

OpenAI-compatible 示例：

```env
LLM_PROVIDER=openai_compatible
OPENAI_BASE_URL=https://api.openai.com/v1
OPENAI_API_KEY=...
OPENAI_MODEL=gpt-4.1-mini
```

Ollama 示例：

```env
LLM_PROVIDER=ollama
OLLAMA_BASE_URL=http://127.0.0.1:11434
OLLAMA_MODEL=qwen2.5-coder:7b
```

如果要使用知识库/RAG，保持：

```env
USE_KONWAGE_BASE=true
KONWAGE_BASE_PROVIDER=qdrant # qdrant / pinecone / openai
QDRANT_URL=http://127.0.0.1:6333
QDRANT_COLLECTION_KB=kb_chunks
QDRANT_COLLECTION_MEMORY=memory_chunks
QDRANT_COLLECTION_CODE=code_chunks
QDRANT_RECREATE_ON_DIMENSION_MISMATCH=false
```

Pinecone provider 需要配置 `PINECONE_INDEX_HOST`、`PINECONE_API_KEY` 和 namespace；OpenAI provider 使用 Vector Stores，需要配置 `OPENAI_VECTOR_STORE_KB`，如果记忆也走 OpenAI 后端还需要 `OPENAI_VECTOR_STORE_MEMORY`。

不需要知识库时可以设置：

```env
USE_KONWAGE_BASE=false
```

3. 将 `.env` 导入当前 shell：

```bash
set -a
source .env
set +a
```

4. 启动依赖服务：

```bash
make docker-up
```

这会启动 PostgreSQL 和 Qdrant。没有 PostgreSQL 时，服务会回退到内存仓储，但 run/step 不能跨进程恢复。

5. 启动后端：

```bash
make server
```

默认地址是 `http://127.0.0.1:8765`。

6. 检查服务状态：

```bash
go run ./cmd/agent health
```

Swagger 文档入口：

```text
http://127.0.0.1:8765/swagger/index.html
```

## Web UI

Web UI 位于 `ui/web`，只通过公开 HTTP/WebSocket API 调用后端。

```bash
make ui-install
make ui-dev
```

Vite 默认会输出本地访问地址，通常是：

```text
http://127.0.0.1:5173
```

可选配置：

```env
VITE_AGENT_API_BASE_URL=http://127.0.0.1:8765
VITE_AGENT_WS_BASE_URL=ws://127.0.0.1:8765
```

Web UI 覆盖 Chat、Runs、Approvals、Code、Memory、Knowledge、Skills、MCP、Ops、Eval 和 Settings。

## TUI

TUI 位于 `ui/tui`，适合在终端里使用：

```bash
make tui
```

也可以直接指定后端：

```bash
cd ui/tui
go run ./cmd/agent-tui --server http://127.0.0.1:8765
```

TUI 内常用命令：

```text
:help
:new
:approvals
:approve <approval_id>
:reject <approval_id> [reason]
:runs
:run <run_id>
:memory <query>
:kb <kb_id> <question>
:evals
:diff
:quit
```

普通输入会发送为聊天消息。WebSocket 不可用时，TUI 会回退到普通 HTTP 消息接口。

## CLI 常用命令

所有 CLI 命令默认连接 `http://127.0.0.1:8765`，也可以用 `--server` 指定。

```bash
go run ./cmd/agent ask "帮我总结当前项目"
go run ./cmd/agent chat
go run ./cmd/agent approvals list
go run ./cmd/agent approvals approve <approval_id>
go run ./cmd/agent approvals reject <approval_id>
```

代码与 Git：

```bash
go run ./cmd/agent code inspect .
go run ./cmd/agent code search . "ToolProposal"
go run ./cmd/agent code test .
go run ./cmd/agent git status .
go run ./cmd/agent git diff-summary .
```

记忆与知识库：

```bash
go run ./cmd/agent memory list
go run ./cmd/agent memory search "我的偏好"
curl -X POST http://127.0.0.1:8765/v1/kbs \
  -H 'Content-Type: application/json' \
  -d '{"name":"my-kb","description":"local notes"}'
go run ./cmd/agent kb --kb-id <kb_id> upload README.md
go run ./cmd/agent kb --kb-id <kb_id> search "项目安全边界是什么"
go run ./cmd/agent kb answer <kb_id> "如何启动系统"
```

Skill、MCP、运维和评测：

```bash
go run ./cmd/agent skills list
go run ./cmd/agent mcp list
go run ./cmd/agent ops hosts list
go run ./cmd/agent ops docker ps
go run ./cmd/agent eval run
go run ./cmd/agent eval report latest
```

## 审批怎么工作

当 Agent 提出需要审批的操作时，你会在 Web UI、TUI、CLI 或 API 中看到 approval。

重点规则：

- 审批绑定精确的 `input_snapshot`。
- 批准后 Executor 只执行该 snapshot。
- 如果命令、路径、参数、文件、effect 或风险等级变化，必须重新审批。
- 想修改命令、patch 或参数时，应拒绝当前 approval，再发起新的请求。
- UI/TUI 只展示并提交 approve/reject，不会绕过后端安全链路。

## 数据位置

- `memory/`：长期记忆和偏好，Markdown 是事实源。
- `knowledge/`：知识库 registry、上传文件和 source 记录。
- `runs/`：run JSONL 事件。
- `audit/`：审计事件。
- `evals/`：eval cases、runs 和 reports。
- `config/`：后端、策略、Skill/MCP 配置。
- `data/`：Docker 启动的 PostgreSQL/Qdrant 数据目录，已被 `.gitignore` 忽略。

不要把 token、密码、私钥、cookie、完整 `.env` 内容写入 memory、knowledge、eval、run log 或审计日志。

## HTTP API

常用入口：

- `GET /v1/health`
- `POST /v1/conversations`
- `POST /v1/conversations/{id}/messages`
- `GET /v1/conversations/{id}/ws`
- `GET /v1/approvals/pending`
- `POST /v1/approvals/{id}/approve`
- `POST /v1/approvals/{id}/reject`
- `GET /v1/runs`
- `GET /v1/runs/{id}/steps`
- `/v1/memory/*`
- `/v1/kbs/*`
- `/v1/skills/*`
- `/v1/mcp/*`
- `/v1/ops/*`
- `/v1/security/*`
- `/v1/evals/*`

完整 API 请看：

```text
http://127.0.0.1:8765/swagger/index.html
```

## 测试与构建

后端：

```bash
make test
```

Web UI：

```bash
make ui-test
make ui-build
```

TUI：

```bash
make tui-test
```

生成 Swagger/OpenAPI：

```bash
make swagger
```

## 当前边界

- 这是本地单用户系统，不内置云端多租户、组织、RBAC 或公网认证模型。
- Web UI/TUI 是日常使用首版，不是完整 IDE。
- Code 自动修复质量依赖模型和上下文，patch 应用仍必须走审批。
- Ops rollback 多数是 best-effort 描述，不能保证恢复外部系统状态。
- Qdrant/Pinecone/OpenAI Vector Stores 是向量索引，不是事实源；Markdown memory 和原始知识库 source 才是事实源。
- Eval/replay 默认使用 safe-mode/mock 路径，不能完全代表真实外部环境。
