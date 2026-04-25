import { useEffect, useState } from 'react';

import type { ApiClient } from '../api/client';
import { ToolOutput } from '../components/ToolOutput';
import { asArray, objectLabel } from '../utils/format';

interface MCPPageProps {
  api: ApiClient;
}

export function MCPPage({ api }: MCPPageProps) {
  const [servers, setServers] = useState<unknown[]>([]);
  const [policies, setPolicies] = useState<unknown[]>([]);
  const [selected, setSelected] = useState<Record<string, unknown> | undefined>();
  const [toolName, setToolName] = useState('');
  const [argumentsJson, setArgumentsJson] = useState('{}');
  const [lastResponse, setLastResponse] = useState<unknown>();
  const [error, setError] = useState('');

  const load = async () => {
    const [serverResponse, policyResponse] = await Promise.all([api.get('/v1/mcp/servers'), api.get('/v1/mcp/tools')]);
    setServers(asArray(serverResponse));
    setPolicies(asArray(policyResponse));
  };

  useEffect(() => {
    load().catch((err) => setError(err instanceof Error ? err.message : String(err)));
  }, []);

  const serverId = String(selected?.id ?? '');
  const serverAction = async (suffix: string) => {
    const response = await api.post(`/v1/mcp/servers/${serverId}/${suffix}`, {});
    setLastResponse(response);
  };

  const callTool = async () => {
    const args = JSON.parse(argumentsJson || '{}') as Record<string, unknown>;
    const response = await api.post(`/v1/mcp/servers/${serverId}/tools/${encodeURIComponent(toolName)}/call`, {
      arguments: args,
      purpose: 'Call MCP tool from web UI'
    });
    setLastResponse(response);
  };

  return (
    <div className="two-column">
      <section className="panel">
        <header className="panel-header">
          <h2>MCP Servers</h2>
          <button type="button" className="button" onClick={load}>Refresh</button>
        </header>
        {error ? <div className="error-banner">{error}</div> : null}
        <div className="list">
          {servers.map((server, index) => {
            const row = server as Record<string, unknown>;
            return (
              <button className={selected?.id === row.id ? 'list-item active' : 'list-item'} key={String(row.id ?? index)} type="button" onClick={() => setSelected(row)}>
                <strong>{objectLabel(server, 'server')}</strong>
                <span>{String(row.transport ?? row.type ?? '')}</span>
              </button>
            );
          })}
        </div>
        <ToolOutput title="Tool policies" output={policies} />
      </section>
      <section className="panel">
        <header className="panel-header">
          <h2>Server Detail</h2>
          <div className="action-row compact-actions">
            <button type="button" className="button" onClick={() => serverAction('test')} disabled={!serverId}>Test</button>
            <button type="button" className="button" onClick={() => serverAction('refresh')} disabled={!serverId}>Refresh tools</button>
          </div>
        </header>
        <ToolOutput title="Selected server" output={selected} defaultOpen />
        <div className="form-grid">
          <label className="field">
            <span>Tool name</span>
            <input value={toolName} onChange={(event) => setToolName(event.target.value)} />
          </label>
          <label className="field">
            <span>Arguments JSON</span>
            <textarea value={argumentsJson} onChange={(event) => setArgumentsJson(event.target.value)} rows={5} />
          </label>
        </div>
        <button type="button" className="button primary" onClick={callTool} disabled={!serverId || !toolName}>Call through approval chain</button>
        <ToolOutput title="Last response" output={lastResponse} defaultOpen />
      </section>
    </div>
  );
}

