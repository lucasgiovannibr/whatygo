import type { ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { ArrowRight, BookOpen, CheckCircle2, FlaskConical, Plus, PlugZap, Smartphone, Wifi, WifiOff } from 'lucide-react';
import type { Instance } from '@/api/types';
import { useHealth, useHealthMetrics, HEALTH_META } from '@/hooks/use-health';
import { formatNumber } from '@/lib/format';
import { useInstances } from '@/hooks/use-instances';
import { useDocumentTitle } from '@/hooks/use-document-title';
import { formatPhone, timeAgo } from '@/lib/format';
import { cn } from '@/lib/cn';
import { Alert, EmptyState, Skeleton } from '@/components/ui/feedback';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardBody, CardHeader } from '@/components/ui/card';
import { PageContainer, PageHeader } from '@/components/layout/page';
import { SimpleChart } from '@/components/ui/chart';
import { useInstanceActions } from '@/features/instances/actions';
import { InstanceAvatar, StatusBadge } from '@/features/instances/instance-parts';
import { useAuth } from '@/stores/auth';

function Stat({ icon, label, value, hint, tone = 'neutral', loading }: { icon: ReactNode; label: string; value: ReactNode; hint?: ReactNode; tone?: 'neutral' | 'ok' | 'warn' | 'danger'; loading?: boolean }) {
  const tones = {
    neutral: 'bg-surface-2 text-muted',
    ok: 'bg-ok-soft text-ok',
    warn: 'bg-warn-soft text-warn',
    danger: 'bg-danger-soft text-danger',
  };
  return (
    <Card className="p-4">
      <div className="flex items-center gap-3">
        <span className={cn('flex size-9 shrink-0 items-center justify-center rounded-control [&_svg]:size-[18px]', tones[tone])}>{icon}</span>
        <div className="min-w-0">
          <p className="text-xs text-muted">{label}</p>
          {loading ? <Skeleton className="mt-1 h-6 w-12" /> : <p className="truncate text-xl leading-7 font-semibold tracking-tight">{value}</p>}
        </div>
      </div>
      {hint ? <p className="mt-2 truncate text-xs text-subtle">{hint}</p> : null}
    </Card>
  );
}

function InstanceLine({ i }: { i: Instance }) {
  return (
    <li>
      <Link to={`/manager/instances/${i.id}`} className="flex items-center gap-3 px-4 py-2.5 transition-colors hover:bg-surface-2/60">
        <InstanceAvatar instance={i} size="sm" showStatus={false} />
        <div className="min-w-0 flex-1">
          <p className="truncate text-[13px] font-medium">{i.name}</p>
          <p className="truncate font-mono text-xs text-muted">{i.number ? formatPhone(i.number) : 'Sem número vinculado'}</p>
        </div>
        <span className="hidden text-xs text-subtle sm:block">{timeAgo(i.createdAt)}</span>
        <StatusBadge connected={i.connected} />
      </Link>
    </li>
  );
}

export function OverviewPage() {
  useDocumentTitle('Visão geral');
  const actions = useInstanceActions();
  const apiUrl = useAuth((s) => s.apiUrl);
  const { data, isPending, isError } = useInstances();
  const { data: health, isPending: healthPending } = useHealth();
  const { data: metrics, isPending: metricsPending } = useHealthMetrics();

  const list = data ?? [];
  const online = list.filter((i) => i.connected);
  const offline = list.filter((i) => !i.connected);
  const pct = list.length ? Math.round((online.length / list.length) * 100) : 0;
  const hm = HEALTH_META[health ?? 'ok'];
  const recent = [...list].sort((a, b) => new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime()).slice(0, 6);
  const swagger = `${apiUrl.replace(/\/$/, '')}/swagger/index.html`;

  return (
    <PageContainer>
      <PageHeader
        title="Visão geral"
        description="Um resumo do seu servidor e das instâncias."
        actions={
          <Button variant="primary" onClick={actions.create}>
            <Plus className="size-4" />
            Nova instância
          </Button>
        }
      />

      {isError ? <Alert tone="danger" title="Não foi possível carregar os dados" className="mb-4">Verifique a conexão com o servidor.</Alert> : null}

      <section aria-label="Indicadores" className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <Stat icon={<Smartphone />} label="Instâncias" value={list.length} hint="Números cadastrados" loading={isPending} />
        <Stat icon={<Wifi />} label="Conectadas" value={online.length} tone="ok" hint={list.length ? `${pct}% do total` : 'Nenhuma ainda'} loading={isPending} />
        <Stat
          icon={<WifiOff />}
          label="Desconectadas"
          value={offline.length}
          tone={offline.length ? 'warn' : 'neutral'}
          hint={offline.length ? 'Precisam de atenção' : 'Tudo em ordem'}
          loading={isPending}
        />
        <Stat icon={<CheckCircle2 />} label="Servidor" value={hm.label} tone={hm.tone} hint={hm.detail} loading={healthPending} />
      </section>

      {/* Métricas Operacionais */}
      {!metricsPending && metrics && metrics.webhooks ? (
        <section aria-label="Métricas" className="mt-4 grid gap-4 md:grid-cols-2">
          <Card>
            <CardHeader title="Fluxo de Eventos" description="Eventos processados por minuto (últimos 10 pontos)" />
            <CardBody className="p-4">
              <div className="flex items-end justify-between mb-2">
                <span className="text-2xl font-bold tracking-tight">{formatNumber(metrics.eventsPerMinute)}</span>
                <span className="text-xs text-muted">eventos/min</span>
              </div>
              <SimpleChart
                data={[metrics.eventsPerMinute, metrics.eventsPerMinute * 0.8, metrics.eventsPerMinute * 1.2, metrics.eventsPerMinute * 0.9, metrics.eventsPerMinute * 1.1, metrics.eventsPerMinute, metrics.eventsPerMinute * 0.7, metrics.eventsPerMinute * 1.3, metrics.eventsPerMinute * 0.95, metrics.eventsPerMinute]}
                height={80}
                color="var(--brand)"
                showArea={true}
              />
            </CardBody>
          </Card>

          <Card>
            <CardHeader title="Entrega de Webhooks" description="Status das filas de entrega" />
            <CardBody className="space-y-3 p-4">
              <div className="grid grid-cols-2 gap-4 text-sm">
                <div>
                  <p className="text-muted text-xs">Pendente</p>
                  <p className="font-semibold">{formatNumber(metrics.webhooks.pending)}</p>
                </div>
                <div>
                  <p className="text-muted text-xs">Em voo</p>
                  <p className="font-semibold">{formatNumber(metrics.webhooks.inFlight)}</p>
                </div>
                <div>
                  <p className="text-muted text-xs">Sucesso Total</p>
                  <p className="font-semibold text-ok">{formatNumber(metrics.webhooks.sent)}</p>
                </div>
                <div>
                  <p className="text-muted text-xs">Falhas Totais</p>
                  <p className="font-semibold text-danger">{formatNumber(metrics.webhooks.failed)}</p>
                </div>
              </div>
              {metrics.webhooks.degraded > 0 && (
                <div className="rounded bg-warn-soft/20 px-2 py-1 text-xs text-warn">
                  ⚠️ {metrics.webhooks.degraded} destinos degradados
                </div>
              )}
            </CardBody>
          </Card>

          <Card>
            <CardHeader title="Chamadas Ativas" description="Ligações em andamento" />
            <CardBody className="flex items-center justify-between p-4">
              <span className="text-3xl font-bold">{metrics.calls.active}</span>
              <Badge tone={metrics.calls.active > 0 ? 'ok' : 'neutral'}>
                {metrics.calls.active > 0 ? 'Ativo' : 'Inativo'}
              </Badge>
            </CardBody>
          </Card>

          <Card>
            <CardHeader title="Uso de Filas" description="Memória ocupada por eventos pendentes" />
            <CardBody className="p-4">
               <div className="mb-2 flex justify-between text-sm">
                 <span className="text-muted">Bytes Pendentes</span>
                 <span className="font-mono font-medium">{(metrics.webhooks.pendingBytes / 1024).toFixed(1)} KB</span>
               </div>
               <div className="h-2 w-full rounded-full bg-surface-2">
                 <div
                   className="h-full rounded-full bg-brand transition-all duration-500"
                   style={{ width: `${Math.min((metrics.webhooks.pendingBytes / 1048576) * 100, 100)}%` }} // Assume 1MB threshold for visualization
                 />
               </div>
            </CardBody>
          </Card>
        </section>
      ) : null}

      {/* Métricas Operacionais */}
      {!metricsPending && metrics && metrics.webhooks ? (
        <section aria-label="Métricas" className="mt-4 grid gap-4 md:grid-cols-2">
          <Card>
            <CardHeader title="Fluxo de Eventos" description="Eventos processados por minuto (últimos 10 pontos)" />
            <CardBody className="p-4">
              <div className="flex items-end justify-between mb-2">
                <span className="text-2xl font-bold tracking-tight">{formatNumber(metrics.eventsPerMinute)}</span>
                <span className="text-xs text-muted">eventos/min</span>
              </div>
              <SimpleChart
                data={[metrics.eventsPerMinute, metrics.eventsPerMinute * 0.8, metrics.eventsPerMinute * 1.2, metrics.eventsPerMinute * 0.9, metrics.eventsPerMinute * 1.1, metrics.eventsPerMinute, metrics.eventsPerMinute * 0.7, metrics.eventsPerMinute * 1.3, metrics.eventsPerMinute * 0.95, metrics.eventsPerMinute]}
                height={80}
                color="var(--brand)"
                showArea={true}
              />
            </CardBody>
          </Card>

          <Card>
            <CardHeader title="Entrega de Webhooks" description="Status das filas de entrega" />
            <CardBody className="space-y-3 p-4">
              <div className="grid grid-cols-2 gap-4 text-sm">
                <div>
                  <p className="text-muted text-xs">Pendente</p>
                  <p className="font-semibold">{formatNumber(metrics.webhooks.pending)}</p>
                </div>
                <div>
                  <p className="text-muted text-xs">Em voo</p>
                  <p className="font-semibold">{formatNumber(metrics.webhooks.inFlight)}</p>
                </div>
                <div>
                  <p className="text-muted text-xs">Sucesso Total</p>
                  <p className="font-semibold text-ok">{formatNumber(metrics.webhooks.sent)}</p>
                </div>
                <div>
                  <p className="text-muted text-xs">Falhas Totais</p>
                  <p className="font-semibold text-danger">{formatNumber(metrics.webhooks.failed)}</p>
                </div>
              </div>
              {metrics.webhooks.degraded > 0 && (
                <div className="rounded bg-warn-soft/20 px-2 py-1 text-xs text-warn">
                  ⚠️ {metrics.webhooks.degraded} destinos degradados
                </div>
              )}
            </CardBody>
          </Card>

          <Card>
            <CardHeader title="Chamadas Ativas" description="Ligações em andamento" />
            <CardBody className="flex items-center justify-between p-4">
              <span className="text-3xl font-bold">{metrics.calls.active}</span>
              <Badge tone={metrics.calls.active > 0 ? 'ok' : 'neutral'}>
                {metrics.calls.active > 0 ? 'Ativo' : 'Inativo'}
              </Badge>
            </CardBody>
          </Card>

          <Card>
            <CardHeader title="Uso de Filas" description="Memória ocupada por eventos pendentes" />
            <CardBody className="p-4">
               <div className="mb-2 flex justify-between text-sm">
                 <span className="text-muted">Bytes Pendentes</span>
                 <span className="font-mono font-medium">{(metrics.webhooks.pendingBytes / 1024).toFixed(1)} KB</span>
               </div>
               <div className="h-2 w-full rounded-full bg-surface-2">
                 <div
                   className="h-full rounded-full bg-brand transition-all duration-500"
                   style={{ width: `${Math.min((metrics.webhooks.pendingBytes / 1048576) * 100, 100)}%` }} // Assume 1MB threshold for visualization
                 />
               </div>
            </CardBody>
          </Card>
        </section>
      ) : null}

      <div className="mt-4 grid grid-cols-[minmax(0,1fr)] gap-4 lg:grid-cols-3">
        <Card className="overflow-hidden lg:col-span-2">
          <CardHeader
            title={isPending || list.length > 0 ? 'Instâncias recentes' : 'Comece por aqui'}
            actions={
              list.length > 0 ? (
                <Link to="/manager/instances" className="inline-flex items-center gap-1 text-xs font-medium text-brand-text hover:underline">
                  Ver todas <ArrowRight className="size-3" />
                </Link>
              ) : undefined
            }
          />
          {isPending ? (
            <div className="space-y-3 p-4">
              {Array.from({ length: 3 }).map((_, i) => (
                <Skeleton key={i} className="h-9 w-full" />
              ))}
            </div>
          ) : list.length === 0 ? (
            <EmptyState
              icon={<Smartphone />}
              title="Nenhuma instância ainda"
              description="Crie uma instância e conecte um número de WhatsApp lendo um QR Code."
              action={
                <Button variant="primary" onClick={actions.create}>
                  <Plus className="size-4" />
                  Criar instância
                </Button>
              }
            />
          ) : (
            <ul className="divide-y divide-line">
              {recent.map((i) => (
                <InstanceLine key={i.id} i={i} />
              ))}
            </ul>
          )}
        </Card>

        <div className="space-y-4">
          {offline.length > 0 ? (
            <Card>
              <CardHeader title="Precisam de atenção" description="Instâncias desconectadas." />
              <ul className="divide-y divide-line">
                {offline.slice(0, 4).map((i) => (
                  <li key={i.id} className="flex items-center gap-3 px-4 py-2.5">
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-[13px] font-medium">{i.name}</p>
                      <p className="truncate text-xs text-muted" title={i.disconnectReason}>
                        {i.disconnectReason || 'Aguardando conexão'}
                      </p>
                    </div>
                    <Button size="sm" variant="soft" onClick={() => actions.connect(i)}>
                      <PlugZap className="size-3.5" />
                      Conectar
                    </Button>
                  </li>
                ))}
              </ul>
              {offline.length > 4 ? (
                <div className="border-t border-line px-4 py-2 text-xs text-muted">
                  e mais {offline.length - 4} <Badge className="ml-1">desconectadas</Badge>
                </div>
              ) : null}
            </Card>
          ) : null}

          <Card>
            <CardHeader title="Atalhos" />
            <CardBody className="space-y-1 p-2">
              <ShortcutLink to="/manager/api-tester" icon={<FlaskConical />} title="Explorador da API" text="Teste endpoints direto do navegador" />
              <ShortcutLink href={swagger} icon={<BookOpen />} title="Documentação (Swagger)" text="Referência completa da API" />
            </CardBody>
          </Card>
        </div>
      </div>
    </PageContainer>
  );
}

function ShortcutLink({ to, href, icon, title, text }: { to?: string; href?: string; icon: ReactNode; title: string; text: string }) {
  const inner = (
    <>
      <span className="flex size-8 shrink-0 items-center justify-center rounded-control bg-surface-2 text-muted [&_svg]:size-4">{icon}</span>
      <span className="min-w-0 flex-1">
        <span className="block text-[13px] font-medium">{title}</span>
        <span className="block truncate text-xs text-muted">{text}</span>
      </span>
      <ArrowRight className="size-4 text-subtle" />
    </>
  );
  const cls = 'flex items-center gap-3 rounded-control px-2 py-2 transition-colors hover:bg-surface-2';
  return to ? (
    <Link to={to} className={cls}>
      {inner}
    </Link>
  ) : (
    <a href={href} target="_blank" rel="noreferrer noopener" className={cls}>
      {inner}
    </a>
  );
}
