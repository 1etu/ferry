import { api, call, type ErrorCode, type UpdateStatus } from '$lib/api/client';

type StatusRequest = Promise<{ data?: UpdateStatus; error?: unknown; response: Response }>;

class Updater {
  status = $state<UpdateStatus>();

  #eventsApplied = 0;

  async load(): Promise<void> {
    await this.#applyUnlessOutdated(api.GET('/api/update'));
  }

  async check(): Promise<void> {
    await this.#applyUnlessOutdated(api.POST('/api/update/check'));
  }

  async apply(): Promise<ErrorCode | undefined> {
    const outcome = await call(api.POST('/api/update/apply'));
    return 'code' in outcome ? outcome.code : undefined;
  }

  applyEvent(status: UpdateStatus): void {
    this.#eventsApplied += 1;
    this.status = status;
  }

  async #applyUnlessOutdated(request: StatusRequest): Promise<void> {
    const eventsBefore = this.#eventsApplied;
    const outcome = await call(request);
    if ('data' in outcome && eventsBefore === this.#eventsApplied) this.status = outcome.data;
  }
}

export const update = new Updater();
