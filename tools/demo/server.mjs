import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { extname, join, normalize } from "node:path";

const types = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript",
  ".mjs": "text/javascript",
  ".css": "text/css",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".json": "application/json",
  ".woff2": "font/woff2",
  ".webmanifest": "application/manifest+json",
};

function inside(directory, path) {
  const resolved = normalize(join(directory, path));
  return resolved.startsWith(normalize(directory)) ? resolved : undefined;
}

export async function serve({ appDirectory, stageDirectory, logoDirectory, bezelPath, layout }) {
  const mounts = [
    ["/stage/", stageDirectory],
    ["/logo/", logoDirectory],
    ["/", appDirectory],
  ];

  async function respond(path) {
    if (path === "/stage/layout.json") return { type: types[".json"], body: JSON.stringify(layout) };
    if (path === "/bezel.png" && bezelPath) return { type: types[".png"], body: await readFile(bezelPath) };
    const [prefix, directory] = mounts.find(([mount]) => path.startsWith(mount));
    const relative = path.slice(prefix.length) || "index.html";
    const file = inside(directory, relative);
    if (!file) return undefined;
    try {
      return { type: types[extname(file)] ?? "application/octet-stream", body: await readFile(file) };
    } catch {
      return prefix === "/" && !extname(relative)
        ? { type: types[".html"], body: await readFile(join(appDirectory, "index.html")) }
        : undefined;
    }
  }

  const server = createServer(async (request, response) => {
    const path = decodeURIComponent(new URL(request.url, "http://localhost").pathname);
    if (/^\/api\/files\/[^/]+\/content$/.test(path)) {
      response.writeHead(204);
      response.end();
      return;
    }
    const found = await respond(path);
    if (!found) {
      response.writeHead(404);
      response.end();
      return;
    }
    response.writeHead(200, { "content-type": found.type, "cache-control": "no-store" });
    response.end(found.body);
  });
  await new Promise((done) => server.listen(0, "127.0.0.1", done));
  return { origin: `http://127.0.0.1:${server.address().port}`, close: () => server.close() };
}
