const SECRET_PATTERNS: RegExp[] = [
  /(api[_-]?key|token|secret|password|passwd|cookie|session)\s*[:=]\s*["']?[^"',\s}]+/gi,
  /bearer\s+[a-z0-9._~+/-]+=*/gi,
  /-----BEGIN\s+[A-Z ]*PRIVATE KEY-----[\s\S]*?-----END\s+[A-Z ]*PRIVATE KEY-----/g
];

export function redactForDisplay(value: unknown): string {
  const raw = typeof value === 'string' ? value : JSON.stringify(value, null, 2);
  return SECRET_PATTERNS.reduce((text, pattern) => text.replace(pattern, (match) => {
    const separator = match.includes('=') ? '=' : match.includes(':') ? ':' : ' ';
    const label = match.split(separator)[0];
    return `${label}${separator} [REDACTED]`;
  }), raw ?? '');
}

export function escapeHtml(input: string): string {
  return input
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;');
}

export function sanitizeText(input: unknown): string {
  return escapeHtml(redactForDisplay(input));
}

export function truncateMiddle(input: string, max = 96): string {
  if (input.length <= max) {
    return input;
  }
  const head = Math.floor(max / 2) - 2;
  const tail = max - head - 3;
  return `${input.slice(0, head)}...${input.slice(input.length - tail)}`;
}

