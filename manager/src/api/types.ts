import { coversAllEvents, parseEvents } from '@/lib/events';
import { numberFromJid } from '@/lib/format';

/** Shape returned by /instance/all and /instance/info/:id (pkg/instance/model). */
export interface RawInstance {
  id: string;
  name: string;
  token: string;
  webhook?: string;
  rabbitmqEnable?: string;
  websocketEnable?: string;
  natsEnable?: string;
  jid?: string;
  qrcode?: string;
  connected?: boolean;
  disconnect_reason?: string;
  events?: string;
  os_name?: string;
  proxy?: string;
  client_name?: string;
  createdAt?: string;
  alwaysOnline?: boolean;
  rejectCall?: boolean;
  msgRejectCall?: string;
  callsEnabled?: boolean;
  readMessages?: boolean;
  ignoreGroups?: boolean;
  ignoreStatus?: boolean;
}

/** "" means "use the server default" (and, on save, "leave as is"). */
export type ProducerState = '' | 'enabled' | 'disabled';

export interface Producers {
  rabbitmq: ProducerState;
  websocket: ProducerState;
  nats: ProducerState;
}

export interface BehaviorSettings {
  alwaysOnline: boolean;
  rejectCall: boolean;
  msgRejectCall: string;
  /** Turns on the call engine (answer, dial, stream); takes effect on the next connection. */
  callsEnabled: boolean;
  readMessages: boolean;
  ignoreGroups: boolean;
  ignoreStatus: boolean;
}

export interface Instance {
  id: string;
  name: string;
  token: string;
  jid: string;
  number: string;
  connected: boolean;
  disconnectReason: string;
  createdAt: string;
  webhook: string;
  events: string[];
  allEvents: boolean;
  producers: Producers;
  behavior: BehaviorSettings;
  proxy: { host: string; port: string } | null;
  osName: string;
  clientName: string;
}

const producer = (v?: string): ProducerState => (v === 'enabled' || v === 'disabled' ? v : '');

function parseProxy(raw?: string): Instance['proxy'] {
  if (!raw) return null;
  try {
    const p = JSON.parse(raw) as { host?: string; port?: string } | null;
    return p?.host ? { host: p.host, port: p.port ?? '' } : null;
  } catch {
    return null;
  }
}

export function toInstance(r: RawInstance): Instance {
  const events = parseEvents(r.events);
  return {
    id: r.id,
    name: r.name,
    token: r.token,
    jid: r.jid ?? '',
    number: numberFromJid(r.jid),
    connected: !!r.connected,
    disconnectReason: r.disconnect_reason ?? '',
    createdAt: r.createdAt ?? '',
    webhook: r.webhook ?? '',
    events,
    allEvents: coversAllEvents(events),
    producers: {
      rabbitmq: producer(r.rabbitmqEnable),
      websocket: producer(r.websocketEnable),
      nats: producer(r.natsEnable),
    },
    behavior: {
      alwaysOnline: !!r.alwaysOnline,
      rejectCall: !!r.rejectCall,
      msgRejectCall: r.msgRejectCall ?? '',
      callsEnabled: !!r.callsEnabled,
      readMessages: !!r.readMessages,
      ignoreGroups: !!r.ignoreGroups,
      ignoreStatus: !!r.ignoreStatus,
    },
    proxy: parseProxy(r.proxy),
    osName: r.os_name ?? '',
    clientName: r.client_name ?? '',
  };
}

export interface QrInfo {
  qrcode: string;
  code: string;
  passkeyStage?: string;
  passkeyOpenUrl?: string;
  passkeyCode?: string;
}

export type HealthStatus = 'ok' | 'degraded' | 'unavailable' | 'unreachable';

export interface WebhookStats {
  pending: number;
  pendingBytes: number;
  inFlight: number;
  destinations: number;
  degraded: number;
  sent: number;
  failed: number;
}

export interface HealthMetricsResponse {
  instances: { total: number; connected: number; disconnected: number };
  webhooks: WebhookStats;
}
