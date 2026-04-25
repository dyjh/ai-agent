export type JsonPrimitive = string | number | boolean | null;
export type JsonValue = JsonPrimitive | JsonValue[] | { [key: string]: JsonValue };
export type JsonObject = { [key: string]: JsonValue };

export interface ListResponse<T> {
  items?: T[];
  [key: string]: unknown;
}

export interface Conversation {
  id: string;
  title?: string;
  project_key?: string;
  created_at?: string;
  updated_at?: string;
}

export interface Message {
  id: string;
  conversation_id?: string;
  role: 'user' | 'assistant' | 'system' | 'tool' | string;
  content: string;
  created_at?: string;
}

export interface ToolProposal {
  id?: string;
  tool?: string;
  input?: Record<string, unknown>;
  purpose?: string;
  created_at?: string;
}

export interface PolicyDecision {
  action?: string;
  risk_level?: string;
  reason?: string;
  approval_payload?: Record<string, unknown>;
  policy_profile?: string;
  risk_trace?: unknown;
}

export interface ApprovalExplanation {
  summary?: string;
  why_needed?: string;
  expected_effects?: string[];
  affected_targets?: string[];
  rollback_plan?: string;
  safety_notes?: string[];
  risk_level?: string;
}

export interface ApprovalRecord {
  id: string;
  status?: string;
  tool?: string;
  summary?: string;
  reason?: string;
  risk_level?: string;
  snapshot_hash?: string;
  input_snapshot?: Record<string, unknown>;
  proposal?: ToolProposal;
  decision?: PolicyDecision;
  explanation?: ApprovalExplanation;
  run_id?: string;
  created_at?: string;
  resolved_at?: string;
}

export interface ToolResult {
  tool_call_id?: string;
  output?: Record<string, unknown>;
  error?: string;
  ok?: boolean;
}

export interface RunState {
  run_id: string;
  conversation_id?: string;
  status?: string;
  user_message?: string;
  approval_id?: string;
  final_answer?: string;
  error?: string;
  proposal?: ToolProposal;
  policy?: PolicyDecision;
  tool_result?: ToolResult;
  created_at?: string;
  updated_at?: string;
  completed_at?: string;
}

export interface RunStep {
  id?: string;
  step_id?: string;
  run_id?: string;
  step_index?: number;
  status?: string;
  summary?: string;
  proposal?: ToolProposal;
  policy?: PolicyDecision;
  approval?: ApprovalRecord;
  tool_result?: ToolResult;
  error?: string;
  started_at?: string;
  completed_at?: string;
}

export interface HealthResponse {
  status?: string;
  database?: unknown;
  qdrant?: unknown;
  knowledge_base?: unknown;
  workflow?: unknown;
  docs?: unknown;
  [key: string]: unknown;
}

export interface Citation {
  title?: string;
  source_file?: string;
  url?: string;
  section?: string;
  chunk_id?: string;
  score?: number;
  updated_at?: string;
  snippet?: string;
  [key: string]: unknown;
}

