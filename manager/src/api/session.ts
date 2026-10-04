import { api, ApiError, rawRequest } from '@/lib/http';
import type { HealthStatus } from './types';

/** Session calls. They run before the store is populated, so they pass url/key explicitly. */

/** Validates the key against an authenticated endpoint. */
export async function verifyApiKey(baseUrl: string, apikey: string): Promise<void> {
  try {
    await api('/instance/all', { baseUrl, apikey, query: { t: Date.now() } });
  } catch (err) {
    if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
      throw new Error('API Key inválida. Verifique a chave informada.');
    }
    throw new Error('Não foi possível conectar. Verifique a URL e a API Key.');
  }
}

export async function fetchHealth(signal?: AbortSignal): Promise<HealthStatus> {
  try {
    const res = await rawRequest('/health', { apikey: null, signal, timeoutMs: 6000 });
    const status = (res.data as { status?: string } | null)?.status;
    if (status === 'ok' || status === 'degraded' || status === 'unavailable') return status;
    return res.status >= 500 ? 'unavailable' : 'ok';
  } catch {
    return 'unreachable';
  }
}
