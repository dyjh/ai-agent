export type RouteKey =
  | 'chat'
  | 'runs'
  | 'approvals'
  | 'code'
  | 'memory'
  | 'knowledge'
  | 'skills'
  | 'mcp'
  | 'ops'
  | 'evals'
  | 'settings';

export interface RouteItem {
  key: RouteKey;
  label: string;
  description: string;
}

export const routes: RouteItem[] = [
  { key: 'chat', label: 'Chat', description: 'Streaming conversation and current run' },
  { key: 'runs', label: 'Runs', description: 'Workflow timeline and recovery' },
  { key: 'approvals', label: 'Approvals', description: 'Pending high-risk actions' },
  { key: 'code', label: 'Code', description: 'Code workflow launcher and diff preview' },
  { key: 'memory', label: 'Memory', description: 'Governed markdown memory' },
  { key: 'knowledge', label: 'Knowledge', description: 'KB sources, retrieval, answers' },
  { key: 'skills', label: 'Skills', description: 'Skill registry and sandbox metadata' },
  { key: 'mcp', label: 'MCP', description: 'MCP servers, tools, policies' },
  { key: 'ops', label: 'Ops', description: 'Host profiles and runbooks' },
  { key: 'evals', label: 'Eval', description: 'Safe-mode eval and replay reports' },
  { key: 'settings', label: 'Settings', description: 'Health, security, docs' }
];

