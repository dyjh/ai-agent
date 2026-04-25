import { useEffect, useMemo, useRef, useState } from 'react';

import type { ApiClient } from '../api/client';
import { ApprovalCard } from '../components/ApprovalCard';
import { RunTimeline } from '../components/RunTimeline';
import { StatusBadge } from '../components/StatusBadge';
import { ToolOutput } from '../components/ToolOutput';
import type { ApprovalRecord, Conversation, Message } from '../types/domain';
import { formatDate, asArray } from '../utils/format';
import { connectConversationWS, type ChatSocketClient, type SocketStatus } from '../ws/chatSocket';
import { extractApproval, reduceAssistantEvent, type AgentEvent, type ChatTranscriptState } from '../ws/eventTypes';

interface ChatPageProps {
  api: ApiClient;
  wsBaseUrl: string;
}

export function ChatPage({ api, wsBaseUrl }: ChatPageProps) {
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [conversationId, setConversationId] = useState('');
  const [messages, setMessages] = useState<Message[]>([]);
  const [input, setInput] = useState('');
  const [events, setEvents] = useState<AgentEvent[]>([]);
  const [approval, setApproval] = useState<ApprovalRecord | undefined>();
  const [socketStatus, setSocketStatus] = useState<SocketStatus>('closed');
  const [error, setError] = useState('');
  const [transcript, setTranscript] = useState<ChatTranscriptState>({ assistantDraft: '', assistantMessages: [] });
  const socketRef = useRef<ChatSocketClient | undefined>(undefined);

  const loadConversations = async () => {
    const response = await api.listConversations();
    const items = asArray<Conversation>(response);
    setConversations(items);
    if (!conversationId && items[0]?.id) {
      setConversationId(items[0].id);
    }
  };

  useEffect(() => {
    loadConversations().catch((err) => setError(err instanceof Error ? err.message : String(err)));
  }, []);

  useEffect(() => {
    if (!conversationId) {
      return;
    }
    api.listMessages(conversationId)
      .then((response) => setMessages(asArray<Message>(response)))
      .catch((err) => setError(err instanceof Error ? err.message : String(err)));

    socketRef.current?.close();
    socketRef.current = connectConversationWS(conversationId, {
      baseUrl: wsBaseUrl,
      onStatus: setSocketStatus,
      onEvent: (event) => {
        setEvents((items) => [event, ...items].slice(0, 100));
        setTranscript((state) => reduceAssistantEvent(state, event));
        const nextApproval = extractApproval(event);
        if (nextApproval) {
          setApproval(nextApproval);
        }
      }
    });
    return () => socketRef.current?.close();
  }, [conversationId, wsBaseUrl]);

  const createConversation = async () => {
    const item = await api.createConversation('Web UI chat');
    setConversations((items) => [item, ...items]);
    setConversationId(item.id);
  };

  const send = async () => {
    const content = input.trim();
    if (!content || !conversationId) {
      return;
    }
    setInput('');
    setMessages((items) => [...items, { id: `local-${Date.now()}`, role: 'user', content }]);
    try {
      if (socketRef.current?.status() === 'open') {
        socketRef.current.sendUserMessage(content);
      } else {
        await api.postMessage(conversationId, content);
        const response = await api.listMessages(conversationId);
        setMessages(asArray<Message>(response));
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const visibleMessages = useMemo(() => {
    const assistantMessages = transcript.assistantMessages.map((content, index) => ({
      id: `assistant-stream-${index}`,
      role: 'assistant',
      content
    }));
    return [...messages, ...assistantMessages];
  }, [messages, transcript.assistantMessages]);

  return (
    <div className="chat-grid">
      <section className="panel conversation-list">
        <header className="panel-header">
          <h2>Conversations</h2>
          <button type="button" className="button primary" onClick={createConversation}>New</button>
        </header>
        <div className="list">
          {conversations.map((conversation) => (
            <button
              className={conversation.id === conversationId ? 'list-item active' : 'list-item'}
              key={conversation.id}
              type="button"
              onClick={() => setConversationId(conversation.id)}
            >
              <strong>{conversation.title || conversation.id}</strong>
              <span>{formatDate(conversation.updated_at || conversation.created_at)}</span>
            </button>
          ))}
        </div>
      </section>

      <section className="panel chat-panel">
        <header className="panel-header">
          <div>
            <h2>Chat</h2>
            <p>WebSocket <StatusBadge status={socketStatus} /></p>
          </div>
        </header>
        {error ? <div className="error-banner">{error}</div> : null}
        <div className="message-list">
          {visibleMessages.map((message) => (
            <article className={`message message-${message.role}`} key={message.id}>
              <span>{message.role}</span>
              <p>{message.content}</p>
            </article>
          ))}
          {transcript.assistantDraft ? (
            <article className="message message-assistant streaming">
              <span>assistant</span>
              <p>{transcript.assistantDraft}</p>
            </article>
          ) : null}
        </div>
        <footer className="composer">
          <textarea value={input} onChange={(event) => setInput(event.target.value)} rows={3} placeholder="Send a task or question" />
          <button type="button" className="button primary" onClick={send}>Send</button>
        </footer>
      </section>

      <aside className="panel side-panel">
        <h2>Current Run</h2>
        <RunTimeline events={events} />
        {approval ? <ApprovalCard approval={approval} api={api} onResolved={() => setApproval(undefined)} /> : null}
        <ToolOutput title="Recent events" output={events.slice(0, 8)} />
      </aside>
    </div>
  );
}
