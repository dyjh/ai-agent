import type { ApprovalRecord } from '../types/domain';

export function approvalRisk(approval: ApprovalRecord): string {
  return approval.risk_level || approval.decision?.risk_level || approval.explanation?.risk_level || 'unknown';
}

export function approvalTool(approval: ApprovalRecord): string {
  return approval.tool || approval.proposal?.tool || 'unknown.tool';
}

export function approvalSummary(approval: ApprovalRecord): string {
  return approval.explanation?.summary || approval.summary || approval.proposal?.purpose || approvalTool(approval);
}

export function isApprovalPending(approval: ApprovalRecord): boolean {
  return !approval.status || approval.status === 'pending' || approval.status === 'requested';
}

