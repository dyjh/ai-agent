import { describe, expect, it } from 'vitest';

import { parseUnifiedDiff } from '../src/components/DiffViewer';

describe('diff parser', () => {
  it('renders multi-file diff metadata', () => {
    const files = parseUnifiedDiff('diff --git a/a.go b/a.go\n@@\n-old\n+new\ndiff --git a/.env b/.env\n+TOKEN=value');
    expect(files).toHaveLength(2);
    expect(files[0].lines.some((line) => line.type === 'add')).toBe(true);
    expect(files[1].sensitive).toBe(true);
  });

  it('marks conflict lines', () => {
    const files = parseUnifiedDiff('diff --git a/a b/a\n<<<<<<< HEAD');
    expect(files[0].hasConflict).toBe(true);
  });
});

