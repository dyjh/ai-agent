import type { ReactNode } from 'react';

import { type RouteKey, routes } from '../routes';
import type { HealthResponse } from '../types/domain';
import { Sidebar } from './Sidebar';
import { Topbar } from './Topbar';

interface LayoutProps {
  active: RouteKey;
  health?: HealthResponse;
  onRouteChange: (route: RouteKey) => void;
  onRefresh: () => void;
  children: ReactNode;
}

export function Layout({ active, health, onRouteChange, onRefresh, children }: LayoutProps) {
  const route = routes.find((item) => item.key === active);
  return (
    <div className="app-shell">
      <Sidebar active={active} onChange={onRouteChange} />
      <main>
        <Topbar title={route?.label || 'Local Agent'} health={health} onRefresh={onRefresh} />
        {children}
      </main>
    </div>
  );
}

