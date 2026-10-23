import { api } from '@/lib/http';
import type { HealthMetricsResponse } from './types';

/** Fetches detailed operational metrics (admin protected). */
export async function fetchHealthMetrics(signal?: AbortSignal): Promise<HealthMetricsResponse> {
  return api<HealthMetricsResponse>('/health/metrics', { signal });
}
