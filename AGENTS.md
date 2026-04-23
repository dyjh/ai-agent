# AGENTS.md

## Project

This repository implements a personal local Codex-like Agent system.

The project must be implemented in Go and use CloudWeGo Eino as the Agent / LLM application framework.

The system is for single-user local deployment. Do not add tenant logic, organization logic, multi-user RBAC, or cloud-first assumptions unless explicitly requested.

## Core Principle

The model must not directly operate the external world.

The LLM may only produce structured Tool Proposals. All external effects must go through:

1. Tool Router
2. Effect Inference
3. Policy Engine
4. Approval Center
5. Executor
6. Event/Audit Log

Read-only operations may auto-execute if policy allows and confidence is high enough.

Write operations, installs, sensitive reads, dangerous operations, network writes, and unknown/low-confidence actions must request approval.

Approval applies to an exact input snapshot. After approval, the executor must run only that approved snapshot. If the command, path, parameters, files, effects, or risk level change, request approval again.

## Eino Usage

Use Eino for:

- ChatModel integration
- Agent orchestration
- Tool abstraction
- Retriever / Embedding integration
- Workflow / Graph / ADK patterns
- Streaming and callbacks
- Human-in-the-loop interrupt/resume design

Do not let Eino tools directly execute shell commands or write files.

Eino tool adapters should create Tool Proposals and pass them into the local Tool Router.

## No Dead Keyword Routing

Do not implement user intent or command risk decisions using simple fixed keyword matching as the primary mechanism.

Use:

- Intent Planner
- Tool Proposal
- Effect Inference
- Tool manifest effects
- Command structure analysis
- Sensitive resource analysis
- Policy Engine
- Low-confidence fallback to approval

Hard guardrails may exist only as a safety fallback.

## Memory

Long-term memory and preference memory use Markdown as the source of truth.

PostgreSQL stores only:

- conversations
- messages
- message usage
- conversation usage rollups
- optional lightweight agent_events

Qdrant is only a vector index, not a source of truth.

Do not store long-term memory正文 in PostgreSQL.

## Security

Never store secrets, tokens, passwords, private keys, cookies, or full `.env` contents in:

- messages
- JSONL logs
- agent_events
- Markdown memory
- Qdrant payloads

Sensitive reads require approval.

Unknown effects require approval.

Low confidence requires approval.

## Shell Execution

Never pass ordinary LLM text directly to os/exec.

Shell execution is allowed only through:

ToolProposal -> EffectInference -> PolicyEngine -> ApprovalCenter when needed -> ShellExecutor.

Read-only shell commands may execute automatically only when high-confidence and policy-approved.

Write, install, dangerous, sensitive, unknown, or low-confidence shell commands must require user approval.

After approval, execute only approval.input_snapshot.

## Testing

Add or update tests for:

- policy decisions
- effect inference
- shell structure analysis
- Markdown memory store
- approval snapshot behavior
- API smoke flows

Run `go test ./...` before completing a task when practical.

## Style

Prefer small, clear packages.

Use typed Go structs and interfaces.

Keep executors separate from planners.

Do not let planner code execute tools directly.

Do not let LLM response text become executable shell commands without structured validation and policy.
