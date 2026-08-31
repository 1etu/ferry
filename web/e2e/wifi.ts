import { connect, createServer, type Socket } from 'node:net';
import { close, listen } from './ports.js';

export type WifiOptions = { bytesPerSecond?: number };

export type Wifi = {
  url: string;
  setOffline: (isOffline: boolean) => void;
  tamperNextPatch: () => Promise<void>;
};

type Rewrite = (chunk: Buffer) => Buffer;

const patchRequestLine = Buffer.from('PATCH /api/uploads/', 'latin1');
const headerEnd = Buffer.from('\r\n\r\n', 'latin1');
const keptTailBytes = 1024;
const flippedBodyOffset = 4096;

type FlipState =
  { mode: 'seeking'; tail: Buffer } | { mode: 'skipping'; remaining: number } | { mode: 'done' };

function flipped(chunk: Buffer, index: number): Buffer {
  const copy = Buffer.from(chunk);
  copy.writeUInt8((copy.readUInt8(index) ^ 0xff) & 0xff, index);
  return copy;
}

function patchBodyFlipper(claim: () => boolean): Rewrite {
  let state: FlipState = { mode: 'seeking', tail: Buffer.alloc(0) };
  const flipAt = (chunk: Buffer, index: number): Buffer => {
    if (index >= chunk.length) {
      state = { mode: 'skipping', remaining: index - chunk.length };
      return chunk;
    }
    state = { mode: 'done' };
    return claim() ? flipped(chunk, index) : chunk;
  };
  return (chunk) => {
    switch (state.mode) {
      case 'done':
        return chunk;
      case 'skipping':
        return flipAt(chunk, state.remaining);
      case 'seeking': {
        const seen = Buffer.concat([state.tail, chunk]);
        const request = seen.indexOf(patchRequestLine);
        const end = request < 0 ? -1 : seen.indexOf(headerEnd, request);
        if (end >= 0) {
          const bodyStart = end + headerEnd.length - state.tail.length;
          return flipAt(chunk, Math.max(0, bodyStart + flippedBodyOffset));
        }
        state = {
          mode: 'seeking',
          tail: request >= 0 ? seen.subarray(request) : seen.subarray(-keptTailBytes),
        };
        return chunk;
      }
    }
  };
}

function pace(from: Socket, to: Socket, bytesPerSecond: number, rewrite: () => Rewrite): void {
  let sendableAt = Date.now();
  from.on('data', (chunk: Buffer) => {
    to.write(rewrite()(chunk));
    if (!Number.isFinite(bytesPerSecond)) return;
    sendableAt = Math.max(sendableAt, Date.now()) + (chunk.length / bytesPerSecond) * 1000;
    const waitMs = sendableAt - Date.now();
    if (waitMs <= 0) return;
    from.pause();
    setTimeout(() => from.resume(), waitMs);
  });
  from.on('end', () => to.end());
}

export async function openWifi(
  targetPort: number,
  { bytesPerSecond = Infinity }: WifiOptions = {},
): Promise<Wifi & { close: () => Promise<void> }> {
  const sockets = new Set<Socket>();
  let isDown = false;
  let pendingTamper: { claim: () => boolean; armedAt: number } | undefined;
  let armings = 0;
  const unchanged: Rewrite = (chunk) => chunk;
  const track = (socket: Socket) => {
    sockets.add(socket);
    socket.on('close', () => sockets.delete(socket));
    socket.on('error', () => socket.destroy());
  };
  const dropAll = () => {
    for (const socket of sockets) socket.resetAndDestroy();
  };
  const link = createServer((client) => {
    if (isDown) {
      client.resetAndDestroy();
      return;
    }
    const upstream = connect(targetPort, '127.0.0.1');
    track(client);
    track(upstream);
    client.on('close', () => upstream.destroy());
    upstream.on('close', () => client.destroy());
    let flipper: { rewrite: Rewrite; armedAt: number } | undefined;
    pace(client, upstream, bytesPerSecond, () => {
      const tamper = pendingTamper;
      if (tamper === undefined) return unchanged;
      if (flipper?.armedAt !== tamper.armedAt) {
        flipper = { rewrite: patchBodyFlipper(tamper.claim), armedAt: tamper.armedAt };
      }
      return flipper.rewrite;
    });
    upstream.pipe(client);
  });
  const port = await listen(link);
  return {
    url: `http://127.0.0.1:${String(port)}`,
    setOffline: (isOffline) => {
      isDown = isOffline;
      if (isOffline) dropAll();
    },
    tamperNextPatch: () =>
      new Promise((resolve) => {
        armings += 1;
        const armedAt = armings;
        pendingTamper = {
          armedAt,
          claim: () => {
            if (pendingTamper?.armedAt !== armedAt) return false;
            pendingTamper = undefined;
            resolve();
            return true;
          },
        };
      }),
    close: () => {
      dropAll();
      return close(link);
    },
  };
}
