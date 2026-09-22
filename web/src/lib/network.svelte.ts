import { api, call, type ErrorCode, type Network } from '$lib/api/client';

class NetworkState {
  state = $state<Network>();

  async load(): Promise<void> {
    const outcome = await call(api.GET('/api/network'));
    if ('data' in outcome) this.state = outcome.data;
  }

  async allow(): Promise<ErrorCode | undefined> {
    const outcome = await call(api.POST('/api/network/allow'));
    return 'code' in outcome ? outcome.code : undefined;
  }

  applyEvent(network: Network): void {
    this.state = network;
  }
}

export const network = new NetworkState();
