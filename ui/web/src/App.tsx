import { useMemo, useState } from 'react';

import { createApiClient } from './api/client';
import { Layout } from './components/Layout';
import { ApprovalsPage } from './pages/ApprovalsPage';
import { ChatPage } from './pages/ChatPage';
import { CodePage } from './pages/CodePage';
import { EvalPage } from './pages/EvalPage';
import { KnowledgePage } from './pages/KnowledgePage';
import { MCPPage } from './pages/MCPPage';
import { MemoryPage } from './pages/MemoryPage';
import { OpsPage } from './pages/OpsPage';
import { RunsPage } from './pages/RunsPage';
import { SettingsPage } from './pages/SettingsPage';
import { SkillsPage } from './pages/SkillsPage';
import type { RouteKey } from './routes';
import { getAppSettings } from './stores/appStore';
import type { HealthResponse } from './types/domain';

function App() {
  const [route, setRoute] = useState<RouteKey>('chat');
  const [health, setHealth] = useState<HealthResponse>();
  const settings = getAppSettings();
  const api = useMemo(() => createApiClient({ baseUrl: settings.apiBaseUrl }), [settings.apiBaseUrl]);

  const refreshHealth = async () => {
    const response = await api.health();
    setHealth(response);
  };

  const page = (() => {
    switch (route) {
      case 'chat':
        return <ChatPage api={api} wsBaseUrl={settings.wsBaseUrl} />;
      case 'runs':
        return <RunsPage api={api} />;
      case 'approvals':
        return <ApprovalsPage api={api} />;
      case 'code':
        return <CodePage api={api} />;
      case 'memory':
        return <MemoryPage api={api} />;
      case 'knowledge':
        return <KnowledgePage api={api} />;
      case 'skills':
        return <SkillsPage api={api} />;
      case 'mcp':
        return <MCPPage api={api} />;
      case 'ops':
        return <OpsPage api={api} />;
      case 'evals':
        return <EvalPage api={api} />;
      case 'settings':
        return <SettingsPage api={api} apiBaseUrl={settings.apiBaseUrl} wsBaseUrl={settings.wsBaseUrl} health={health} onHealth={setHealth} />;
      default:
        return <ChatPage api={api} wsBaseUrl={settings.wsBaseUrl} />;
    }
  })();

  return (
    <Layout active={route} health={health} onRouteChange={setRoute} onRefresh={refreshHealth}>
      {page}
    </Layout>
  );
}

export default App;

