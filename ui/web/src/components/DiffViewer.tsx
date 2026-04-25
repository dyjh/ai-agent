import { redactForDisplay } from '../utils/sanitize';

export interface DiffLine {
  type: 'add' | 'del' | 'ctx' | 'meta' | 'warn';
  text: string;
}

export interface DiffFile {
  path: string;
  lines: DiffLine[];
  hasConflict: boolean;
  sensitive: boolean;
}

const sensitivePattern = /(^|\/)(\.env|id_rsa|id_ed25519|credentials|cookies|session|.*private.*key.*)$/i;

export function parseUnifiedDiff(diff: string): DiffFile[] {
  const files: DiffFile[] = [];
  let current: DiffFile | undefined;
  for (const rawLine of diff.split('\n')) {
    const line = redactForDisplay(rawLine);
    if (line.startsWith('diff --git ')) {
      const parts = line.split(' ');
      const path = (parts[3] || parts[2] || 'unknown').replace(/^b\//, '').replace(/^a\//, '');
      current = {
        path,
        lines: [{ type: 'meta', text: line }],
        hasConflict: false,
        sensitive: sensitivePattern.test(path)
      };
      files.push(current);
      continue;
    }
    if (!current) {
      current = { path: 'patch', lines: [], hasConflict: false, sensitive: false };
      files.push(current);
    }
    if (line.includes('<<<<<<<') || line.includes('=======') || line.includes('>>>>>>>')) {
      current.hasConflict = true;
      current.lines.push({ type: 'warn', text: line });
    } else if (line.startsWith('+') && !line.startsWith('+++')) {
      current.lines.push({ type: 'add', text: line });
    } else if (line.startsWith('-') && !line.startsWith('---')) {
      current.lines.push({ type: 'del', text: line });
    } else if (line.startsWith('@@') || line.startsWith('index ') || line.startsWith('---') || line.startsWith('+++')) {
      current.lines.push({ type: 'meta', text: line });
    } else {
      current.lines.push({ type: 'ctx', text: line });
    }
  }
  return files;
}

interface DiffViewerProps {
  diff?: string;
  files?: DiffFile[];
}

export function DiffViewer({ diff = '', files }: DiffViewerProps) {
  const parsed = files ?? parseUnifiedDiff(diff);
  if (parsed.length === 0 || parsed.every((file) => file.lines.length === 0)) {
    return <div className="empty-state">No diff loaded.</div>;
  }
  return (
    <div className="diff-viewer">
      <div className="diff-file-list">
        {parsed.map((file) => (
          <span className="diff-file-chip" key={file.path}>
            {file.path}
            {file.hasConflict ? ' conflict' : ''}
            {file.sensitive ? ' sensitive' : ''}
          </span>
        ))}
      </div>
      {parsed.map((file) => (
        <section className="diff-file" key={file.path}>
          <header>
            <strong>{file.path}</strong>
            {file.hasConflict ? <span className="warning">Conflict markers detected</span> : null}
            {file.sensitive ? <span className="warning">Sensitive file warning</span> : null}
          </header>
          <pre>
            {file.lines.map((line, index) => (
              <span className={`diff-line diff-${line.type}`} key={`${file.path}-${index}`}>
                {line.text || ' '}
                {'\n'}
              </span>
            ))}
          </pre>
        </section>
      ))}
    </div>
  );
}

