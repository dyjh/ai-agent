import { useState } from 'react';

import type { ApiClient } from '../api/client';
import { approvalRisk, approvalSummary, approvalTool, isApprovalPending } from '../stores/approvalStore';
import type { ApprovalRecord } from '../types/domain';
import { compactJson } from '../utils/format';
import { redactForDisplay } from '../utils/sanitize';
import { StatusBadge } from './StatusBadge';

interface ApprovalCardProps {
  approval?: ApprovalRecord;
  api: ApiClient;
  onResolved?: () => void;
}

export function ApprovalCard({ approval, api, onResolved }: ApprovalCardProps) {
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  if (!approval) {
    return <div className="empty-state">No approval selected.</div>;
  }

  const resolve = async (approved: boolean) => {
    setBusy(true);
    setError('');
    try {
      if (approved) {
        await api.approve(approval.id);
      } else {
        await api.reject(approval.id, reason || 'rejected from web UI');
      }
      onResolved?.();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const pending = isApprovalPending(approval);
  const explanation = approval.explanation;
  return (
    <article className="approval-card">
      <header>
        <div>
          <div className="eyebrow">{approvalTool(approval)}</div>
          <h3>{approvalSummary(approval)}</h3>
        </div>
        <StatusBadge risk={approvalRisk(approval)} />
      </header>
      <dl className="kv-grid">
        <dt>Approval ID</dt>
        <dd>{approval.id}</dd>
        <dt>Snapshot hash</dt>
        <dd>{approval.snapshot_hash || '-'}</dd>
        <dt>Status</dt>
        <dd>{approval.status || 'pending'}</dd>
        <dt>Why</dt>
        <dd>{explanation?.why_needed || approval.reason || 'Policy requires explicit approval.'}</dd>
      </dl>
      {explanation?.expected_effects?.length ? (
        <div className="pill-row">
          {explanation.expected_effects.map((effect) => <span className="pill" key={effect}>{effect}</span>)}
        </div>
      ) : null}
      {explanation?.rollback_plan ? (
        <section className="notice">
          <strong>Rollback plan</strong>
          <p>{explanation.rollback_plan}</p>
        </section>
      ) : null}
      <label className="field">
        <span>Approved input snapshot</span>
        <textarea readOnly value={redactForDisplay(compactJson(approval.input_snapshot ?? approval.proposal?.input ?? {}))} rows={10} />
      </label>
      {error ? <div className="error-banner">{error}</div> : null}
      {pending ? (
        <footer className="action-row">
          <button type="button" className="button danger" disabled={busy} onClick={() => resolve(false)}>Reject</button>
          <input value={reason} onChange={(event) => setReason(event.target.value)} placeholder="Reject reason" />
          <button type="button" className="button primary" disabled={busy} onClick={() => resolve(true)}>Approve snapshot</button>
        </footer>
      ) : null}
    </article>
  );
}

