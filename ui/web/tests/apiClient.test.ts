import { describe, expect, it, vi } from 'vitest';

import { ApiError, createApiClient } from '../src/api/client';

describe('api client', () => {
  it('returns JSON for successful responses', async () => {
    const fetchImpl = vi.fn(async () => new Response(JSON.stringify({ ok: true }), { status: 200 })) as unknown as typeof fetch;
    const api = createApiClient({ baseUrl: 'http://agent.local', fetchImpl });
    await expect(api.get('/v1/health')).resolves.toEqual({ ok: true });
    expect(fetchImpl).toHaveBeenCalledWith('http://agent.local/v1/health', expect.objectContaining({ method: 'GET' }));
  });

  it('normalizes backend error responses', async () => {
    const fetchImpl = vi.fn(async () => new Response(JSON.stringify({ code: 'bad', message: 'failed' }), { status: 400 })) as unknown as typeof fetch;
    const api = createApiClient({ baseUrl: 'http://agent.local', fetchImpl });
    await expect(api.get('/v1/health')).rejects.toMatchObject({ status: 400, code: 'bad', message: 'failed' } satisfies Partial<ApiError>);
  });

  it('times out slow requests', async () => {
    const fetchImpl = vi.fn((_url: RequestInfo | URL, init?: RequestInit) => new Promise<Response>((_resolve, reject) => {
      init?.signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')));
    })) as unknown as typeof fetch;
    const api = createApiClient({ baseUrl: 'http://agent.local', fetchImpl, timeoutMs: 5 });
    await expect(api.get('/v1/health')).rejects.toMatchObject({ code: 'request_timeout' });
  });
});
