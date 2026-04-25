import { useEffect, useState } from 'react';

import type { ApiClient } from '../api/client';
import { ApprovalCard } from '../components/ApprovalCard';
import { RunTimeline } from '../components/RunTimeline';
import { StatusBadge } from '../components/StatusBadge';
import type { ApprovalRecord, RunState, RunStep } from '../types/domain';
import { asArray, formatDate } from '../utils/format';

interface RunsPageProps {
  api: ApiClient;
}

export function RunsPage({ api }: RunsPageProps) {
  const [runs, setRuns] = useState<RunState[]>([]);
  const [selected, setSelected] = useState<RunState | undefined>();
  const [steps, setSteps] = useState<RunStep[]>([]);
  const [approval, setApproval] = useState<ApprovalRecord | undefined>();
  const [error, setError] = useState('');

  const load = async () => {
    const response = await api.listRuns('?limit=50');
    const items = asArray<RunState>(response);
    setRuns(items);
    if (!selected && items[0]) {
      setSelected(items[0]);
    }
  };

  useEffect(() => {
    load().catch((err) => setError(err instanceof Error ? err.message : String(err)));
  }, []);

  useEffect(() => {
    if (!selected?.run_id) {
      return;
    }
    api.listRunSteps(selected.run_id)
      .then((response) => {
        const items = asArray<RunStep>(response);
        setSteps(items);
        setApproval(items.find((step) => step.approval)?.approval);
      })
      .catch((err) => setError(err instanceof Error ? err.message : String(err)));
  }, [selected?.run_id]);

  const cancel = async () => {
    if (!selected) {
      return;
    }
    await api.cancelRun(selected.run_id);
    await load();
  };

  const resume = async () => {
    if (!selected?.approval_id) {
      return;
    }
    await api.resumeRun(selected.run_id, selected.approval_id, true);
    await load();
  };

  return (
    <div className="two-column">
      <section className="panel">
        <header className="panel-header">
          <h2>Runs</h2>
          <button type="button" className="button" onClick={load}>Refresh</button>
        </header>
        {error ? <div className="error-banner">{error}</div> : null}
        <div className="list">
          {runs.map((run) => (
            <button className={selected?.run_id === run.run_id ? 'list-item active' : 'list-item'} key={run.run_id} type="button" onClick={() => setSelected(run)}>
              <strong>{run.run_id}</strong>
              <span>{formatDate(run.updated_at || run.created_at)}</span>
              <StatusBadge status={run.status} />
            </button>
          ))}
        </div>
      </section>
      <section className="panel">
        <header className="panel-header">
          <h2>Timeline</h2>
          <div className="action-row compact-actions">
            <button type="button" className="button" onClick={resume} disabled={!selected?.approval_id}>Resume</button>
            <button type="button" className="button danger" onClick={cancel} disabled={!selected}>Cancel</button>
          </div>
        </header>
        <RunTimeline run={selected} steps={steps} onSelectApproval={(approvalId) => setApproval(steps.find((step) => step.approval?.id === approvalId)?.approval)} />
        {approval ? <ApprovalCard approval={approval} api={api} onResolved={load} /> : null}
      </section>
    </div>
  );
}

