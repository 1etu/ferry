const pickerTempName = /^(?:tempImage\w*|image)\.([a-z0-9]+)$/i;

type PickedFile = { name: string; type: string };

function pad(value: number): string {
  return String(value).padStart(2, '0');
}

function formatTimestamp(time: Date): string {
  const date = `${String(time.getFullYear())}-${pad(time.getMonth() + 1)}-${pad(time.getDate())}`;
  const clock = `${pad(time.getHours())}.${pad(time.getMinutes())}.${pad(time.getSeconds())}`;
  return `${date} ${clock}`;
}

export function displayName(file: PickedFile, now: Date): string {
  const extension = pickerTempName.exec(file.name)?.[1];
  if (extension === undefined) return file.name;
  const kind = file.type.startsWith('video/') ? 'Video' : 'Photo';
  return `${kind} ${formatTimestamp(now)}.${extension.toLowerCase()}`;
}
