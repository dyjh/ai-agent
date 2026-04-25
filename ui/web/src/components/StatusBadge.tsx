import { approvalRisk } from '../stores/approvalStore';
import type { ApprovalRecord } from '../types/domain';

interface StatusBadgeProps {
  status?: string;
  risk?: string;
  approval?: ApprovalRecord;
}

export function StatusBadge({ status, risk, approval }: StatusBadgeProps) {
  const value = (risk || (approval ? approvalRisk(approval) : status) || 'unknown').toLowerCase();
  const className = `status-badge status-${value.replaceAll('_', '-')}`;
  return <span className={className}>{value}</span>;
}

