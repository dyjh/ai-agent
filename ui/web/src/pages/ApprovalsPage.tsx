import { useEffect, useMemo, useState } from 'react';

import type { ApiClient } from '../api/client';
import { ApprovalCard } from '../components/ApprovalCard';
import { StatusBadge } from '../components/StatusBadge';
import { approvalRisk, approvalSummary, approvalTool } from '../stores/approvalStore';
import type { ApprovalRecord } from '../types/domain';
import { asArray, formatDate } from '../utils/format';

interface ApprovalsPageProps {
  api: ApiClient;
}

export function ApprovalsPage({ api }: ApprovalsPageProps) {
  const [approvals, setApprovals] = useState<ApprovalRecord[]>([]);
  const [selected, setSelected] = useState<ApprovalRecord | undefined>();
  const [risk, setRisk] = useState('');
  const [error, setError] = useState('');

  const load = async () => {
    const response = await api.pendingApprovals();
    const items = asArray<ApprovalRecord>(response);
    setApprovals(items);
    setSelected((current) => current ?? items[0]);
  };

  useEffect(() => {
    load().catch((err) => setError(err instanceof Error ? err.message : String(err)));
  }, []);

  const filtered = useMemo(() => {
    return risk ? approvals.filter((approval) => approvalRisk(approval) === risk) : approvals;
  }, [approvals, risk]);

  return (
    <div className="two-column">
      <section className="panel">
        <header className="panel-header">
          <h2>Pending Approvals</h2>
          <button type="button" className="button" onClick={load}>Refresh</button>
        </header>
        <label className="field inline">
          <span>Risk filter</span>
          <select value={risk} onChange={(event) => setRisk(event.target.value)}>
            <option value="">All</option>
            <option value="low">low</option>
            <option value="medium">medium</option>
            <option value="high">high</option>
            <option value="danger">danger</option>
            <option value="unknown">unknown</option>
          </select>
        </label>
        {error ? <div className="error-banner">{error}</div> : null}
        <div className="list">
          {filtered.map((approval) => (
            <button className={selected?.id === approval.id ? 'list-item active' : 'list-item'} key={approval.id} type="button" onClick={() => setSelected(approval)}>
              <strong>{approvalSummary(approval)}</strong>
              <span>{approvalTool(approval)}</span>
              <span>{formatDate(approval.created_at)}</span>
              <StatusBadge approval={approval} />
            </button>
          ))}
          {filtered.length === 0 ? <div className="empty-state">No pending approvals.</div> : null}
        </div>
      </section>
      <section className="panel">
        <ApprovalCard approval={selected} api={api} onResolved={load} />
      </section>
    </div>
  );
}

