import { useAuth } from '@/stores/auth';

export class ApiError extends Error {
  status: number;
  code?: string;
  data?: unknown;

  constructor(message: string, status: number, code?: string, data?: unknown) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
    this.data = data;
  }
}

export interface RequestOptions {
  method?: string;
  body?: unknown;
  query?: Record<string, string | number | undefined>;
  /** Overrides the stored API key (instance token, custom key, or an explicit pre-login key). */
  apikey?: string | null;
  /** Overrides the stored base URL (used while logging in). */
  baseUrl?: string;
  headers?: Record<string, string>;
  signal?: AbortSignal;
  timeoutMs?: number;
}

export interface RawResponse {
  status: number;
  statusText: string;
  headers: Record<string, string>;
  data: unknown;
  ms: number;
  size: number;
}

const LOGIN_PATH = '/manager/login';
export const EXPIRED_FLAG = 'whatygo-session-expired';
let redirecting = false;

function expireSession() {
  if (redirecting) return;
  redirecting = true;
  try {
    sessionStorage.setItem(EXPIRED_FLAG, 'session');
  } catch {
    /* the login page simply will not show the notice */
  }
  useAuth.getState().clear();
  window.location.assign(LOGIN_PATH);
}

function buildUrl(base: string, path: string, query?: RequestOptions['query']) {
  // Keep any base path so sub-path deployments (https://host/evo) still resolve.
  const url = new URL(base);
  url.pathname = `${url.pathname.replace(/\/+$/, '')}/${path.replace(/^\/+/, '')}`;
  if (query) for (const [k, v] of Object.entries(query)) if (v !== undefined && v !== '') url.searchParams.set(k, String(v));
  return url.toString();
}

function withTimeout(signal: AbortSignal | undefined, ms: number): AbortSignal {
  const t = AbortSignal.timeout(ms);
  return signal ? AbortSignal.any([signal, t]) : t;
}

/** Low-level call that never throws on HTTP status: used by the API explorer. */
export async function rawRequest(path: string, opts: RequestOptions = {}): Promise<RawResponse> {
  const { apiUrl, apiKey } = useAuth.getState();
  const key = opts.apikey === undefined ? apiKey : opts.apikey;
  const headers: Record<string, string> = { ...(opts.headers ?? {}) };
  if (key) headers.apikey = key;
  let body: BodyInit | undefined;
  if (opts.body !== undefined) {
    headers['Content-Type'] ??= 'application/json';
    body = typeof opts.body === 'string' ? opts.body : JSON.stringify(opts.body);
  }

  const started = performance.now();
  const res = await fetch(buildUrl(opts.baseUrl ?? apiUrl, path, opts.query), {
    method: opts.method ?? 'GET',
    headers,
    body,
    signal: withTimeout(opts.signal, opts.timeoutMs ?? 30_000),
  });
  const text = await res.text();
  const ms = Math.round(performance.now() - started);

  let data: unknown = text;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      /* keep as text */
    }
  } else {
    data = null;
  }
  const resHeaders: Record<string, string> = {};
  res.headers.forEach((v, k) => {
    resHeaders[k] = v;
  });
  return {
    status: res.status,
    statusText: res.statusText,
    headers: resHeaders,
    data,
    ms,
    size: new Blob([text]).size,
  };
}

function errorMessage(data: unknown, fallback: string): string {
  if (data && typeof data === 'object') {
    const d = data as Record<string, unknown>;
    const m = d.error ?? d.message;
    if (typeof m === 'string' && m) return m;
  }
  if (typeof data === 'string' && data.trim() && data.length < 300) return data;
  return fallback;
}

/** Typed JSON call that throws ApiError and handles session expiry globally. */
export async function api<T = unknown>(path: string, opts: RequestOptions = {}): Promise<T> {
  let res: RawResponse;
  try {
    res = await rawRequest(path, opts);
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') throw err;
    const timedOut = err instanceof DOMException && err.name === 'TimeoutError';
    throw new ApiError(
      timedOut ? 'O servidor demorou demais para responder.' : 'Não foi possível conectar ao servidor. Verifique a URL e sua rede.',
      0,
    );
  }

  if (res.status >= 200 && res.status < 300) return res.data as T;

  const code = res.data && typeof res.data === 'object' ? (res.data as Record<string, unknown>).code : undefined;
  const codeStr = typeof code === 'string' ? code : undefined;
  // Only calls made with the stored session may end it: pre-login calls (explicit baseUrl) and calls
  // made with another key (an instance token) report their own error without logging the admin out.
  const usesSession = opts.baseUrl === undefined && opts.apikey === undefined;

  if (usesSession && res.status === 401) expireSession();
  throw new ApiError(errorMessage(res.data, `Erro ${res.status}`), res.status, codeStr, res.data);
}
