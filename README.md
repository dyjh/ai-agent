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
- `internal/tools/`: ToolRegistry、Router、shell/code/ops/runbook/memory/kb/skills/mcp
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

## Code Workspace / Patch 能力

当前代码能力已接入 ToolRegistry，并继续走统一安全链路：

```text
ToolProposal -> EffectInference -> PolicyEngine -> ApprovalCenter -> Executor
```

已注册的 code tools：

- `code.list_files`：列出工作区文件，默认跳过 `.git`、`node_modules`、`vendor` 等大目录，并跳过敏感路径
- `code.read_file`：读取工作区普通文件，支持 `max_bytes` 限制
- `code.search` / `code.search_text`：搜索文本并返回文件、行号和匹配行
- `code.search_symbol`：按符号边界搜索源码标识符
- `code.inspect_project`：检测语言、配置文件、测试命令和构建命令
- `code.detect_language`：检测工作区主要语言
- `code.detect_test_command`：检测可能的测试命令，不执行测试
- `code.run_tests`：在工作区内运行 allowlisted 测试命令，支持自动检测、timeout、输出限长和脱敏
- `code.parse_test_failure`：把 Go / pytest / Node / Rust 的常见失败输出解析为结构化失败信息
- `code.fix_test_failure_loop`：运行测试、解析失败，并输出有界修复循环状态、失败上下文和下一步 `code.propose_patch` 指引；可携带上一轮 test runs / patch 摘要、识别审批拒绝和 max iteration 停止；不会自动写文件
- `code.propose_patch`：生成单文件、多文件 full-file replacement 或 unified diff 的 diff preview、文件 hash、changed files 和冲突摘要，不写文件
- `code.validate_patch` / `code.dry_run_patch`：校验 full-file replacement 或 unified diff，检测 hash mismatch、hunk-level conflict、路径逃逸、敏感路径，并返回 diff statistics / rollback metadata
- `code.apply_patch`：审批后应用 full-file replacement 或已校验的 unified diff，支持 `expected_sha256` 基线校验、原子写入、失败回滚和 rollback snapshot metadata
- `code.explain_diff`：总结 diff 或 patch payload，不写文件

已注册的 git tools：

- `git.status` / `git.diff` / `git.log` / `git.branch`：固定子命令，只读且高置信时可自动执行
- `git.diff_summary` / `git.commit_message_proposal`：只读生成 diff 摘要和 commit message 建议，不 stage、不 commit、不 push
- `git.add` / `git.commit` / `git.restore`：会改变仓库或工作区状态，必须审批；`git.commit` 执行前会检查 status、staged diff 和非空 message
- `git.clean`：删除未跟踪文件，标记为 `danger`，必须强审批；当前不提供 push/reset-hard 自动路径

安全规则：

- 普通 `code.read_file` / `code.search_text` / `code.list_files` 是高置信只读操作，可按策略自动执行
- 读取 `.env`、private key、credentials、token、cookie、session 等敏感路径会被标记为 `sensitive_read`，必须审批
- 搜索默认跳过敏感文件；只有显式 `include_sensitive=true` 时才允许进入敏感读取审批
- `code.propose_patch` 不修改文件；如果目标是敏感路径，也需要审批
- `code.apply_patch` 一律是 `fs.write + code.modify`，必须审批
- `code.run_tests` 只接受 `go test`、`npm/pnpm/yarn test`、`pytest`、`cargo test`、`make test` 等受控测试命令；包含 install、redirect 或 shell control 的命令会进入审批或被拒绝
- Git 写入类工具的审批快照包含 workspace、paths、message 和固定 git args；审批后只执行该 snapshot，不会自动 push
- 审批绑定具体 patch 输入快照；执行时只使用 `approval.input_snapshot`，并继续校验 `snapshot_hash`
- `code.apply_patch` 的 `expected_sha256` 用于防止审批后目标文件基线漂移

当前边界：

- patch 系统已支持 full-file replacement 和常见 unified diff parser，包括 old/new path、hunk range、hunk context conflict、diff statistics、path escape guard、expected hash guard、dry-run 和 rollback metadata；但还不是完整 `git apply` 兼容解析器
- 已支持 patch 预览、hash guard、hunk conflict 检测、原子写入和失败回滚；交互式 conflict resolution、更复杂 rename 语义和完整 git apply 兼容性仍待后续扩展
- Planner 已能为代码任务生成结构化 `CodePlan`，并把 CodePlan 存入 run state / plan step；每个 step 仍必须转成 ToolProposal 后走统一安全链路
- `code.fix_test_failure_loop` 当前提供多轮修复循环状态接口、max iteration 限制、审批拒绝停止、失败上下文和下一步 patch proposal 指引；真正 patch 内容仍依赖 planner/model 生成，且 `code.apply_patch` 必须审批
- `code.apply_patch` 审批恢复后，workflow 可继续 rerun tests 并解析下一轮失败；拒绝审批会停止当前 run，不会写文件
- CLI 的 `agent code ... <workspace>` / `agent git ... <workspace>` 会把 workspace hint 传入 Planner 生成的 ToolProposal；所有实际读取、测试、Git 操作仍由 workspace/path guard 校验
- 自动修复质量取决于模型和上下文，不保证所有测试失败都能自动修好

## Ops Capability / 运维能力

当前运维能力已接入 ToolRegistry，并继续走统一安全链路：

```text
ToolProposal -> EffectInference -> PolicyEngine -> ApprovalCenter -> Executor
```

Host Profile：

- `GET/POST/PATCH/DELETE /v1/ops/hosts` 管理本地运维目标，默认提供 `local` host
- 支持 `local`、`ssh`、`docker`、`k8s` 类型校验；SSH key path、password ref、kubeconfig path 和敏感 metadata 在 API 输出中脱敏
- `POST /v1/ops/hosts/{host_id}/test` 提供安全连接/配置检查；不存储私钥、密码或 kubeconfig 正文

已注册的 Ops tools：

- Local：`ops.local.system_info`、`ops.local.processes`、`ops.local.disk_usage`、`ops.local.memory_usage`、`ops.local.network_info`、`ops.local.service_status`、`ops.local.logs_tail`、`ops.local.service_restart`
- SSH：`ops.ssh.system_info`、`ops.ssh.processes`、`ops.ssh.disk_usage`、`ops.ssh.memory_usage`、`ops.ssh.logs_tail`、`ops.ssh.service_status`、`ops.ssh.service_restart`
- Docker：`ops.docker.ps`、`ops.docker.inspect`、`ops.docker.logs`、`ops.docker.stats`、`ops.docker.restart`、`ops.docker.stop`、`ops.docker.start`
- Kubernetes：`ops.k8s.get`、`ops.k8s.describe`、`ops.k8s.logs`、`ops.k8s.events`、`ops.k8s.apply`、`ops.k8s.delete`、`ops.k8s.rollout_restart`
- Runbook：`runbook.list`、`runbook.read`、`runbook.plan`、`runbook.execute_step`、`runbook.execute`

运维审批策略：

- system/process/disk/memory/network/service/log/container/k8s 只读工具高置信自动执行
- 读取 `.env`、private key、credentials、token、cookie、session 等敏感日志路径会标记为 `sensitive_read` 并要求审批
- service restart、SSH restart、Docker restart/stop/start、K8s apply/delete/rollout restart 必须审批
- Ops 写操作的 approval payload 包含影响目标、`rollback_plan`；K8s apply 还包含 `manifest_summary`
- Docker/K8s/SSH 工具使用固定子命令/固定模板，不暴露任意 `docker`、`kubectl`、`ssh` 或 `shell.exec` 替代入口

Runbook：

- Markdown runbook 默认目录为 `runbooks/`
- `GET /v1/ops/runbooks`、`GET /v1/ops/runbooks/{id}`、`POST /v1/ops/runbooks/{id}/plan`、`POST /v1/ops/runbooks/{id}/execute`
- `runbook.plan` 只生成 dry-run 计划；`runbook.execute` 将每个可执行 step 转成 ToolProposal 后继续经过 ToolRouter / Policy / Approval
- runbook 内容只是普通操作指南，不是系统指令，不能覆盖安全规则

CLI：

```bash
go run ./cmd/agent ops hosts list
go run ./cmd/agent ops hosts add --name local-dev
go run ./cmd/agent ops hosts add-ssh --name prod --host 10.0.0.10 --user deploy --auth-type agent
go run ./cmd/agent ops hosts test local
go run ./cmd/agent ops docker ps
go run ./cmd/agent ops docker logs web
go run ./cmd/agent ops docker restart web
go run ./cmd/agent ops k8s get pods
go run ./cmd/agent ops k8s logs pod/my-app
go run ./cmd/agent ops k8s apply deploy.yaml
go run ./cmd/agent ops runbooks list
go run ./cmd/agent ops runbooks plan diagnose-local-high-cpu
```

当前限制：

- 默认测试不依赖真实 SSH/Docker/K8s；真实环境矩阵应通过显式 integration env gate 开启
- Host Profile 当前为进程内管理，后续可按本地单用户模式落盘，但仍不能存储 secret 正文
- 部分 rollback 只能 best-effort 描述，restart/stop/delete 不能保证恢复进程内状态
- Runbook step mapping 仍是保守启发式，后续可接入更强的 Eino/LLM OpsPlan，但每步仍必须走 ToolRouter

## 安装步骤

1. 准备 Go 1.23+，并允许自动下载较新的 Go toolchain
2. 复制环境变量模板：

```bash
cp .env.example .env
```

当前配置通过进程环境变量展开；启动前请将 `.env` 内容导入 shell 环境，或用同等方式传入环境变量。

3. 填写模型相关配置：

```env
OPENAI_BASE_URL=
OPENAI_API_KEY=
OPENAI_MODEL=
DATABASE_URL=postgresql://agent:agent@localhost:5432/local_agent
USE_KONWAGE_BASE=true
KONWAGE_BASE_PROVIDER=qdrant
QDRANT_URL=http://localhost:6333
QDRANT_API_KEY=
```

4. 配置知识库开关和向量后端：

- `USE_KONWAGE_BASE=false`：不初始化 KB runtime，`ContextBuilder` 不检索 KB，`kb.search` 不注册，KB API 返回 `feature_disabled`
- `USE_KONWAGE_BASE=true`：必须设置 `KONWAGE_BASE_PROVIDER=qdrant`
- 当前 runtime KB provider 只支持 `qdrant`，非法 provider 会启动失败
- `vector.backend=memory` 仍保留给 memory index、本地 smoke 和单元测试使用

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
go run ./cmd/agent-server --check-config
make run
```

默认监听：

- `GET /v1/health`
- `GET /swagger/index.html`
- `GET /swagger/doc.json`
- `GET /v1/kbs/health`
- `POST /v1/conversations`
- `POST /v1/conversations/{conversation_id}/messages`
- `GET /v1/approvals/pending`
- `GET /v1/ops/hosts`
- `GET /v1/ops/runbooks`
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
go run ./cmd/agent skills install ./skills/demo.zip
go run ./cmd/agent skills manifest demo
go run ./cmd/agent skills validate demo --args '{"top_n":3}'
go run ./cmd/agent skills run demo --args '{"top_n":3}'
go run ./cmd/agent skills remove demo
go run ./cmd/agent mcp list
go run ./cmd/agent runs list
go run ./cmd/agent runs get run_xxx
go run ./cmd/agent runs steps run_xxx
go run ./cmd/agent runs resume run_xxx --approval apr_xxx --approved=true
go run ./cmd/agent runs cancel run_xxx
go run ./cmd/agent code inspect .
go run ./cmd/agent code search . "ToolRouter"
go run ./cmd/agent code read . internal/app/bootstrap.go
go run ./cmd/agent code test .
go run ./cmd/agent code fix-tests .
go run ./cmd/agent code diff .
go run ./cmd/agent code patch validate change.diff
go run ./cmd/agent code patch dry-run change.diff
go run ./cmd/agent code patch apply change.diff
go run ./cmd/agent git status .
go run ./cmd/agent git diff .
go run ./cmd/agent git diff-summary .
go run ./cmd/agent git commit-message .
go run ./cmd/agent ops hosts list
go run ./cmd/agent ops docker ps
go run ./cmd/agent ops k8s get pods
go run ./cmd/agent ops runbooks list
```

## Swagger / OpenAPI

生成 Swagger 文档：

```bash
make swagger
```

生成文件位于 [docs/swagger.json](/www/wwwroot/ai-agent/docs/swagger.json)，并同步镜像到 [docs/openapi.json](/www/wwwroot/ai-agent/docs/openapi.json) 以兼容现有脚本。服务启动后可访问：

- Swagger UI: `http://127.0.0.1:8765/swagger/index.html`
- Swagger JSON: `http://127.0.0.1:8765/swagger/doc.json`

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
  enabled: ${USE_KONWAGE_BASE}
  provider: ${KONWAGE_BASE_PROVIDER}
  registry_path: ./knowledge/registry.yaml

vector:
  backend: qdrant
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

禁用知识库时：

```env
USE_KONWAGE_BASE=false
KONWAGE_BASE_PROVIDER=
```

此时 runtime 不会构建 KB 检索上下文，`kb.search` 不会出现在 ToolRegistry 中，`POST /v1/kbs`、`POST /v1/kbs/{kb_id}/search` 等接口返回：

```json
{
  "code": "feature_disabled",
  "message": "knowledge base is disabled"
}
```

启用知识库时：

```env
USE_KONWAGE_BASE=true
KONWAGE_BASE_PROVIDER=qdrant
QDRANT_URL=http://localhost:6333
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
- `POST /v1/skills/upload-zip`
- `GET /v1/skills`
- `GET /v1/skills/{id}`
- `DELETE /v1/skills/{id}`
- `GET /v1/skills/{id}/manifest`
- `GET /v1/skills/{id}/package`
- `POST /v1/skills/{id}/enable`
- `POST /v1/skills/{id}/disable`
- `POST /v1/skills/{id}/test`
- `POST /v1/skills/{id}/validate`
- `POST /v1/skills/{id}/run`

当前 Skill Runtime 已支持通过 `skill.yaml` 声明本地 skill，并通过 `skill.run -> ToolRouter -> EffectInference -> PolicyEngine -> Executor` 链路执行。

- 当前支持的 runtime 类型：`executable`、`script`
- 当前支持的输入模式：`json_stdin`、`args`、`env`
- 当前支持的输出模式：`json_stdout`、`text`
- manifest `effects` 会参与 effect inference 和 policy 判定
- 只读高置信 skill 可自动执行
- 写入、敏感、未知 effect，或 `approval.default=require` 的 skill 会进入审批
- `POST /v1/skills/{id}/run` 会走与 Agent 内部相同的 ToolRouter / Approval 链路

当前 Skill 安装支持两种来源：

- `POST /v1/skills/upload`：注册本地 skill 目录或 `skill.yaml` 路径
- `POST /v1/skills/upload-zip`：上传 zip 包，安全解包到 `skills/packages/<skill-id>/<version>/`

zip 安装会执行以下检查：

- 防止 zip slip / path traversal
- 限制文件数、单文件大小和总解压大小
- 解包后必须存在且只能存在一个 `skill.yaml`
- manifest 校验通过后才注册
- 记录 `source_type`、`checksum`、`installed_at` 和 `package_path`
- 同一 `skill_id + version` 默认拒绝重复安装，传 `force=true` 才覆盖

当前支持的 manifest 关键字段示例：

```yaml
id: cpu_analyzer
name: CPU Analyzer
version: 1.0.0

runtime:
  type: executable
  command: ./bin/cpu_analyzer
  cwd: .
  input_mode: json_stdin
  output_mode: json_stdout
  timeout_seconds: 30

effects:
  - process.read
  - system.metrics.read

approval:
  default: auto

permissions:
  filesystem:
    read:
      - .
      - ./workspace
    write: []
  network:
    enabled: false
    allow_hosts: []
  env:
    allow:
      - LANG
  tools:
    allow_shell: false
    allow_mcp: false
  process:
    max_runtime_seconds: 30
    max_output_bytes: 1048576

sandbox:
  profile: restricted # best_effort_local | trusted_local | restricted | linux_isolated
```

当前执行期权限隔离行为：

- `permissions` 会在运行前参与 `runtime.command`、`cwd`、env allowlist 和显式路径参数校验
- `effects` 决定 Policy / Approval；`permissions` 决定执行前允许范围，两者不一致会校验失败
- `GET /v1/skills/{id}/package` 返回安装元数据，不返回 zip 内容
- `POST /v1/skills/{id}/validate` 会做 manifest / permissions / runtime 路径 / 输入的预校验，并返回 `runner`、`sandbox_profile`、`platform_supported`、`will_fallback`、`requires_approval` 等审计信息；validate 不会真正执行 skill

当前 Skill sandbox profile：

- `best_effort_local`：使用 `LocalRestrictedRunner`，提供 timeout、max output、受控 env、`cwd`/command 路径校验和显式路径参数预检查
- `trusted_local`：仍走本地 runner 和 preflight 校验，但明确表示这是宿主机信任执行，不提供 OS 级隔离
- `restricted`：优先尝试 `LinuxIsolatedRunner`；若当前平台或运行时条件不满足，则显式 fallback 到 `best_effort_local`，并强制审批
- `linux_isolated`：仅在 Linux 上可用；当前要求 rootfs-compatible 的 `runtime.command`，不满足时直接拒绝，不静默降级

当前 Linux stronger runner 能力：

- 通过 user/mount/pid/ipc/uts namespace 提供更强执行边界
- 当 `permissions.network.enabled=false` 时，会额外使用独立 network namespace 关闭外网访问
- 当 `runtime.command` 可在 skill root 内独立运行时，会对 skill root 做 `chroot`，把文件系统范围收紧到 skill 包目录
- 会继续应用 timeout、max output bytes、env allowlist，并在结果中返回 runner metadata
- `permissions.network.allow_hosts` 目前仍无法做内核级 host allowlist enforcement；若声明该能力，会产生 warning，`restricted` 路径会强制审批，`linux_isolated` 路径会拒绝

当前沙箱能力的真实边界：

- `LocalRestrictedRunner` 仍是 best-effort 本地执行器，不是容器级或 syscall 级强隔离
- `LinuxIsolatedRunner` 比 best-effort 更强，但当前仍不是完整容器沙箱；尚未实现 seccomp、cgroup、bind mount rootfs、host allowlist 和动态依赖自动封装
- shell script 或依赖宿主解释器/动态加载器的 skill，可能无法使用 `chroot` 文件系统收紧；此时系统会显式 warning，并要求审批或拒绝
- 当前不会把 best-effort enforcement 包装成“已完成强隔离”；不同 profile 的实际边界以上述规则为准

## MCP 配置说明

当前 MCP runtime 支持：

- `GET /v1/mcp/servers`
- `POST /v1/mcp/servers`
- `GET /v1/mcp/servers/{id}`
- `PATCH /v1/mcp/servers/{id}`
- `POST /v1/mcp/servers/{id}/refresh`
- `POST /v1/mcp/servers/{id}/test`
- `POST /v1/mcp/servers/{id}/tools/{tool_name}/call`
- `GET /v1/mcp/tools`
- `PATCH /v1/mcp/tools/{id}/policy`

MCP server 配置从 `config/mcp.servers.yaml` 加载，tool policy override 从 `config/mcp.tool-policies.yaml` 加载，并支持环境变量展开。当前支持两种 transport：

- `stdio`：启动本地 MCP 子进程，通过 stdin/stdout 做 line-delimited JSON-RPC 调用
- `http`：向配置的 MCP URL 发送 JSON-RPC HTTP 请求，并支持 headers、timeout 和 health check

MCP transport 层现在支持显式 dialect / compatibility profile。默认是保守的 `strict_jsonrpc`，也可按 server 配置选择：

- `strict_jsonrpc`：标准 JSON-RPC 2.0 `tools/list` / `tools/call`
- `line_delimited_jsonrpc`：接受 line-delimited JSON-RPC 响应，主要用于 stdio 或兼容型 HTTP server
- `envelope_wrapped`：接受 `data` / `payload` / `response` 外层包裹的结果

示例：

```yaml
servers:
  - id: filesystem
    name: filesystem
    enabled: true
    transport: stdio
    command: node
    args:
      - ./mcp/filesystem/index.js
    cwd: ./mcp/filesystem
    dialect: line_delimited_jsonrpc
    compatibility:
      accept_missing_schema: true
      accept_extra_metadata: true
      accept_text_only_result: true
      strict_id_matching: false

  - id: local-http-tools
    name: local-http-tools
    enabled: true
    transport: http
    url: http://localhost:3001/mcp
    headers:
      Authorization: ${MCP_LOCAL_TOKEN}
    dialect: strict_jsonrpc
```

兼容层目前集中在 `internal/tools/mcp` 的 transport / decode helper 内，handler、ToolRouter、policy 和 approval 不感知具体方言。conformance 测试覆盖了 strict、line-delimited、envelope、text-only result、malformed payload、JSON-RPC error、timeout 和 stdio 进程异常。错误会收敛为 `invalid_response`、`timeout`、`transport_failure`、`protocol_violation`、`server_error` 等结构化 MCP error code，并继续做敏感内容脱敏。

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

已知限制：当前兼容矩阵覆盖常见 JSON-RPC 结果形态和错误场景，但不是“完全兼容所有 MCP server”。SSE、流式 tool result、更复杂的 capability negotiation、外部真实 server 集成矩阵仍属于后续扩展；新增方言应继续只扩展 transport / compatibility layer，不应把协议分支散落到 handler 或 router。

## 审批机制说明

- 读操作且高置信：自动执行
- 写操作、敏感读、未知效果、低置信：生成 approval
- approval 绑定的是精确 `input_snapshot`
- approval 同时保存 `snapshot_hash`，执行前会校验快照未被篡改
- 审批通过后，Executor 只会执行批准过的那个 snapshot
- 如果命令、参数、目录、目标文件变化，必须重新审批
- Agent Workflow 会把需要审批的 run 标记为 `paused_for_approval`
- 审批通过后通过 workflow resume 执行原 approval 的 `input_snapshot`，不会重新生成 ToolProposal
- 当 PostgreSQL 可用时，paused run 的状态和 step 会落到 `agent_runs` / `agent_run_steps`，服务重启后仍可恢复审批中的 run

## Eino Workflow / HITL 编排

当前已提供 `internal/einoapp.AgentWorkflow` facade：

- `Start(ctx, input)`：处理新用户消息，构建上下文、进入 step-based plan loop，并在每个工具步骤都走 effect/policy/approval/executor 链路
- `Resume(ctx, run_id, approval_id, approved)`：处理审批恢复，不重新规划已审批 step，也不替换 tool input

运行状态由 `RunState` 和 `RunStep` 记录。`RunState` 记录当前状态、step 游标、审批关联和最近的 planner 输出；`RunStep` 记录 `build_context / plan / continue / propose_tool / infer_effect / decide_policy / request_approval / execute_tool / summarize / finalize` 等步骤历史。代码任务的 plan step 会额外保存结构化 `CodePlan`，用于 replay 和后续 workflow 分析。

核心状态包括：

- `received_user_message`
- `context_built`
- `planned`
- `tool_proposed`
- `effect_inferred`
- `policy_decided`
- `paused_for_approval`
- `approval_approved`
- `approval_rejected`
- `tool_executing`
- `tool_completed`
- `assistant_summarizing`
- `completed`

当前 workflow 已支持显式 multi-step tool loop：

- tool 结果返回后，planner 可以输出 `continue / stop / answer / tool`
- 每个新 tool proposal 都会重新经过 `ToolRouter -> EffectInference -> PolicyEngine -> ApprovalCenter -> Executor`
- 对代码任务，planner 可以先产出 `CodePlan`，再逐步转换为 `code.*` 或 `git.*` ToolProposal；CodePlan 本身不执行外部动作
- 默认 `MaxWorkflowSteps=6`，超过上限会安全终止并把 run 标记为 failed

RunStateStore 当前是可插拔的：

- `InMemoryRunStateStore`：用于单元测试和无数据库 fallback
- `PersistentRunStateStore`：通过仓库层持久化 run state / steps；在 PostgreSQL 可用时可跨进程恢复审批中的 run

HTTP approval API、`/v1/runs/{run_id}/resume` 和 WebSocket `approval.respond` 已接入同一套 workflow resume；非 workflow 直接工具 API 保留兼容路径。

## Run 管理 API

当前新增的 run 可观测性与恢复接口包括：

- `GET /v1/runs`
- `GET /v1/runs/{run_id}`
- `GET /v1/runs/{run_id}/steps`
- `POST /v1/runs/{run_id}/resume`
- `POST /v1/runs/{run_id}/cancel`

CLI 对应子命令为：

- `agent runs list`
- `agent runs get <run_id>`
- `agent runs steps <run_id>`
- `agent runs resume <run_id> --approval <approval_id> --approved=true`
- `agent runs cancel <run_id>`

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

## 全能型 Agent 路线图

当前仓库已完成 production-ready MVP 和 post-MVP 平台底座，后续按阶段推进为更完整的本地 Agent：

- F0 治理与当前状态对齐：README、task.json、Swagger/OpenAPI 与代码状态保持一致
- F1 代码能力闭环：已完成代码搜索/读取、patch proposal/apply、Git workflow、测试执行和失败修复循环
- F2 运维能力闭环：已完成 local / SSH / Docker / Kubernetes host profile、runbook、rollback plan
- F3 RAG 可靠性增强：当前下一阶段，知识库 connectors、增量索引、hybrid search、rerank、citations、RAG eval
- F4 长期记忆治理：记忆分类、候选审批、查看/编辑/删除、敏感信息防写入
- F5 安全策略与 Secret Guard：统一 secret scanning、策略解释、危险命令更细粒度结构分析
- F6 评测与回放系统：run replay、golden tasks、安全回归、工具链路回放
- F7 Web UI / TUI：本地审批中心、run timeline、memory/KB/code/ops 可视化

## 测试命令

```bash
go test ./...
```

当前已覆盖：

- policy decisions
- effect inference
- shell parser
- markdown memory
- code workspace read/search/project inspection, schema-driven CodePlan, test runner, failure parsing, fix loop state carry/max-iteration stop, unified diff proposal preview, patch validation/dry-run/apply rollback metadata, Git summary/commit pre-check tools, and patch approval behavior
- qdrant store / vector factory / kb.search tool
- config validation / KB feature gate / Swagger docs
- skill manifest / runner / policy
- mcp config / transport / policy / call_tool / API
- approval snapshot behavior and hash integrity
- durable run state / multi-step workflow / persistent resume / run API
- workflow start / pause / resume / reject / idempotency
- ops host profile API, local/SSH/Docker/K8s mock-based tools, runbook planning/execution routing, rollback approval payload, planner ops mapping
- API smoke flows

可选外部依赖集成测试建议通过环境变量显式开启，例如：

```bash
RUN_INTEGRATION=1 go test ./...
RUN_QDRANT_INTEGRATION=1 go test ./...
RUN_POSTGRES_INTEGRATION=1 go test ./...
RUN_SSH_INTEGRATION=1 go test ./...
RUN_DOCKER_INTEGRATION=1 go test ./...
RUN_K8S_INTEGRATION=1 go test ./...
```

## 当前 MVP 限制和后续 TODO

- Runtime KB provider 当前只支持 `qdrant`；`memory` 向量索引用于本地 memory index、测试和开发辅助
- 当前 step loop 已覆盖本地单用户场景下的可恢复多步任务，但仍不是无限自动规划的通用 graph 平台
- PostgreSQL 可用时 run/step 可持久化；无数据库 fallback 仍只提供进程内内存 run state
- Skill Runtime 已支持 zip 安装、manifest permissions 校验，以及 Linux namespace/chroot 优先的 stronger runner；但 seccomp、cgroup、host allowlist 和更完整 rootfs 封装仍待后续阶段补齐
- MCP runtime 已支持 stdio/http、dialect compatibility profile 和 conformance matrix；SSE、流式结果、capability negotiation 和更高保真外部 server 集成矩阵仍待后续扩展
- Code Workspace 已支持读/搜/项目检测、结构化 CodePlan、测试执行、失败解析、Git workflow summary、patch validation/dry-run 和审批后 patch apply；自动修复循环已有有界状态接口和审批恢复后 rerun tests，但仍不保证自动修好所有失败，不做自动写入或自动 push
- Ops Capability 已支持结构化 local/SSH/Docker/K8s 排查、写操作审批、runbook 和 rollback plan；真实外部环境测试需显式 integration env，HostProfile 当前为进程内管理，rollback 对部分操作只能 best-effort
- CLI 已走 HTTP API，但仍是轻量控制台，不包含富交互 TUI

## 任务清单

当前任务进度记录在 [task.json](/www/wwwroot/ai-agent/task.json)。
