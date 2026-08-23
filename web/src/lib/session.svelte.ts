import {
  api,
  call,
  type Device,
  type ErrorCode,
  type PairRequest,
  type ServerInfo,
} from '$lib/api/client';
import { connect, type DeviceChange, type ServerEvent } from '$lib/api/events';
import {
  isOriginReachable,
  isStandalone,
  pairUrlAt,
  readOriginalsHintShown,
  readPairToken,
  removeLaunchParams,
  writeOriginalsHintShown,
} from '$lib/browser';
import { files } from '$lib/files.svelte';
import { network } from '$lib/network.svelte';
import { pairing } from '$lib/pairing.svelte';
import { seal } from '$lib/seal/session.svelte';
import { settings } from '$lib/settings.svelte';
import { transfers } from '$lib/transfers.svelte';
import { update } from '$lib/update.svelte';
import { uploadQueue } from '$lib/uploads/queue.svelte';

export { isStandalone } from '$lib/browser';

export type PairStatus = 'idle' | 'wrong' | 'expired' | 'rateLimited' | 'notAllowed';

export type SessionState =
  | { kind: 'booting' }
  | { kind: 'connecting' }
  | { kind: 'pair'; status: PairStatus }
  | { kind: 'waiting'; device: Device }
  | { kind: 'phone'; device: Device }
  | { kind: 'pc' };

type LiveKind = 'waiting' | 'phone' | 'pc';
type LoadedState = Extract<SessionState, { kind: 'phone' | 'pc' }>;

const deviceName = 'iPhone';
const bootRetryMs = 2000;
const ownerStores = [transfers, files, pairing, settings, update, network];
const deviceStores = [transfers, files];
const statusByTokenError: Partial<Record<ErrorCode, PairStatus>> = {
  pairing_invalid: 'expired',
  pairing_expired: 'expired',
  rate_limited: 'rateLimited',
};
const statusByCodeError: Partial<Record<ErrorCode, PairStatus>> = {
  pairing_invalid: 'wrong',
  invalid_request: 'wrong',
  pairing_expired: 'expired',
  rate_limited: 'rateLimited',
};

function isLive(kind: SessionState['kind']): kind is LiveKind {
  return kind === 'waiting' || kind === 'phone' || kind === 'pc';
}

async function loadStores(kind: LoadedState['kind']): Promise<void> {
  const stores = kind === 'pc' ? ownerStores : deviceStores;
  await Promise.all(stores.map((store) => store.load()));
  if (kind === 'pc') return;
  uploadQueue.restore();
  for (const transfer of transfers.items) uploadQueue.applyTransfer(transfer);
}

class Session {
  state = $state<SessionState>({ kind: 'booting' });
  server = $state<ServerInfo>();
  isConnected = $state(true);
  originalsHintShown = $state(readOriginalsHintShown());

  #disconnect: (() => void) | undefined;
  #streamKind: LiveKind | undefined;
  #bootRetry: ReturnType<typeof setTimeout> | undefined;
  #resets = 0;

  async boot(): Promise<void> {
    const outcome = await call(api.GET('/api/session'));
    if ('code' in outcome) {
      this.state = { kind: 'connecting' };
      this.#bootRetry = setTimeout(() => void this.boot(), bootRetryMs);
      return;
    }
    const { role, device, server } = outcome.data;
    this.server = server;
    if (role === 'owner') {
      await this.#enterLoaded({ kind: 'pc' });
      return;
    }
    if (role === 'device' && device) {
      await this.#enterDevice(device);
      if (isStandalone()) removeLaunchParams();
      return;
    }
    const token = readPairToken();
    if (token === null) {
      this.#enter({ kind: 'pair', status: 'idle' });
      return;
    }
    this.state = { kind: 'connecting' };
    if (
      location.origin !== server.origins.local &&
      (await isOriginReachable(server.origins.local))
    ) {
      location.replace(pairUrlAt(server.origins.local, token));
      return;
    }
    if (!seal.adoptFragmentSecret()) seal.reset();
    const failure = await this.#pair({ token, name: deviceName, hasSecret: seal.hasSecret() });
    if (failure !== undefined) {
      removeLaunchParams();
      this.#enter({ kind: 'pair', status: statusByTokenError[failure] ?? 'idle' });
      return;
    }
    if (isStandalone()) removeLaunchParams();
  }

  async pairWithCode(code: string): Promise<void> {
    seal.reset();
    const failure = await this.#pair({ code, name: deviceName, hasSecret: false });
    if (failure !== undefined) {
      this.#enter({ kind: 'pair', status: statusByCodeError[failure] ?? 'idle' });
    }
  }

  dismissPairStatus(): void {
    if (this.state.kind === 'pair' && this.state.status !== 'idle') {
      this.state = { kind: 'pair', status: 'idle' };
    }
  }

  async forget(): Promise<void> {
    if (this.state.kind !== 'waiting' && this.state.kind !== 'phone') return;
    const deviceId = this.state.device.id;
    const outcome = await call(
      api.DELETE('/api/devices/{deviceId}', { params: { path: { deviceId } } }),
    );
    if ('data' in outcome) this.reset();
  }

  markOriginalsHint(): void {
    this.originalsHintShown = true;
    writeOriginalsHintShown();
  }

  reset(status: PairStatus = 'idle'): void {
    this.#resets += 1;
    seal.reset();
    removeLaunchParams();
    transfers.clear();
    files.clear();
    this.#enter({ kind: 'pair', status });
  }

  close(): void {
    clearTimeout(this.#bootRetry);
    this.#stopListening();
  }

  async #pair(request: PairRequest): Promise<ErrorCode | undefined> {
    const outcome = await call(api.POST('/api/pair', { body: request }));
    if ('code' in outcome) return outcome.code;
    await this.#enterDevice(outcome.data);
    return undefined;
  }

  async #enterDevice(device: Device): Promise<void> {
    switch (device.status) {
      case 'pending':
        this.#enter({ kind: 'waiting', device });
        return;
      case 'approved':
        await this.#enterPhone(device);
        return;
      case 'revoked':
        this.reset('notAllowed');
        return;
    }
  }

  async #enterPhone(device: Device): Promise<void> {
    if (seal.session === undefined) {
      const resets = this.#resets;
      const outcome = await seal.handshake();
      if (resets !== this.#resets) return;
      if (outcome === 'invalid') {
        this.reset();
        return;
      }
    }
    await this.#enterLoaded({ kind: 'phone', device });
  }

  async #enterLoaded(next: LoadedState): Promise<void> {
    const resets = this.#resets;
    await loadStores(next.kind);
    if (resets === this.#resets) this.#enter(next);
  }

  #enter(next: SessionState): void {
    this.state = next;
    if (!isLive(next.kind)) {
      this.#stopListening();
      return;
    }
    if (this.#streamKind === next.kind) return;
    this.#stopListening();
    this.#streamKind = next.kind;
    this.#disconnect = connect(
      (event) => {
        this.#dispatch(event);
      },
      () => void this.#refresh(),
      (isConnected) => {
        this.isConnected = isConnected;
      },
      next.kind === 'phone',
    );
  }

  #stopListening(): void {
    this.#disconnect?.();
    this.#disconnect = undefined;
    this.#streamKind = undefined;
    this.isConnected = true;
  }

  async #refresh(): Promise<void> {
    const { kind } = this.state;
    if (kind === 'waiting') await this.#recheckApproval();
    else if (kind === 'phone' || kind === 'pc') await loadStores(kind);
  }

  async #recheckApproval(): Promise<void> {
    const outcome = await call(api.GET('/api/session'));
    if ('code' in outcome || this.state.kind !== 'waiting') return;
    const { device } = outcome.data;
    if (device) await this.#enterDevice(device);
    else this.reset('notAllowed');
  }

  #dispatch(event: ServerEvent): void {
    switch (event.kind) {
      case 'transfer':
        uploadQueue.applyTransfer(transfers.apply(event.transfer));
        return;
      case 'file':
        files.apply(event);
        return;
      case 'device':
        this.#applyDevice(event);
        return;
      case 'pairing':
        pairing.applyPairing(event.pairing);
        return;
      case 'update':
        update.applyEvent(event.status);
        return;
      case 'network':
        network.applyEvent(event.network);
        return;
      case 'reset':
        void this.#refresh();
        return;
    }
  }

  #applyDevice(change: DeviceChange): void {
    if (this.state.kind === 'pc') {
      pairing.applyDevice(change);
      return;
    }
    if (this.state.kind !== 'waiting' && this.state.kind !== 'phone') return;
    if (this.state.device.id !== change.device.id) return;
    if (change.action !== 'revoked') {
      void this.#enterDevice(change.device);
      return;
    }
    this.reset(this.state.kind === 'waiting' ? 'notAllowed' : 'idle');
  }
}

export const session = new Session();
