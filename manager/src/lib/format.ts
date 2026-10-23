/** Digits of the WhatsApp number inside a JID ('5511999990000:12@s.whatsapp.net' -> '5511999990000'). */
export function numberFromJid(jid: string | undefined | null): string {
  if (!jid) return '';
  return (jid.split('@')[0] ?? '').split(':')[0] ?? '';
}

/** Friendly phone: '+55 11 99999-0000' for Brazilian numbers, '+<digits>' otherwise. */
export function formatPhone(digits: string): string {
  if (!digits) return '';
  const d = digits.replace(/\D/g, '');
  if (d.startsWith('55') && (d.length === 13 || d.length === 12)) {
    const ddd = d.slice(2, 4);
    const rest = d.slice(4);
    const split = rest.length === 9 ? 5 : 4;
    return `+55 ${ddd} ${rest.slice(0, split)}-${rest.slice(split)}`;
  }
  return `+${d}`;
}

const rtf = typeof Intl !== 'undefined' ? new Intl.RelativeTimeFormat('pt-BR', { numeric: 'auto' }) : null;

/** 'há 3 dias', 'ontem', 'agora mesmo'... */
export function timeAgo(input: string | number | Date | undefined | null, now = Date.now()): string {
  if (!input) return '—';
  const t = new Date(input).getTime();
  if (Number.isNaN(t)) return '—';
  const diff = Math.round((t - now) / 1000);
  const abs = Math.abs(diff);
  if (abs < 45) return 'agora mesmo';
  const units: [Intl.RelativeTimeFormatUnit, number][] = [
    ['year', 31536000],
    ['month', 2592000],
    ['week', 604800],
    ['day', 86400],
    ['hour', 3600],
    ['minute', 60],
  ];
  for (const [unit, secs] of units) {
    if (abs >= secs) return rtf?.format(Math.round(diff / secs), unit) ?? '';
  }
  return 'agora mesmo';
}

export function formatDateTime(input: string | number | Date | undefined | null): string {
  if (!input) return '—';
  const d = new Date(input);
  if (Number.isNaN(d.getTime())) return '—';
  return new Intl.DateTimeFormat('pt-BR', { dateStyle: 'medium', timeStyle: 'short' }).format(d);
}

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

export function initials(name: string): string {
  const parts = name
    .trim()
    .split(/[\s._-]+/)
    .filter(Boolean);
  if (parts.length === 0) return '?';
  if (parts.length === 1) return (parts[0] ?? '?').slice(0, 2).toUpperCase();
  return ((parts[0]?.[0] ?? '') + (parts[1]?.[0] ?? '')).toUpperCase();
}

/** Stable hue (0-359) from a string, so each instance keeps the same avatar color. */
export function hueFromString(s: string): number {
  let h = 0;
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) >>> 0;
  return h % 360;
}

export function maskToken(token: string): string {
  if (token.length <= 8) return '•'.repeat(token.length);
  return `${token.slice(0, 4)}${'•'.repeat(12)}${token.slice(-4)}`;
}

export function hostOf(url: string): string {
  try {
    return new URL(url).host;
  } catch {
    return url;
  }
}

export function isHttpUrl(value: string): boolean {
  try {
    const u = new URL(value);
    return u.protocol === 'http:' || u.protocol === 'https:';
  } catch {
    return false;
  }
}

/** RFC 4122 v4 token. crypto.randomUUID is missing on plain-http origins, so fall back to getRandomValues. */
export function randomToken(): string {
  const c = crypto as Crypto & { randomUUID?: () => string };
  if (typeof c.randomUUID === 'function') return c.randomUUID();
  const b = c.getRandomValues(new Uint8Array(16));
  b[6] = ((b[6] ?? 0) & 0x0f) | 0x40;
  b[8] = ((b[8] ?? 0) & 0x3f) | 0x80;
  const hex = [...b].map((x) => x.toString(16).padStart(2, '0')).join('');
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

/** Locale-aware metric display for Manager cards. */
export function formatNumber(value: number): string {
  return new Intl.NumberFormat('pt-BR').format(value);
}
