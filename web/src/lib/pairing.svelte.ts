import { SvelteMap } from 'svelte/reactivity';
import { api, call, type Device, type Pairing } from '$lib/api/client';
import { type DeviceChange } from '$lib/api/events';

function byNewest(a: Device, b: Device): number {
  return b.createdAt.localeCompare(a.createdAt) || b.id.localeCompare(a.id);
}

class PairingState {
  #devicesById = new SvelteMap<string, Device>();

  current = $state<Pairing>();
  readonly devices = $derived([...this.#devicesById.values()].sort(byNewest));
  readonly requests = $derived(
    this.devices.filter((device) => device.status === 'pending').reverse(),
  );
  readonly hasApprovedDevices = $derived(
    this.devices.some((device) => device.status === 'approved'),
  );

  async load(): Promise<void> {
    const [pairing, devices] = await Promise.all([
      call(api.GET('/api/pairing')),
      call(api.GET('/api/devices')),
    ]);
    if ('data' in pairing) this.current = pairing.data;
    if ('code' in devices) return;
    this.#devicesById.clear();
    for (const device of devices.data) this.#devicesById.set(device.id, device);
  }

  applyPairing(pairing: Pairing): void {
    this.current = pairing;
  }

  applyDevice({ device }: DeviceChange): void {
    this.#devicesById.set(device.id, device);
  }

  async approve(id: string): Promise<boolean> {
    const outcome = await call(
      api.POST('/api/devices/{deviceId}/approve', { params: { path: { deviceId: id } } }),
    );
    if ('data' in outcome) this.#devicesById.set(id, outcome.data);
    else if (outcome.code === 'not_found') this.#markRevoked(id);
    return 'data' in outcome || outcome.code === 'not_found';
  }

  async revoke(id: string): Promise<boolean> {
    const outcome = await call(
      api.DELETE('/api/devices/{deviceId}', { params: { path: { deviceId: id } } }),
    );
    const isGone = 'data' in outcome || outcome.code === 'not_found';
    if (isGone) this.#markRevoked(id);
    return isGone;
  }

  #markRevoked(id: string): void {
    const device = this.#devicesById.get(id);
    if (device) this.#devicesById.set(id, { ...device, status: 'revoked' });
  }
}

export const pairing = new PairingState();
