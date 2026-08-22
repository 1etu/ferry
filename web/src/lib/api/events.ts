import { seal } from '$lib/seal/session.svelte';
import { session } from '$lib/session.svelte';
import {
  api,
  call,
  renewSeal,
  type Device,
  type Network,
  type OfferedFile,
  type Pairing,
  type Transfer,
  type UpdateStatus,
} from './client';

export type FileChange = { action: 'added' | 'removed'; file: OfferedFile };
export type DeviceChange = { action: 'requested' | 'approved' | 'revoked'; device: Device };

export type ServerEvent =
  | { kind: 'transfer'; transfer: Transfer }
  | ({ kind: 'file' } & FileChange)
  | ({ kind: 'device' } & DeviceChange)
  | { kind: 'pairing'; pairing: Pairing }
  | { kind: 'update'; status: UpdateStatus }
  | { kind: 'network'; network: Network }
  | { kind: 'reset' };

type EventKind = ServerEvent['kind'];

const eventsUrl = '/api/events';
const sealParam = 'seal';
const eventKinds: readonly EventKind[] = [
  'transfer',
  'file',
  'device',
  'pairing',
  'update',
  'network',
  'reset',
];
const downGraceMs = 5000;

function parseEvent(kind: EventKind, payload: string): ServerEvent {
  switch (kind) {
    case 'transfer':
      return { kind, transfer: JSON.parse(payload) as Transfer };
    case 'file':
      return { kind, ...(JSON.parse(payload) as FileChange) };
    case 'device':
      return { kind, ...(JSON.parse(payload) as DeviceChange) };
    case 'pairing':
      return { kind, pairing: JSON.parse(payload) as Pairing };
    case 'update':
      return { kind, status: JSON.parse(payload) as UpdateStatus };
    case 'network':
      return { kind, network: JSON.parse(payload) as Network };
    case 'reset':
      return { kind };
  }
}

function streamUrl(sealId: string | undefined): string {
  if (sealId === undefined) return eventsUrl;
  return `${eventsUrl}?${sealParam}=${encodeURIComponent(sealId)}`;
}

async function isServerReachable(): Promise<boolean> {
  const health = await call(api.GET('/api/health', { cache: 'no-store' }));
  return 'data' in health;
}

async function isSignedOut(): Promise<boolean> {
  const outcome = await call(api.GET('/api/session', { cache: 'no-store' }));
  return 'data' in outcome && outcome.data.role === 'none';
}

export function connect(
  onEvent: (event: ServerEvent) => void,
  onOpen: () => void,
  onConnectionChange: (isConnected: boolean) => void,
  isSealed = false,
): () => void {
  let isActive = true;
  let downTimer: ReturnType<typeof setTimeout> | undefined;
  let usedSealId: string | undefined;
  let source = open();

  function open(): EventSource {
    usedSealId = isSealed ? seal.session?.id : undefined;
    const next = new EventSource(streamUrl(usedSealId));
    next.addEventListener('open', handleOpen);
    next.addEventListener('error', handleError);
    for (const kind of eventKinds) {
      next.addEventListener(kind, (message: MessageEvent<string>) => {
        onEvent(parseEvent(kind, message.data));
      });
    }
    return next;
  }

  function reopen(): void {
    source.close();
    source = open();
  }

  function handleOpen(): void {
    clearTimeout(downTimer);
    downTimer = undefined;
    onConnectionChange(true);
    onOpen();
  }

  function handleError(): void {
    scheduleCheck();
  }

  function scheduleCheck(): void {
    downTimer ??= setTimeout(() => void checkConnection(), downGraceMs);
  }

  async function checkConnection(): Promise<void> {
    downTimer = undefined;
    if (source.readyState === EventSource.OPEN) return;
    const isReachable = await isServerReachable();
    const isRejected =
      isReachable && source.readyState === EventSource.CLOSED && (await isSignedOut());
    if (!isActive || source.readyState === EventSource.OPEN) return;
    if (isRejected) {
      session.reset();
      return;
    }
    onConnectionChange(isReachable);
    if (source.readyState !== EventSource.CLOSED) return;
    if (isSealed) void renewAndReopen();
    else reopen();
  }

  async function renewAndReopen(): Promise<void> {
    const isRenewed = await renewSeal(usedSealId);
    if (!isActive) return;
    if (isRenewed) reopen();
    else scheduleCheck();
  }

  function handleVisibilityChange(): void {
    if (document.visibilityState === 'visible' && source.readyState === EventSource.CLOSED) {
      reopen();
    }
  }

  document.addEventListener('visibilitychange', handleVisibilityChange);

  return () => {
    isActive = false;
    clearTimeout(downTimer);
    document.removeEventListener('visibilitychange', handleVisibilityChange);
    source.close();
  };
}
