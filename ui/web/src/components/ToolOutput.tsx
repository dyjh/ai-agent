import { useState } from 'react';

import { redactForDisplay } from '../utils/sanitize';

interface ToolOutputProps {
  title?: string;
  output?: unknown;
  defaultOpen?: boolean;
}

export function ToolOutput({ title = 'Tool output', output, defaultOpen = false }: ToolOutputProps) {
  const [open, setOpen] = useState(defaultOpen);
  const text = redactForDisplay(output ?? '');
  return (
    <section className="tool-output">
      <button className="row-button" type="button" onClick={() => setOpen((value) => !value)} aria-expanded={open}>
        <span>{title}</span>
        <span>{open ? 'Hide' : 'Show'}</span>
      </button>
      {open ? <pre>{text}</pre> : <div className="muted">{text ? `${text.slice(0, 160)}${text.length > 160 ? '...' : ''}` : 'No output'}</div>}
    </section>
  );
}

