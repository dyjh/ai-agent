import type { AgentEvent } from './eventTypes';

export type SocketStatus = 'connecting' | 'open' | 'reconnecting' | 'closed' | 'error';

export interface ChatSocketOptions {
  baseUrl?: string;
  onEvent: (event: AgentEvent) => void;
  onStatus?: (status: SocketStatus) => void;
  reconnect?: boolean;
}

export interface ChatSocketClient {
  sendUserMessage: (content: string) => void;
  respondApproval: (approvalId: string, approved: boolean) => void;
  close: () => void;
  status: () => SocketStatus;
}

const defaultWSBaseUrl = import.meta.env.VITE_AGENT_WS_BASE_URL || 'ws://127.0.0.1:8765';

function normalizeBaseUrl(baseUrl: string): string {
  const converted = baseUrl.startsWith('http://')
    ? baseUrl.replace('http://', 'ws://')
    : baseUrl.startsWith('https://')
      ? baseUrl.replace('https://', 'wss://')
      : baseUrl;
  return converted.endsWith('/') ? converted.slice(0, -1) : converted;
}

export function conversationWsUrl(conversationId: string, baseUrl = defaultWSBaseUrl): string {
  return `${normalizeBaseUrl(baseUrl)}/v1/conversations/${encodeURIComponent(conversationId)}/ws`;
}

export function connectConversationWS(conversationId: string, options: ChatSocketOptions): ChatSocketClient {
  let socket: WebSocket | undefined;
  let closedByClient = false;
  let reconnectTimer: number | undefined;
  let currentStatus: SocketStatus = 'connecting';
  const reconnect = options.reconnect ?? true;

  const setStatus = (status: SocketStatus) => {
    currentStatus = status;
    options.onStatus?.(status);
  };

  const connect = () => {
    setStatus(socket ? 'reconnecting' : 'connecting');
    socket = new WebSocket(conversationWsUrl(conversationId, options.baseUrl));
    socket.onopen = () => setStatus('open');
    socket.onerror = () => setStatus('error');
    socket.onmessage = (message) => {
      try {
        options.onEvent(JSON.parse(message.data) as AgentEvent);
      } catch {
        options.onEvent({ type: 'error', content: 'received malformed websocket event' });
      }
    };
    socket.onclose = () => {
      if (closedByClient || !reconnect) {
        setStatus('closed');
        return;
      }
      setStatus('reconnecting');
      reconnectTimer = window.setTimeout(connect, 1200);
    };
  };

  connect();

  const send = (payload: Record<string, unknown>) => {
    if (!socket || socket.readyState !== WebSocket.OPEN) {
      throw new Error('websocket is not connected');
    }
    socket.send(JSON.stringify(payload));
  };

  return {
    sendUserMessage: (content: string) => send({ type: 'user.message', content }),
    respondApproval: (approvalId: string, approved: boolean) => send({ type: 'approval.respond', approval_id: approvalId, approved }),
    close: () => {
      closedByClient = true;
      if (reconnectTimer) {
        window.clearTimeout(reconnectTimer);
      }
      socket?.close();
      setStatus('closed');
    },
    status: () => currentStatus
  };
}

