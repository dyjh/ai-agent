export function formatDate(value?: string): string {
  if (!value) {
    return '-';
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit'
  }).format(date);
}

export function compactJson(value: unknown): string {
  if (value == null) {
    return '';
  }
  if (typeof value === 'string') {
    return value;
  }
  return JSON.stringify(value, null, 2);
}

export function asArray<T = unknown>(value: unknown): T[] {
  if (Array.isArray(value)) {
    return value as T[];
  }
  if (value && typeof value === 'object' && Array.isArray((value as { items?: unknown[] }).items)) {
    return (value as { items: unknown[] }).items as T[];
  }
  return [];
}

export function objectLabel(value: unknown, fallback = 'item'): string {
  if (!value || typeof value !== 'object') {
    return fallback;
  }
  const obj = value as Record<string, unknown>;
  return String(obj.name ?? obj.title ?? obj.id ?? obj.run_id ?? obj.case_id ?? fallback);
}

