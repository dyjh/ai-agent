import type { HealthResponse } from '../types/domain';
import { StatusBadge } from './StatusBadge';

interface TopbarProps {
  title: string;
  health?: HealthResponse;
  onRefresh: () => void;
}

export function Topbar({ title, health, onRefresh }: TopbarProps) {
  return (
    <header className="topbar">
      <div>
        <div className="eyebrow">Local deployment</div>
        <h1>{title}</h1>
      </div>
      <div className="topbar-actions">
        <StatusBadge status={health?.status || 'unknown'} />
        <a className="button subtle" href="/swagger/index.html" target="_blank" rel="noreferrer">Swagger</a>
        <button className="button" type="button" onClick={onRefresh}>Refresh</button>
      </div>
    </header>
  );
}

