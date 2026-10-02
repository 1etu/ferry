(() => {
  const isAppPage = globalThis !== globalThis.top || new URLSearchParams(location.search).has("role");
  if (!isAppPage || globalThis.__mock) return;

  const realFetch = fetch.bind(globalThis);
  const sources = new Set();
  let pending = 0;

  async function ask(request) {
    pending += 1;
    try {
      return await globalThis.__ferryApi(request);
    } finally {
      pending -= 1;
    }
  }

  function wait(ms) {
    return new Promise((resolve) => setTimeout(resolve, Math.max(0, ms)));
  }

  function untilVirtual(at) {
    return wait(at - performance.now());
  }

  globalThis.fetch = async (input, init) => {
    const request = new Request(input, init);
    const url = new URL(request.url);
    if (!url.pathname.startsWith("/api/")) return realFetch(input, init);
    const body = request.method === "GET" || request.method === "HEAD" ? "" : await request.text();
    const reply = await ask({
      method: request.method,
      path: url.pathname,
      body,
      seal: request.headers.get("X-Ferry-Seal"),
    });
    if (reply.at !== undefined) await untilVirtual(reply.at);
    const hasBody = reply.body !== undefined;
    return new Response(hasBody ? JSON.stringify(reply.body) : null, {
      status: reply.status,
      headers: hasBody ? { "Content-Type": "application/json" } : {},
    });
  };

  class DemoEventSource extends EventTarget {
    static CONNECTING = 0;
    static OPEN = 1;
    static CLOSED = 2;

    constructor(url) {
      super();
      this.url = String(url);
      this.readyState = DemoEventSource.CONNECTING;
      sources.add(this);
      setTimeout(() => {
        if (this.readyState !== DemoEventSource.CONNECTING) return;
        this.readyState = DemoEventSource.OPEN;
        this.dispatchEvent(new Event("open"));
      }, 20);
    }

    close() {
      this.readyState = DemoEventSource.CLOSED;
      sources.delete(this);
    }
  }

  class DemoUploadRequest {
    constructor() {
      this.upload = { onprogress: null };
      this.status = 0;
      this.responseText = "";
      this.onload = null;
      this.onerror = null;
      this.requestHeaders = {};
      this.responseHeaders = {};
      this.timers = [];
    }

    open(method, url) {
      this.method = method;
      this.url = new URL(url, location.href);
    }

    setRequestHeader(name, value) {
      this.requestHeaders[name.toLowerCase()] = String(value);
    }

    getResponseHeader(name) {
      return this.responseHeaders[name.toLowerCase()] ?? null;
    }

    async send(body) {
      const total = body?.size ?? 0;
      const reply = await ask({
        method: this.method,
        path: this.url.pathname,
        body: "",
        headers: this.requestHeaders,
        seal: this.requestHeaders["x-ferry-seal"] ?? null,
      });
      for (const [at, fraction] of reply.progress ?? []) {
        const loaded = Math.round(total * fraction);
        this.later(at, () => this.upload.onprogress?.({ lengthComputable: true, loaded, total }));
      }
      this.later(reply.at ?? performance.now(), () => {
        this.status = reply.status;
        this.responseHeaders = reply.headers ?? {};
        this.onload?.();
      });
    }

    later(at, action) {
      this.timers.push(setTimeout(action, Math.max(0, at - performance.now())));
    }

    abort() {
      for (const timer of this.timers) clearTimeout(timer);
      this.timers = [];
    }
  }

  globalThis.EventSource = DemoEventSource;
  globalThis.XMLHttpRequest = DemoUploadRequest;

  globalThis.__mock = {
    pending: () => pending,
    emit(kind, payload) {
      const data = JSON.stringify(payload);
      for (const source of sources) {
        if (source.readyState === DemoEventSource.OPEN) {
          source.dispatchEvent(new MessageEvent(kind, { data }));
        }
      }
    },
  };
})();
