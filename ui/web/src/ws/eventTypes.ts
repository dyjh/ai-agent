import type { ApprovalRecord, JsonObject, RunState, ToolProposal, ToolResult } from '../types/domain';

export type AgentEventType =
  | 'run.started'
  | 'assistant.delta'
  | 'tool.proposed'
  | 'effect.inferred'
  | 'policy.decided'
  | 'approval.requested'
  | 'approval.approved'
  | 'approval.rejected'
  | 'tool.started'
  | 'tool.output'
  | 'tool.completed'
  | 'tool.failed'
  | 'assistant.message'
  | 'run.completed'
  | 'run.failed'
  | 'run.cancelled'
  | 'error';

export interface AgentEvent {
  type: AgentEventType | string;
  run_id?: string;
  step_id?: string;
  tool_call_id?: string;
  approval_id?: string;
  content?: string;
  payload?: JsonObject | Record<string, unknown>;
  proposal?: ToolProposal;
  approval?: ApprovalRecord;
  result?: ToolResult;
  run?: RunState;
  created_at?: string;
}

export interface ChatTranscriptState {
  assistantDraft: string;
  assistantMessages: string[];
  lastRunId?: string;
}

export function reduceAssistantEvent(state: ChatTranscriptState, event: AgentEvent): ChatTranscriptState {
  if (event.type === 'run.started' && event.run_id) {
    return { ...state, lastRunId: event.run_id };
  }
  if (event.type === 'assistant.delta' && event.content) {
    return { ...state, assistantDraft: state.assistantDraft + event.content };
  }
  if (event.type === 'assistant.message') {
    const finalMessage = event.content || state.assistantDraft;
    return {
      ...state,
      assistantDraft: '',
      assistantMessages: finalMessage ? [...state.assistantMessages, finalMessage] : state.assistantMessages
    };
  }
  if (event.type === 'run.completed' || event.type === 'run.failed' || event.type === 'run.cancelled') {
    return { ...state, assistantDraft: '' };
  }
  return state;
}

export function extractApproval(event: AgentEvent): ApprovalRecord | undefined {
  if (event.approval) {
    return event.approval;
  }
  if (event.type !== 'approval.requested') {
    return undefined;
  }
  const payload = event.payload ?? {};
  const approval = payload.approval;
  if (approval && typeof approval === 'object') {
    return approval as ApprovalRecord;
  }
  if (event.approval_id) {
    return {
      id: event.approval_id,
      status: 'pending',
      summary: typeof payload.summary === 'string' ? payload.summary : undefined,
      risk_level: typeof payload.risk_level === 'string' ? payload.risk_level : undefined,
      input_snapshot: typeof payload.input_snapshot === 'object' ? payload.input_snapshot as Record<string, unknown> : undefined,
      snapshot_hash: typeof payload.snapshot_hash === 'string' ? payload.snapshot_hash : undefined
    };
  }
  return undefined;
}

