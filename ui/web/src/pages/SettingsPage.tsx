import { useEffect, useState } from 'react';

import type { ApiClient } from '../api/client';
import { ToolOutput } from '../components/ToolOutput';
import type { HealthResponse } from '../types/domain';
import { asArray, objectLabel } from '../utils/format';

interface SettingsPageProps {
  api: ApiClient;
  apiBaseUrl: string;
  wsBaseUrl: string;
  health?: HealthResponse;
  onHealth: (health: HealthResponse) => void;
}

export function SettingsPage({ api, apiBaseUrl, wsBaseUrl, health, onHealth }: SettingsPageProps) {
  const [profiles, setProfiles] = useState<unknown[]>([]);
  const [networkPolicy, setNetworkPolicy] = useState<unknown>();
  const [audit, setAudit] = useState<unknown>();
  const [error, setError] = useState('');

  const load = async () => {
    const [healthResponse, profilesResponse, networkResponse, auditResponse] = await Promise.all([
      api.health(),
      api.get('/v1/security/policy-profiles'),
      api.get('/v1/security/network-policy'),
      api.get('/v1/security/audit')
    ]);
    onHealth(healthResponse);
    setProfiles(asArray(profilesResponse));
    setNetworkPolicy(networkResponse);
    setAudit(auditResponse);
  };

  useEffect(() => {
    load().catch((err) => setError(err instanceof Error ? err.message : String(err)));
  }, []);

  return (
    <div className="single-column">
      <section className="panel">
        <header className="panel-header">
          <h2>Settings and Health</h2>
          <button type="button" className="button" onClick={load}>Refresh</button>
        </header>
        {error ? <div className="error-banner">{error}</div> : null}
        <dl className="kv-grid">
          <dt>API base URL</dt>
          <dd>{apiBaseUrl}</dd>
          <dt>WS base URL</dt>
          <dd>{wsBaseUrl}</dd>
          <dt>Swagger UI</dt>
          <dd><a href={`${apiBaseUrl}/swagger/index.html`} target="_blank" rel="noreferrer">{apiBaseUrl}/swagger/index.html</a></dd>
          <dt>OpenAPI JSON</dt>
          <dd><a href={`${apiBaseUrl}/swagger/doc.json`} target="_blank" rel="noreferrer">{apiBaseUrl}/swagger/doc.json</a></dd>
        </dl>
        <ToolOutput title="Health" output={health} defaultOpen />
      </section>
      <section className="panel">
        <h2>Security</h2>
        <div className="data-grid">
          {profiles.map((profile, index) => (
            <article className="data-card" key={String((profile as Record<string, unknown>).name ?? index)}>
              <strong>{objectLabel(profile, 'profile')}</strong>
              <ToolOutput title="Profile" output={profile} />
            </article>
          ))}
        </div>
        <ToolOutput title="Network policy" output={networkPolicy} defaultOpen />
        <ToolOutput title="Audit summary" output={audit} />
      </section>
    </div>
  );
}

