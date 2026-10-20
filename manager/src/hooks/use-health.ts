import { useQuery } from '@tanstack/react-query';
import { fetchHealth } from '@/api/session';
import { fetchHealthMetrics } from '@/api/health';
import type { HealthStatus, HealthMetricsResponse } from '@/api/types';

// Re-export HealthMetricsResponse type for convenience
export type { HealthMetricsResponse };

export function useHealth() {
  return useQuery({
    queryKey: ['health'],
    queryFn: ({ signal }) => fetchHealth(signal),
    refetchInterval: 30_000,
    staleTime: 15_000,
  });
}

export function useHealthMetrics() {
  return useQuery<HealthMetricsResponse>({
    queryKey: ['health-metrics'],
    queryFn: ({ signal }) => fetchHealthMetrics(signal),
    refetchInterval: 30_000,
    staleTime: 15_000,
  });
}

export const HEALTH_META: Record<HealthStatus, { label: string; detail: string; tone: 'ok' | 'warn' | 'danger' }> = {
  ok: { label: 'Operacional', detail: 'Bancos de dados respondendo', tone: 'ok' },
  degraded: { label: 'Degradado', detail: 'Um banco de dados está lento', tone: 'warn' },
  unavailable: { label: 'Indisponível', detail: 'Falha em um banco de dados', tone: 'danger' },
  unreachable: { label: 'Sem conexão', detail: 'Servidor sem resposta', tone: 'danger' },
};
