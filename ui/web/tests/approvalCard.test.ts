import { describe, expect, it } from 'vitest';

import { approvalRisk, approvalSummary, approvalTool, isApprovalPending } from '../src/stores/approvalStore';

describe('approval display helpers', () => {
  it('uses immutable approval snapshot metadata for display', () => {
    const approval = {
      id: 'appr_1',
      status: 'pending',
      proposal: { tool: 'code.apply_patch', purpose: 'Apply approved diff' },
      decision: { risk_level: 'high' },
      input_snapshot: { patch: 'diff --git a/a b/a' }
    };
    expect(approvalTool(approval)).toBe('code.apply_patch');
    expect(approvalSummary(approval)).toBe('Apply approved diff');
    expect(approvalRisk(approval)).toBe('high');
    expect(isApprovalPending(approval)).toBe(true);
  });
});

