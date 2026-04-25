import { redactForDisplay } from '../utils/sanitize';

interface MarkdownSafeProps {
  content?: unknown;
}

export function MarkdownSafe({ content }: MarkdownSafeProps) {
  const text = redactForDisplay(content ?? '');
  const blocks = text.split(/\n{2,}/);
  return (
    <div className="markdown-safe">
      {blocks.map((block, index) => {
        const trimmed = block.trimEnd();
        if (trimmed.startsWith('```')) {
          return <pre key={index}>{trimmed.replace(/^```[a-zA-Z0-9_-]*\n?/, '').replace(/```$/, '')}</pre>;
        }
        if (trimmed.startsWith('#')) {
          return <h3 key={index}>{trimmed.replace(/^#+\s*/, '')}</h3>;
        }
        if (trimmed.startsWith('- ') || trimmed.startsWith('* ')) {
          return (
            <ul key={index}>
              {trimmed.split('\n').map((line, lineIndex) => (
                <li key={lineIndex}>{line.replace(/^[-*]\s*/, '')}</li>
              ))}
            </ul>
          );
        }
        return <p key={index}>{trimmed}</p>;
      })}
    </div>
  );
}

