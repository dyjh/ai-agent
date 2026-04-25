import type { AgentEvent } from '../ws/eventTypes';
import type { RunState, RunStep } from '../types/domain';
import { formatDate } from '../utils/format';
import { ToolOutput } from './ToolOutput';
import { StatusBadge } from './StatusBadge';

interface RunTimelineProps {
  run?: RunState;
  steps?: RunStep[];
  events?: AgentEvent[];
  onSelectApproval?: (approvalId: string) => void;
}

export function RunTimeline({ run, steps = [], events = [], onSelectApproval }: RunTimelineProps) {
  if (!run && steps.length === 0 && events.length === 0) {
    return <div className="empty-state">No run selected.</div>;
  }
  return (
    <section className="run-timeline">
      {run ? (
        <header className="timeline-header">
          <div>
            <div className="eyebrow">Run</div>
            <h3>{run.run_id}</h3>
          </div>
          <StatusBadge status={run.status} />
        </header>
      ) : null}
      {run ? (
        <dl className="kv-grid">
          <dt>Started</dt>
          <dd>{formatDate(run.created_at)}</dd>
          <dt>Updated</dt>
          <dd>{formatDate(run.updated_at)}</dd>
          <dt>Approval</dt>
          <dd>{run.approval_id || '-'}</dd>
        </dl>
      ) : null}
      <div className="timeline-list">
        {steps.map((step, index) => (
          <article className="timeline-item" key={step.step_id || step.id || index}>
            <header>
              <span>{step.step_index ?? index}</span>
              <strong>{step.summary || step.proposal?.tool || step.status || 'step'}</strong>
              <StatusBadge status={step.status} risk={step.policy?.risk_level} />
            </header>
            {step.proposal ? <p className="muted">{step.proposal.purpose || step.proposal.tool}</p> : null}
            {step.approval ? (
              <button type="button" className="link-button" onClick={() => onSelectApproval?.(step.approval?.id || '')}>
                Approval {step.approval.id}
              </button>
            ) : null}
            {step.tool_result ? <ToolOutput title="Tool result" output={step.tool_result.output || step.tool_result.error} /> : null}
            {step.error ? <div className="error-banner">{step.error}</div> : null}
          </article>
        ))}
        {events.map((event, index) => (
          <article className="timeline-item compact" key={`${event.type}-${index}`}>
            <header>
              <span>{index + 1}</span>
              <strong>{event.type}</strong>
              {event.run_id ? <small>{event.run_id}</small> : null}
            </header>
            {event.content ? <p>{event.content}</p> : null}
            {event.payload ? <ToolOutput title="Event payload" output={event.payload} /> : null}
          </article>
        ))}
      </div>
    </section>
  );
}

