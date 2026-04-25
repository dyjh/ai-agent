import { useEffect, useState } from 'react';

import type { ApiClient } from '../api/client';
import { CitationList } from '../components/CitationList';
import { ToolOutput } from '../components/ToolOutput';
import type { Citation } from '../types/domain';
import { asArray, objectLabel } from '../utils/format';

interface KnowledgePageProps {
  api: ApiClient;
}

export function KnowledgePage({ api }: KnowledgePageProps) {
  const [health, setHealth] = useState<unknown>();
  const [kbs, setKbs] = useState<unknown[]>([]);
  const [selectedKb, setSelectedKb] = useState('');
  const [sources, setSources] = useState<unknown[]>([]);
  const [query, setQuery] = useState('');
  const [answerMode, setAnswerMode] = useState('normal');
  const [sourceUri, setSourceUri] = useState('');
  const [lastResponse, setLastResponse] = useState<unknown>();
  const [citations, setCitations] = useState<Citation[]>([]);
  const [error, setError] = useState('');

  const load = async () => {
    const [healthResponse, kbResponse] = await Promise.all([api.get('/v1/kbs/health'), api.get('/v1/kbs')]);
    setHealth(healthResponse);
    const items = asArray(kbResponse);
    setKbs(items);
    if (!selectedKb && items[0]) {
      setSelectedKb(String((items[0] as Record<string, unknown>).id));
    }
  };

  useEffect(() => {
    load().catch((err) => setError(err instanceof Error ? err.message : String(err)));
  }, []);

  useEffect(() => {
    if (!selectedKb) {
      return;
    }
    api.get(`/v1/kbs/${selectedKb}/sources`)
      .then((response) => setSources(asArray(response)))
      .catch(() => setSources([]));
  }, [selectedKb]);

  const createKB = async () => {
    const response = await api.post('/v1/kbs', { name: `KB ${new Date().toLocaleTimeString()}`, description: 'created from web UI' });
    setLastResponse(response);
    await load();
  };

  const addSource = async () => {
    const response = await api.post(`/v1/kbs/${selectedKb}/sources`, {
      name: sourceUri,
      type: sourceUri.startsWith('http') ? 'url' : 'local_folder',
      uri: sourceUri
    });
    setLastResponse(response);
  };

  const retrieve = async () => {
    const response = await api.post(`/v1/kbs/${selectedKb}/retrieve`, { query, top_k: 6, mode: 'hybrid' });
    setLastResponse(response);
    setCitations(asArray<Citation>((response as Record<string, unknown>).citations ?? response));
  };

  const answer = async () => {
    const response = await api.post(`/v1/kbs/${selectedKb}/answer`, { question: query, query, mode: answerMode, answer_mode: answerMode, top_k: 6 });
    setLastResponse(response);
    setCitations(asArray<Citation>((response as Record<string, unknown>).citations));
  };

  return (
    <div className="two-column">
      <section className="panel">
        <header className="panel-header">
          <h2>Knowledge Bases</h2>
          <button type="button" className="button primary" onClick={createKB}>Create KB</button>
        </header>
        {error ? <div className="error-banner">{error}</div> : null}
        <ToolOutput title="KB health" output={health} defaultOpen />
        <label className="field">
          <span>KB</span>
          <select value={selectedKb} onChange={(event) => setSelectedKb(event.target.value)}>
            <option value="">Select KB</option>
            {kbs.map((kb, index) => {
              const row = kb as Record<string, unknown>;
              return <option key={String(row.id ?? index)} value={String(row.id)}>{objectLabel(kb, 'kb')}</option>;
            })}
          </select>
        </label>
        <label className="field">
          <span>Source URI</span>
          <input value={sourceUri} onChange={(event) => setSourceUri(event.target.value)} placeholder="/docs or https://..." />
        </label>
        <button type="button" className="button" onClick={addSource} disabled={!selectedKb || !sourceUri}>Add source</button>
        <div className="data-grid">
          {sources.map((source, index) => (
            <article className="data-card" key={String((source as Record<string, unknown>).id ?? index)}>
              <strong>{objectLabel(source, 'source')}</strong>
              <ToolOutput title="Source detail" output={source} />
            </article>
          ))}
        </div>
      </section>
      <section className="panel">
        <h2>Retrieve and Answer</h2>
        <label className="field">
          <span>Question</span>
          <textarea value={query} onChange={(event) => setQuery(event.target.value)} rows={4} />
        </label>
        <label className="field inline">
          <span>Answer mode</span>
          <select value={answerMode} onChange={(event) => setAnswerMode(event.target.value)}>
            <option value="normal">normal</option>
            <option value="kb_only">kb_only</option>
            <option value="no_citation_no_answer">no_citation_no_answer</option>
          </select>
        </label>
        <div className="action-row">
          <button type="button" className="button" onClick={retrieve} disabled={!selectedKb || !query}>Retrieve</button>
          <button type="button" className="button primary" onClick={answer} disabled={!selectedKb || !query}>Answer</button>
        </div>
        <ToolOutput title="RAG response" output={lastResponse} defaultOpen />
        <CitationList citations={citations} />
      </section>
    </div>
  );
}

