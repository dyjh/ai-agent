import { useEffect, useState } from 'react';

import type { ApiClient } from '../api/client';
import { ToolOutput } from '../components/ToolOutput';
import { asArray, objectLabel } from '../utils/format';

interface OpsPageProps {
  api: ApiClient;
}

export function OpsPage({ api }: OpsPageProps) {
  const [hosts, setHosts] = useState<unknown[]>([]);
  const [runbooks, setRunbooks] = useState<unknown[]>([]);
  const [selectedHost, setSelectedHost] = useState('');
  const [selectedRunbook, setSelectedRunbook] = useState('');
  const [lastResponse, setLastResponse] = useState<unknown>();
  const [error, setError] = useState('');

  const load = async () => {
    const [hostResponse, runbookResponse] = await Promise.all([api.get('/v1/ops/hosts'), api.get('/v1/ops/runbooks')]);
    const hostItems = asArray(hostResponse);
    const runbookItems = asArray(runbookResponse);
    setHosts(hostItems);
    setRunbooks(runbookItems);
    if (!selectedHost && hostItems[0]) {
      setSelectedHost(String((hostItems[0] as Record<string, unknown>).id));
    }
    if (!selectedRunbook && runbookItems[0]) {
      setSelectedRunbook(String((runbookItems[0] as Record<string, unknown>).id));
    }
  };

  useEffect(() => {
    load().catch((err) => setError(err instanceof Error ? err.message : String(err)));
  }, []);

  const startWorkflow = async (content: string) => {
    const conversation = await api.createConversation('Ops workflow');
    const response = await api.postMessage(conversation.id, content);
    setLastResponse(response);
  };

  const runbookAction = async (execute: boolean) => {
    const response = await api.post(`/v1/ops/runbooks/${selectedRunbook}/${execute ? 'execute' : 'plan'}`, {
      host_id: selectedHost,
      dry_run: !execute,
      max_steps: 5
    });
    setLastResponse(response);
  };

  return (
    <div className="two-column">
      <section className="panel">
        <header className="panel-header">
          <h2>Ops</h2>
          <button type="button" className="button" onClick={load}>Refresh</button>
        </header>
        {error ? <div className="error-banner">{error}</div> : null}
        <label className="field">
          <span>Host profile</span>
          <select value={selectedHost} onChange={(event) => setSelectedHost(event.target.value)}>
            {hosts.map((host, index) => {
              const row = host as Record<string, unknown>;
              return <option key={String(row.id ?? index)} value={String(row.id)}>{objectLabel(host, 'host')}</option>;
            })}
          </select>
        </label>
        <div className="button-grid">
          <button type="button" className="button" onClick={() => startWorkflow(`请查看 host ${selectedHost || 'local'} 的系统信息`)}>System info</button>
          <button type="button" className="button" onClick={() => startWorkflow(`请查看 host ${selectedHost || 'local'} 的进程和 CPU 占用`)}>Processes</button>
          <button type="button" className="button" onClick={() => startWorkflow(`请查看 host ${selectedHost || 'local'} 的磁盘使用情况`)}>Disk</button>
          <button type="button" className="button" onClick={() => startWorkflow(`请查看 host ${selectedHost || 'local'} 的内存使用情况`)}>Memory</button>
          <button type="button" className="button" onClick={() => startWorkflow('请查看 docker 容器状态')}>Docker ps</button>
          <button type="button" className="button" onClick={() => startWorkflow('请查看 k8s get pods')}>K8s pods</button>
        </div>
      </section>
      <section className="panel">
        <h2>Runbooks</h2>
        <label className="field">
          <span>Runbook</span>
          <select value={selectedRunbook} onChange={(event) => setSelectedRunbook(event.target.value)}>
            {runbooks.map((runbook, index) => {
              const row = runbook as Record<string, unknown>;
              return <option key={String(row.id ?? index)} value={String(row.id)}>{objectLabel(runbook, 'runbook')}</option>;
            })}
          </select>
        </label>
        <div className="action-row">
          <button type="button" className="button" disabled={!selectedRunbook} onClick={() => runbookAction(false)}>Plan</button>
          <button type="button" className="button primary" disabled={!selectedRunbook} onClick={() => runbookAction(true)}>Execute via approval</button>
        </div>
        <ToolOutput title="Hosts" output={hosts} />
        <ToolOutput title="Runbooks" output={runbooks} />
        <ToolOutput title="Last response" output={lastResponse} defaultOpen />
      </section>
    </div>
  );
}

