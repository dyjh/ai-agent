import type {
  ApprovalRecord,
  Citation,
  Conversation,
  HealthResponse,
  JsonObject,
  JsonValue,
  ListResponse,
  Message,
  RunState,
  RunStep
} from '../types/domain';

export type paths = {
  '/v1/health': { get: { responses: { 200: HealthResponse } } };
  '/v1/conversations': {
    get: { responses: { 200: ListResponse<Conversation> } };
    post: { requestBody: { title?: string; project_key?: string }; responses: { 201: Conversation } };
  };
  '/v1/conversations/{conversation_id}/messages': {
    get: { responses: { 200: ListResponse<Message> } };
    post: { requestBody: { content: string }; responses: { 200: JsonObject } };
  };
  '/v1/approvals/pending': { get: { responses: { 200: ListResponse<ApprovalRecord> } } };
  '/v1/runs': { get: { responses: { 200: ListResponse<RunState> } } };
  '/v1/runs/{run_id}/steps': { get: { responses: { 200: ListResponse<RunStep> } } };
};

export type components = {
  schemas: {
    ApprovalRecord: ApprovalRecord;
    Citation: Citation;
    Conversation: Conversation;
    HealthResponse: HealthResponse;
    JsonObject: JsonObject;
    JsonValue: JsonValue;
    Message: Message;
    RunState: RunState;
    RunStep: RunStep;
  };
};

