import { useEffect, useState } from 'react';

import type { ApiClient } from '../api/client';
import { StatusBadge } from '../components/StatusBadge';
import { ToolOutput } from '../components/ToolOutput';
import { asArray, formatDate, objectLabel } from '../utils/format';

interface EvalPageProps {
  api: ApiClient;
}

export function EvalPage({ api }: EvalPageProps) {
  const [cases, setCases] = useState<unknown[]>([]);
  const [runs, setRuns] = useState<unknown[]>([]);
  const [category, setCategory] = useState('');
  const [lastResponse, setLastResponse] = useState<unknown>();
  const [report, setReport] = useState<unknown>();
  const [replayRunId, setReplayRunId] = useState('');
  const [error, setError] = useState('');

  const load = async () => {
    const [caseResponse, runResponse] = await Promise.all([
      api.get(`/v1/evals${category ? `?category=${encodeURIComponent(category)}` : ''}`),
      api.get('/v1/evals/runs?limit=20')
    ]);
    setCases(asArray(caseResponse));
    setRuns(asArray(runResponse));
  };

  useEffect(() => {
    load().catch((err) => setError(err instanceof Error ? err.message : String(err)));
  }, []);

  const runEval = async () => {
    const response = await api.post('/v1/evals/run', { category, approval_mode: 'mock' });
    setLastResponse(response);
    await load();
  };

  const loadReport = async (runId: string) => {
    const response = await api.get(`/v1/evals/runs/${runId}/report`);
    setReport(response);
  };

  const replay = async (mode: 'event' | 'behavior') => {
    const response = await api.post(`/v1/runs/${replayRunId}/replay`, { mode });
    setLastResponse(response);
  };

  return (
    <div className="two-column">
      <section className="panel">
        <header className="panel-header">
          <h2>Eval Cases</h2>
          <button type="button" className="button" onClick={load}>Refresh</button>
        </header>
        {error ? <div className="error-banner">{error}</div> : null}
        <label className="field inline">
          <span>Category</span>
          <select value={category} onChange={(event) => setCategory(event.target.value)}>
            <option value="">all</option>
            <option value="chat">chat</option>
            <option value="rag">rag</option>
            <option value="code">code</option>
            <option value="ops">ops</option>
            <option value="safety">safety</option>
          </select>
        </label>
        <button type="button" className="button primary" onClick={runEval}>Run safe-mode eval</button>
        <div className="data-grid">
          {cases.map((item, index) => {
            const row = item as Record<string, unknown>;
            return (
              <article className="data-card" key={String(row.id ?? row.case_id ?? index)}>
                <strong>{objectLabel(item, 'eval case')}</strong>
                <p>{String(row.category ?? '')}</p>
              </article>
            );
          })}
        </div>
      </section>
      <section className="panel">
        <h2>Runs and Replay</h2>
        <div className="data-grid">
          {runs.map((item, index) => {
            const row = item as Record<string, unknown>;
            const runId = String(row.id ?? row.run_id ?? '');
            return (
              <article className="data-card" key={runId || index}>
                <header>
                  <strong>{runId}</strong>
                  <StatusBadge status={String(row.status ?? '')} />
                </header>
                <p>{formatDate(String(row.started_at ?? row.created_at ?? ''))}</p>
                <button type="button" className="button" onClick={() => loadReport(runId)}>Report</button>
              </article>
            );
          })}
        </div>
        <label className="field">
          <span>Replay run id</span>
          <input value={replayRunId} onChange={(event) => setReplayRunId(event.target.value)} />
        </label>
        <div className="action-row">
          <button type="button" className="button" disabled={!replayRunId} onClick={() => replay('event')}>Event replay</button>
          <button type="button" className="button" disabled={!replayRunId} onClick={() => replay('behavior')}>Behavior replay</button>
        </div>
        <ToolOutput title="Latest eval/replay response" output={lastResponse} defaultOpen />
        <ToolOutput title="Report" output={report} defaultOpen />
      </section>
    </div>
  );
}

