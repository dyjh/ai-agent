import { describe, expect, it } from 'vitest';

import { conversationWsUrl } from '../src/ws/chatSocket';
import { extractApproval, reduceAssistantEvent } from '../src/ws/eventTypes';

describe('websocket helpers', () => {
  it('builds conversation websocket URLs', () => {
    expect(conversationWsUrl('conv 1', 'http://127.0.0.1:8765')).toBe('ws://127.0.0.1:8765/v1/conversations/conv%201/ws');
  });

  it('reduces assistant delta without duplicating final message text', () => {
    const first = reduceAssistantEvent({ assistantDraft: '', assistantMessages: [] }, { type: 'assistant.delta', content: 'hel' });
    const second = reduceAssistantEvent(first, { type: 'assistant.delta', content: 'lo' });
    const final = reduceAssistantEvent(second, { type: 'assistant.message' });
    expect(final.assistantDraft).toBe('');
    expect(final.assistantMessages).toEqual(['hello']);
  });

  it('extracts approval from event payload', () => {
    const approval = extractApproval({ type: 'approval.requested', payload: { approval: { id: 'appr_1', risk_level: 'high' } } });
    expect(approval?.id).toBe('appr_1');
    expect(approval?.risk_level).toBe('high');
  });
});

