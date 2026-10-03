import { fromBase64Url, toBase64Url } from "../../web/src/lib/seal/frames.ts";
import { clientKeypair, confirmFor, sessionKey, sharedSecret } from "../../web/src/lib/seal/keys.ts";

export const epoch = new Date(2026, 9, 2, 9, 41, 0).getTime();

const pcName = "egetu-pc";
const pairToken = "Tq9xR2mV8kLwP4sZbN7c";
const progressStepMs = 100;
const pcEventStepMs = 250;

function stamp(virtualMs) {
  return new Date(epoch + virtualMs).toISOString();
}

function ease(fraction) {
  const clamped = Math.min(1, Math.max(0, fraction));
  return clamped * clamped * (3 - 2 * clamped) * 0.25 + clamped * 0.75;
}

function failure(status, code) {
  return { status, body: { error: { code, message: code } } };
}

export function createFerry({ origin, script, now, emit }) {
  const serverKeys = clientKeypair();
  let idCounter = 0;
  const nextId = () => `01JAFERRYDEMO${String(++idCounter).padStart(13, "0")}`;
  const transfers = new Map();
  const files = new Map();
  let device;
  let sessions = 0;
  const knownDevice = () => (device && now() >= script.pairAt ? device : undefined);

  const server = { name: pcName, version: "1.0.0", origins: { local: origin, ip: origin } };
  const pairing = {
    qrUrl: `http://${pcName}.local:8080/?pair=${pairToken}`,
    localUrl: `http://${pcName}.local:8080`,
    code: "482913",
    expiresAt: stamp(3600000),
  };

  function publishTransfer(transfer, roles, at) {
    const snapshot = { ...transfer, updatedAt: stamp(at) };
    transfers.set(transfer.id, snapshot);
    for (const role of roles) emit(role, "transfer", snapshot, at);
  }

  function playTransfer(transfer, roles, start, duration, doneAt) {
    for (let at = start + pcEventStepMs; at < start + duration; at += pcEventStepMs) {
      const done = Math.round(transfer.size * ease((at - start) / duration));
      publishTransfer({ ...transfer, done }, roles, at);
    }
    publishTransfer({ ...transfer, done: transfer.size, status: "done" }, roles, doneAt);
  }

  function handshake(body) {
    const clientKey = fromBase64Url(JSON.parse(body).clientKey);
    const shared = sharedSecret(serverKeys.secretKey, clientKey);
    const key = sessionKey(shared, undefined, clientKey, serverKeys.publicKey);
    sessions += 1;
    return {
      status: 201,
      body: {
        sessionId: `S${sessions}`,
        serverKey: toBase64Url(serverKeys.publicKey),
        confirm: toBase64Url(confirmFor(key, serverKeys.publicKey)),
      },
    };
  }

  function pair() {
    device = { id: nextId(), name: "iPhone", status: "pending", createdAt: stamp(script.pairAt) };
    emit("pc", "device", { action: "requested", device }, script.pairAt + 40);
    return { status: 201, body: device, at: script.pairAt };
  }

  function approve() {
    const approvedAt = now();
    device = { ...device, status: "approved", approvedAt: stamp(approvedAt) };
    emit("phone", "device", { action: "approved", device }, approvedAt + script.approveLagMs);
    return { status: 200, body: device };
  }

  function upload(headers) {
    const size = Number(headers["upload-length"]);
    const index = script.uploads.findIndex((planned) => planned.size === size);
    const planned = script.uploads[index];
    if (!planned) return failure(400, "invalid_request");
    const id = nextId();
    const end = planned.start + planned.duration;
    const progress = [];
    for (let at = planned.start; at < end; at += progressStepMs) {
      progress.push([at, ease((at - planned.start) / planned.duration)]);
    }
    const transfer = {
      id,
      deviceId: device.id,
      direction: "in",
      name: planned.name,
      size,
      done: 0,
      status: "active",
      createdAt: stamp(planned.start),
    };
    playTransfer(transfer, ["pc"], planned.start, planned.duration, end + planned.pcDoneLagMs);
    return {
      status: 201,
      headers: { location: `/api/uploads/${id}`, "upload-offset": String(size), "tus-resumable": "1.0.0" },
      at: end,
      progress,
    };
  }

  function pick() {
    const offered = script.offered;
    const at = now() + offered.pickMs;
    const file = { id: nextId(), name: offered.name, size: offered.size, modifiedAt: stamp(at), createdAt: stamp(at) };
    files.set(file.id, file);
    emit("phone", "file", { action: "added", file }, at + offered.phoneLagMs);
    return { status: 200, body: [file], at };
  }

  function startDownload(at) {
    const [file] = [...files.values()];
    const { duration } = script.offered;
    const transfer = {
      id: nextId(),
      deviceId: device.id,
      direction: "out",
      name: file.name,
      size: file.size,
      done: 0,
      status: "active",
      fileId: file.id,
      createdAt: stamp(at),
    };
    publishTransfer({ ...transfer, done: Math.round(file.size * 0.04) }, ["phone", "pc"], at);
    playTransfer(transfer, ["phone", "pc"], at, duration, at + duration);
  }

  function listFor(role) {
    const all = [...transfers.values()];
    return role === "pc" ? all : all.filter((transfer) => transfer.direction === "out");
  }

  const routes = {
    "GET /api/health": () => ({ status: 200, body: { app: "ferry", name: pcName, version: server.version } }),
    "GET /api/pairing": () => ({ status: 200, body: pairing }),
    "GET /api/devices": () => ({ status: 200, body: knownDevice() ? [device] : [] }),
    "GET /api/files": () => ({ status: 200, body: [...files.values()] }),
    "GET /api/settings": () => ({
      status: 200,
      body: { name: pcName, receivedDir: "C:\\Users\\egetu\\Downloads\\Ferry", startAtLogin: true, checkUpdates: true },
    }),
    "GET /api/update": () => ({ status: 200, body: { current: server.version, state: "idle" } }),
    "GET /api/network": () => ({ status: 200, body: { firewall: "allowed", profile: "private" } }),
    "POST /api/pair": pair,
    "POST /api/files/pick": pick,
  };

  function handle(role, request) {
    const target = `${request.method} ${request.path}`;
    if (target === "GET /api/session") {
      if (role === "pc") return { status: 200, body: { role: "owner", server } };
      return { status: 200, body: knownDevice() ? { role: "device", device, server } : { role: "none", server } };
    }
    if (target === "GET /api/transfers") return { status: 200, body: listFor(role) };
    if (target === "POST /api/seal") return handshake(request.body);
    if (target === "POST /api/uploads/") return upload(request.headers ?? {});
    if (/^POST \/api\/devices\/[^/]+\/approve$/.test(target)) return approve();
    const route = routes[target];
    return route ? route() : failure(404, "not_found");
  }

  return { handle, startDownload, pairQuery: `?pair=${pairToken}` };
}
