import { api, call, type ErrorCode, type Settings, type SettingsPatch } from '$lib/api/client';

class OwnerSettings {
  current = $state<Settings>();
  canPickReceivedDir = $state(true);

  async load(): Promise<void> {
    const outcome = await call(api.GET('/api/settings'));
    if ('data' in outcome) this.current = outcome.data;
  }

  async patch(patch: SettingsPatch): Promise<ErrorCode | undefined> {
    const outcome = await call(api.PATCH('/api/settings', { body: patch }));
    if ('code' in outcome) return outcome.code;
    this.current = outcome.data;
    return undefined;
  }

  async pickReceivedDir(): Promise<void> {
    const outcome = await call(api.POST('/api/settings/received-dir/pick'));
    if ('code' in outcome) {
      if (outcome.code === 'unsupported') this.canPickReceivedDir = false;
      return;
    }
    this.current = outcome.data;
  }
}

export const settings = new OwnerSettings();
