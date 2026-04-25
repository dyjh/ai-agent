import type {
  ApprovalRecord,
  Conversation,
  HealthResponse,
  ListResponse,
  Message,
  RunState,
  RunStep
} from '../types/domain';

export type HttpMethod = 'GET' | 'POST' | 'PATCH' | 'DELETE';

export interface ApiClientOptions {
  baseUrl?: string;
  timeoutMs?: number;
  fetchImpl?: typeof fetch;
}

export interface RequestOptions {
  timeoutMs?: number;
  signal?: AbortSignal;
}

export class ApiError extends Error {
  status: number;
  code?: string;
  details?: unknown;

  constructor(message: string, status: number, code?: string, details?: unknown) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
    this.details = details;
  }
}

const defaultBaseUrl = import.meta.env.VITE_AGENT_API_BASE_URL || 'http://127.0.0.1:8765';

function joinUrl(baseUrl: string, path: string): string {
  const base = baseUrl.endsWith('/') ? baseUrl.slice(0, -1) : baseUrl;
  const suffix = path.startsWith('/') ? path : `/${path}`;
  return `${base}${suffix}`;
}

async function parseBody(response: Response): Promise<unknown> {
  const text = await response.text();
  if (!text) {
    return null;
  }
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}

function messageFromErrorBody(body: unknown, fallback: string): { message: string; code?: string } {
  if (!body || typeof body !== 'object') {
    return { message: fallback };
  }
  const obj = body as Record<string, unknown>;
  return {
    message: String(obj.message ?? obj.error ?? fallback),
    code: typeof obj.code === 'string' ? obj.code : undefined
  };
}

export function createApiClient(options: ApiClientOptions = {}) {
  const baseUrl = options.baseUrl || defaultBaseUrl;
  const timeoutMs = options.timeoutMs ?? 15000;
  const fetcher = options.fetchImpl ?? fetch;

  async function request<T>(method: HttpMethod, path: string, body?: unknown, requestOptions: RequestOptions = {}): Promise<T> {
    const controller = new AbortController();
    const timeout = window.setTimeout(() => controller.abort(), requestOptions.timeoutMs ?? timeoutMs);
    const outerSignal = requestOptions.signal;
    const abortFromOuter = () => controller.abort();
    outerSignal?.addEventListener('abort', abortFromOuter, { once: true });

    const headers: HeadersInit = {};
    let payload: BodyInit | undefined;
    if (body instanceof FormData) {
      payload = body;
    } else if (body !== undefined) {
      headers['content-type'] = 'application/json';
      payload = JSON.stringify(body);
    }

    try {
      const response = await fetcher(joinUrl(baseUrl, path), {
        method,
        headers,
        body: payload,
        signal: controller.signal
      });
      const parsed = await parseBody(response);
      if (!response.ok) {
        const error = messageFromErrorBody(parsed, response.statusText || 'request failed');
        throw new ApiError(error.message, response.status, error.code, parsed);
      }
      return parsed as T;
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') {
        throw new ApiError('request timed out or was cancelled', 0, 'request_timeout');
      }
      throw error;
    } finally {
      window.clearTimeout(timeout);
      outerSignal?.removeEventListener('abort', abortFromOuter);
    }
  }

  return {
    baseUrl,
    request,
    get: <T>(path: string, options?: RequestOptions) => request<T>('GET', path, undefined, options),
    post: <T>(path: string, body?: unknown, options?: RequestOptions) => request<T>('POST', path, body, options),
    patch: <T>(path: string, body?: unknown, options?: RequestOptions) => request<T>('PATCH', path, body, options),
    delete: <T>(path: string, options?: RequestOptions) => request<T>('DELETE', path, undefined, options),
    health: () => request<HealthResponse>('GET', '/v1/health'),
    listConversations: () => request<ListResponse<Conversation>>('GET', '/v1/conversations'),
    createConversation: (title?: string) => request<Conversation>('POST', '/v1/conversations', { title }),
    listMessages: (conversationId: string) => request<ListResponse<Message>>('GET', `/v1/conversations/${conversationId}/messages`),
    postMessage: (conversationId: string, content: string) => request<Record<string, unknown>>('POST', `/v1/conversations/${conversationId}/messages`, { content }),
    pendingApprovals: () => request<ListResponse<ApprovalRecord>>('GET', '/v1/approvals/pending'),
    approve: (approvalId: string) => request<Record<string, unknown>>('POST', `/v1/approvals/${approvalId}/approve`, {}),
    reject: (approvalId: string, reason: string) => request<Record<string, unknown>>('POST', `/v1/approvals/${approvalId}/reject`, { reason }),
    listRuns: (query = '') => request<ListResponse<RunState>>('GET', `/v1/runs${query}`),
    getRun: (runId: string) => request<RunState>('GET', `/v1/runs/${runId}`),
    listRunSteps: (runId: string) => request<ListResponse<RunStep>>('GET', `/v1/runs/${runId}/steps`),
    resumeRun: (runId: string, approvalId: string, approved: boolean) => request<Record<string, unknown>>('POST', `/v1/runs/${runId}/resume`, { approval_id: approvalId, approved }),
    cancelRun: (runId: string) => request<RunState>('POST', `/v1/runs/${runId}/cancel`, {})
  };
}

export type ApiClient = ReturnType<typeof createApiClient>;
export const apiClient = createApiClient();

