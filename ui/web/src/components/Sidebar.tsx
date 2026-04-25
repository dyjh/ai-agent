import { routes, type RouteKey } from '../routes';

interface SidebarProps {
  active: RouteKey;
  onChange: (route: RouteKey) => void;
}

export function Sidebar({ active, onChange }: SidebarProps) {
  return (
    <aside className="sidebar">
      <div className="brand">
        <strong>Local Agent</strong>
        <span>single-user console</span>
      </div>
      <nav>
        {routes.map((route) => (
          <button
            className={route.key === active ? 'nav-item active' : 'nav-item'}
            key={route.key}
            type="button"
            onClick={() => onChange(route.key)}
            title={route.description}
          >
            <span>{route.label}</span>
          </button>
        ))}
      </nav>
    </aside>
  );
}

