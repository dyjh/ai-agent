# Local Agent UI

This directory contains user interfaces for the local single-user Agent. Both interfaces talk only to the public HTTP and WebSocket APIs exposed by the backend.

## Layout

```text
ui/
  web/   React + TypeScript + Vite web UI
  tui/   Standalone Go terminal UI
```

The UI does not import Go `internal/` packages, execute shell commands, call local Skill/MCP processes directly, or edit memory files. Actions with external effects still go through the backend ToolRouter, policy engine, approval center, executor, and audit log.

## Web UI

```bash
cd ui/web
npm install
npm run dev
```

Optional environment:

```env
VITE_AGENT_API_BASE_URL=http://127.0.0.1:8765
VITE_AGENT_WS_BASE_URL=ws://127.0.0.1:8765
```

The web app covers chat streaming, run timeline, approval cards, diff preview, memory, knowledge base/RAG, skills, MCP, ops, eval/replay, security, health, and docs entry points.

## TUI

```bash
cd ui/tui
go run ./cmd/agent-tui
```

Optional environment:

```env
AGENT_API_BASE_URL=http://127.0.0.1:8765
```

The TUI is a lightweight terminal client for chat, approval review, runs, memory search, KB answer, eval summaries, and diff preview.

