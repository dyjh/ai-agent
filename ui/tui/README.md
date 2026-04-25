# Local Agent TUI

Standalone terminal UI for the local Agent backend. It calls only public HTTP and WebSocket APIs.

```bash
cd ui/tui
go run ./cmd/agent-tui
```

Options:

```bash
go run ./cmd/agent-tui --server http://127.0.0.1:8765
```

Environment:

```env
AGENT_API_BASE_URL=http://127.0.0.1:8765
```

Commands inside the TUI:

- `:help`
- `:new`
- `:approvals`
- `:approve <approval_id>`
- `:reject <approval_id> [reason]`
- `:runs`
- `:run <run_id>`
- `:memory <query>`
- `:kb <kb_id> <question>`
- `:evals`
- `:diff`
- `:quit`

Any normal line is sent as a chat message through the active conversation WebSocket. If WebSocket is unavailable, the TUI falls back to `POST /v1/conversations/{id}/messages`.

