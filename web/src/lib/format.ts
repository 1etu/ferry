const units = ['KB', 'MB', 'GB'] as const;

function roundForDisplay(value: number): number {
  return value < 10 ? Math.round(value * 10) / 10 : Math.round(value);
}

export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B';
  if (bytes < 1000) return `${String(Math.round(bytes))} B`;
  let value = bytes / 1000;
  let unitIndex = 0;
  while (unitIndex < units.length - 1 && roundForDisplay(value) >= 1000) {
    value /= 1000;
    unitIndex += 1;
  }
  return `${String(roundForDisplay(value))} ${units[unitIndex] ?? 'GB'}`;
}

export function formatPercent(fraction: number): string {
  const clamped = Number.isFinite(fraction) ? Math.min(1, Math.max(0, fraction)) : 0;
  return `${String(Math.floor(clamped * 100))}%`;
}
