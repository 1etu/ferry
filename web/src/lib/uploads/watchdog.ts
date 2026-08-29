export type WakeUps = {
  ontick: (stallThresholdMs: number) => void;
  onhide: () => void;
  ononline: () => void;
};

const watchdogIntervalMs = 5000;
const stallAfterMs = 15000;
const stallAfterReturnMs = 5000;

export function watchUploads({ ontick, onhide, ononline }: WakeUps): () => void {
  const interval = setInterval(() => {
    ontick(stallAfterMs);
  }, watchdogIntervalMs);
  const handleVisibilityChange = () => {
    if (document.visibilityState === 'visible') ontick(stallAfterReturnMs);
    else onhide();
  };
  const handleOnline = () => {
    ononline();
    ontick(stallAfterReturnMs);
  };
  document.addEventListener('visibilitychange', handleVisibilityChange);
  window.addEventListener('online', handleOnline);
  return () => {
    clearInterval(interval);
    document.removeEventListener('visibilitychange', handleVisibilityChange);
    window.removeEventListener('online', handleOnline);
  };
}
