import { mkdir, readFile, writeFile } from 'node:fs/promises';
import pngToIco from 'png-to-ico';
import sharp from 'sharp';

const source = new URL('../../assets/logo/icon.svg', import.meta.url);
const publicDir = new URL('../public/', import.meta.url);
const trayDir = new URL('../../internal/tray/', import.meta.url);
const winresDir = new URL('../../tools/winres/', import.meta.url);
const tileColor = '#1C1C1E';

const svg = await readFile(source);

function renderPNG(size, { fullSquare = false } = {}) {
  const image = sharp(svg).resize(size, size);
  return (fullSquare ? image.flatten({ background: tileColor }) : image).png().toBuffer();
}

async function renderICO(sizes) {
  return pngToIco(await Promise.all(sizes.map((size) => renderPNG(size))));
}

await mkdir(winresDir, { recursive: true });

await Promise.all([
  writeFile(new URL('favicon.svg', publicDir), svg),
  writeFile(new URL('apple-touch-icon.png', publicDir), await renderPNG(180, { fullSquare: true })),
  writeFile(new URL('icon-192.png', publicDir), await renderPNG(192)),
  writeFile(new URL('icon-512.png', publicDir), await renderPNG(512)),
  writeFile(new URL('icon.ico', trayDir), await renderICO([16, 24, 32, 48])),
  writeFile(new URL('icon.ico', winresDir), await renderICO([16, 24, 32, 48, 64, 128, 256])),
]);
