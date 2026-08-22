const pairParam = 'pair';
const originalsHintKey = 'ferry.originalsHintShown';
const localProbeTimeoutMs = 1500;

export function isStandalone(): boolean {
  return (
    matchMedia('(display-mode: standalone)').matches ||
    ('standalone' in navigator && navigator.standalone === true)
  );
}

export function readPairToken(): string | null {
  return new URLSearchParams(location.search).get(pairParam);
}

export function pairUrlAt(origin: string, token: string): string {
  return `${origin}/?${pairParam}=${encodeURIComponent(token)}${location.hash}`;
}

export function removeLaunchParams(): void {
  const url = new URL(location.href);
  if (!url.searchParams.has(pairParam) && url.hash === '') return;
  url.searchParams.delete(pairParam);
  url.hash = '';
  history.replaceState(history.state, '', url);
}

export async function isOriginReachable(origin: string): Promise<boolean> {
  try {
    await fetch(`${origin}/api/health`, {
      mode: 'no-cors',
      cache: 'no-store',
      signal: AbortSignal.timeout(localProbeTimeoutMs),
    });
    return true;
  } catch {
    return false;
  }
}

export function readOriginalsHintShown(): boolean {
  try {
    return localStorage.getItem(originalsHintKey) !== null;
  } catch {
    return false;
  }
}

export function writeOriginalsHintShown(): void {
  try {
    localStorage.setItem(originalsHintKey, '1');
  } catch {
    return;
  }
}
