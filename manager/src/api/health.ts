import { api } from '@/lib/http';
import type { HealthMetricsResponse } from './types';

/** Fetches basic health status (public/readiness). */
export async function fetchHealth(signal?: AbortSignal): Promise<string> {
  const res = await api<{ status: string }>('/health', { signal });
  return res.status;
}

/** Fetches detailed operational metrics (admin protected). */
export async function fetchHealthMetrics(signal?: AbortSignal): Promise<HealthMetricsResponse> {
  const res = await api<HealthMetricsResponse>('/health/metrics', { signal });
  return res;
}
