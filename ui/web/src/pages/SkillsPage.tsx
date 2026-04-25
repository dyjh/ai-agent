import { useEffect, useState } from 'react';

import type { ApiClient } from '../api/client';
import { ToolOutput } from '../components/ToolOutput';
import { asArray, objectLabel } from '../utils/format';

interface SkillsPageProps {
  api: ApiClient;
}

export function SkillsPage({ api }: SkillsPageProps) {
  const [skills, setSkills] = useState<unknown[]>([]);
  const [selected, setSelected] = useState<Record<string, unknown> | undefined>();
  const [path, setPath] = useState('');
  const [input, setInput] = useState('{}');
  const [lastResponse, setLastResponse] = useState<unknown>();
  const [error, setError] = useState('');

  const load = async () => {
    const response = await api.get('/v1/skills');
    setSkills(asArray(response));
  };

  useEffect(() => {
    load().catch((err) => setError(err instanceof Error ? err.message : String(err)));
  }, []);

  const selectSkill = async (id: string) => {
    const response = await api.get(`/v1/skills/${id}`);
    setSelected(response as Record<string, unknown>);
  };

  const upload = async () => {
    const response = await api.post('/v1/skills/upload', { path, name: path.split('/').pop() || path });
    setLastResponse(response);
    await load();
  };

  const skillId = String((selected?.skill as Record<string, unknown> | undefined)?.id ?? selected?.id ?? '');
  const postSkill = async (suffix: string, body: unknown = {}) => {
    if (!skillId) {
      return;
    }
    const response = await api.post(`/v1/skills/${skillId}/${suffix}`, body);
    setLastResponse(response);
    if (suffix === 'enable' || suffix === 'disable') {
      await load();
    }
  };

  const parsedInput = () => {
    try {
      return JSON.parse(input) as Record<string, unknown>;
    } catch {
      return { raw: input };
    }
  };

  return (
    <div className="two-column">
      <section className="panel">
        <header className="panel-header">
          <h2>Skills</h2>
          <button type="button" className="button" onClick={load}>Refresh</button>
        </header>
        {error ? <div className="error-banner">{error}</div> : null}
        <label className="field">
          <span>Local manifest or skill directory path</span>
          <input value={path} onChange={(event) => setPath(event.target.value)} />
        </label>
        <button type="button" className="button" onClick={upload} disabled={!path}>Upload path</button>
        <div className="list">
          {skills.map((skill, index) => {
            const row = skill as Record<string, unknown>;
            return (
              <button className="list-item" key={String(row.id ?? index)} type="button" onClick={() => selectSkill(String(row.id))}>
                <strong>{objectLabel(skill, 'skill')}</strong>
                <span>{String(row.enabled ?? '')}</span>
              </button>
            );
          })}
        </div>
      </section>
      <section className="panel">
        <header className="panel-header">
          <h2>Skill Detail</h2>
          <div className="action-row compact-actions">
            <button type="button" className="button" onClick={() => postSkill('enable')} disabled={!skillId}>Enable</button>
            <button type="button" className="button" onClick={() => postSkill('disable')} disabled={!skillId}>Disable</button>
            <button type="button" className="button" onClick={() => postSkill('validate', { input: parsedInput() })} disabled={!skillId}>Validate</button>
            <button type="button" className="button primary" onClick={() => postSkill('run', { input: parsedInput() })} disabled={!skillId}>Run via ToolRouter</button>
          </div>
        </header>
        <label className="field">
          <span>Run input JSON</span>
          <textarea value={input} onChange={(event) => setInput(event.target.value)} rows={5} />
        </label>
        <ToolOutput title="Selected skill" output={selected} defaultOpen />
        <ToolOutput title="Last response" output={lastResponse} defaultOpen />
      </section>
    </div>
  );
}

