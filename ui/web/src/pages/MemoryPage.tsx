import { useEffect, useState } from 'react';

import type { ApiClient } from '../api/client';
import { ToolOutput } from '../components/ToolOutput';
import { StatusBadge } from '../components/StatusBadge';
import { asArray } from '../utils/format';

interface MemoryPageProps {
  api: ApiClient;
}

export function MemoryPage({ api }: MemoryPageProps) {
  const [items, setItems] = useState<unknown[]>([]);
  const [review, setReview] = useState<unknown[]>([]);
  const [filters, setFilters] = useState({ scope: '', type: '', project_key: '', status: '', tag: '', query: '' });
  const [text, setText] = useState('');
  const [lastResponse, setLastResponse] = useState<unknown>();
  const [error, setError] = useState('');

  const query = new URLSearchParams(Object.entries(filters).filter(([, value]) => value)).toString();
  const load = async () => {
    const [itemsResponse, reviewResponse] = await Promise.all([
      api.get(`/v1/memory/items${query ? `?${query}` : ''}`),
      api.get('/v1/memory/review')
    ]);
    setItems(asArray(itemsResponse));
    setReview(asArray(reviewResponse));
  };

  useEffect(() => {
    load().catch((err) => setError(err instanceof Error ? err.message : String(err)));
  }, []);

  const create = async () => {
    const response = await api.post('/v1/memory/items', {
      text,
      scope: filters.scope || 'user',
      type: filters.type || 'preference',
      project_key: filters.project_key,
      confidence: 0.8,
      importance: 0.5,
      tags: filters.tag ? [filters.tag] : []
    });
    setLastResponse(response);
    setText('');
    await load();
  };

  const action = async (path: string, body: unknown = {}) => {
    const response = await api.post(path, body);
    setLastResponse(response);
    await load();
  };

  return (
    <div className="two-column wide-left">
      <section className="panel">
        <header className="panel-header">
          <h2>Memory Items</h2>
          <button type="button" className="button" onClick={load}>Refresh</button>
        </header>
        {error ? <div className="error-banner">{error}</div> : null}
        <div className="form-grid">
          {Object.keys(filters).map((key) => (
            <label className="field" key={key}>
              <span>{key}</span>
              <input value={filters[key as keyof typeof filters]} onChange={(event) => setFilters({ ...filters, [key]: event.target.value })} />
            </label>
          ))}
        </div>
        <label className="field">
          <span>New memory text</span>
          <textarea value={text} onChange={(event) => setText(event.target.value)} rows={3} />
        </label>
        <button type="button" className="button primary" onClick={create} disabled={!text.trim()}>Create through approval</button>
        <div className="data-grid">
          {items.map((item, index) => {
            const row = item as Record<string, unknown>;
            return (
              <article className="data-card" key={String(row.id ?? index)}>
                <header>
                  <strong>{String(row.text ?? row.id ?? 'memory item')}</strong>
                  <StatusBadge status={String(row.status ?? 'unknown')} />
                </header>
                <p>{String(row.scope ?? '-')}/{String(row.type ?? '-')} {String(row.project_key ?? '')}</p>
                <div className="action-row compact-actions">
                  <button type="button" className="button" onClick={() => action(`/v1/memory/items/${row.id}/archive`)}>Archive</button>
                  <button type="button" className="button" onClick={() => action(`/v1/memory/items/${row.id}/restore`)}>Restore</button>
                </div>
              </article>
            );
          })}
        </div>
      </section>
      <section className="panel">
        <h2>Review Queue</h2>
        <div className="data-grid">
          {review.map((item, index) => {
            const row = item as Record<string, unknown>;
            return (
              <article className="data-card" key={String(row.id ?? row.review_id ?? index)}>
                <strong>{String(row.text ?? row.candidate_text ?? row.id ?? 'review item')}</strong>
                <div className="action-row compact-actions">
                  <button type="button" className="button primary" onClick={() => action(`/v1/memory/review/${row.id ?? row.review_id}/approve`)}>Approve</button>
                  <button type="button" className="button danger" onClick={() => action(`/v1/memory/review/${row.id ?? row.review_id}/reject`, { reason: 'rejected from web UI' })}>Reject</button>
                </div>
                <ToolOutput title="Review detail" output={row} />
              </article>
            );
          })}
        </div>
        <ToolOutput title="Last response" output={lastResponse} defaultOpen />
      </section>
    </div>
  );
}

