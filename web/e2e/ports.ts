import { createServer, type Server } from 'node:net';

export function listen(server: Server): Promise<number> {
  return new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', () => {
      const address = server.address();
      if (address !== null && typeof address === 'object') resolve(address.port);
      else reject(new Error('no port assigned'));
    });
  });
}

export function close(server: Server): Promise<void> {
  return new Promise((resolve) => {
    server.close(() => {
      resolve();
    });
  });
}

export async function freePort(): Promise<number> {
  const probe = createServer();
  const port = await listen(probe);
  await close(probe);
  return port;
}
