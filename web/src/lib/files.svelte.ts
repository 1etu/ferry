import { SvelteMap } from 'svelte/reactivity';
import { api, call, type OfferedFile } from '$lib/api/client';
import { type FileChange } from '$lib/api/events';
import { seal } from '$lib/seal/session.svelte';
import { byNewestId } from '$lib/transfers.svelte';

function withOpenName(file: OfferedFile): OfferedFile {
  const name = seal.openName(file.name);
  return name === undefined ? file : { ...file, name };
}

class OfferedFileList {
  #byId = new SvelteMap<string, OfferedFile>();

  readonly items = $derived([...this.#byId.values()].sort(byNewestId));
  canPick = $state(true);

  async load(): Promise<void> {
    const outcome = await call(api.GET('/api/files'));
    if ('code' in outcome) return;
    this.#byId.clear();
    for (const file of outcome.data) this.#byId.set(file.id, withOpenName(file));
  }

  apply({ action, file }: FileChange): void {
    if (action === 'added') this.#byId.set(file.id, withOpenName(file));
    else this.#byId.delete(file.id);
  }

  clear(): void {
    this.#byId.clear();
  }

  async pick(): Promise<void> {
    const outcome = await call(api.POST('/api/files/pick'));
    if ('code' in outcome) {
      if (outcome.code === 'unsupported') this.canPick = false;
      return;
    }
    for (const file of outcome.data) this.#byId.set(file.id, file);
  }

  async remove(id: string): Promise<void> {
    const outcome = await call(
      api.DELETE('/api/files/{fileId}', { params: { path: { fileId: id } } }),
    );
    if ('code' in outcome) return;
    this.#byId.delete(id);
  }
}

export const files = new OfferedFileList();
