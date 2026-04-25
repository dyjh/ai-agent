import type { Citation } from '../types/domain';
import { formatDate } from '../utils/format';
import { redactForDisplay, truncateMiddle } from '../utils/sanitize';

interface CitationListProps {
  citations?: Citation[];
}

export function CitationList({ citations = [] }: CitationListProps) {
  if (citations.length === 0) {
    return <div className="empty-state">No citations.</div>;
  }
  return (
    <div className="citation-list">
      {citations.map((citation, index) => (
        <details key={`${citation.chunk_id ?? index}-${index}`} className="citation-item">
          <summary>
            <span>{citation.title || citation.source_file || citation.url || 'Untitled source'}</span>
            <span>{typeof citation.score === 'number' ? citation.score.toFixed(3) : '-'}</span>
          </summary>
          <dl className="kv-grid">
            <dt>Source</dt>
            <dd>{truncateMiddle(String(citation.source_file || citation.url || '-'), 120)}</dd>
            <dt>Section</dt>
            <dd>{String(citation.section || '-')}</dd>
            <dt>Chunk</dt>
            <dd>{String(citation.chunk_id || '-')}</dd>
            <dt>Updated</dt>
            <dd>{formatDate(citation.updated_at)}</dd>
          </dl>
          {citation.snippet ? <pre>{redactForDisplay(citation.snippet)}</pre> : null}
        </details>
      ))}
    </div>
  );
}

