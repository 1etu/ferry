import { SvelteMap } from 'svelte/reactivity';
import { api, call, type Transfer } from '$lib/api/client';
import { seal } from '$lib/seal/session.svelte';

export function byNewestId(a: { id: string }, b: { id: string }): number {
  if (a.id === b.id) return 0;
  return a.id < b.id ? 1 : -1;
}

function isStale(incoming: Transfer, known: Transfer): boolean {
  if (known.status !== 'active' && incoming.status === 'active') return true;
  return Date.parse(incoming.updatedAt) < Date.parse(known.updatedAt);
}

function withOpenName(transfer: Transfer): Transfer {
  const name = seal.openName(transfer.name);
  return name === undefined ? transfer : { ...transfer, name };
}

class TransferList {
  #byId = new SvelteMap<string, Transfer>();

  readonly items = $derived([...this.#byId.values()].sort(byNewestId));

  async load(): Promise<void> {
    const outcome = await call(api.GET('/api/transfers'));
    if ('code' in outcome) return;
    const fetched = outcome.data.map((transfer) => this.#newerOf(withOpenName(transfer)));
    this.#byId.clear();
    for (const transfer of fetched) this.#byId.set(transfer.id, transfer);
  }

  apply(transfer: Transfer): Transfer {
    const kept = this.#newerOf(withOpenName(transfer));
    this.#byId.set(kept.id, kept);
    return kept;
  }

  clear(): void {
    this.#byId.clear();
  }

  async clearHistory(): Promise<void> {
    const outcome = await call(api.DELETE('/api/transfers'));
    if ('code' in outcome) return;
    for (const transfer of [...this.#byId.values()]) {
      if (transfer.status !== 'active') this.#byId.delete(transfer.id);
    }
  }

  async remove(id: string): Promise<void> {
    const outcome = await call(
      api.DELETE('/api/transfers/{transferId}', { params: { path: { transferId: id } } }),
    );
    if ('code' in outcome) return;
    if (this.#byId.get(id)?.status !== 'active') this.#byId.delete(id);
  }

  #newerOf(incoming: Transfer): Transfer {
    const known = this.#byId.get(incoming.id);
    return known !== undefined && isStale(incoming, known) ? known : incoming;
  }
}

export const transfers = new TransferList();
