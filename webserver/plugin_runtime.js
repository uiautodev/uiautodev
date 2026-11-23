let _reqId = 0;
const _pending = {};
const _streams = {};
const _screenTapListeners = new Set();
const _modeListeners = new Set();

const SCREEN_MODES = ["control", "readonly", "recording"];

window.addEventListener("message", (e) => {
  // Only accept messages from the host window embedding this plugin iframe.
  if (e.source !== parent) return;
  if (e.data?.type === "plugin-api-result") {
    const { id, result, error } = e.data;
    const p = _pending[id];
    if (p) {
      if (error) p.reject(new Error(error));
      else p.resolve(result);
      delete _pending[id];
    }
  } else if (e.data?.type === "plugin-api-stream") {
    const h = _streams[e.data.id];
    if (!h) return;
    if (e.data.error) {
      h.error = e.data.error;
      h.done = true;
    } else if (e.data.done) {
      h.done = true;
    } else if (typeof e.data.chunk === "string") {
      h.buffer += e.data.chunk;
    }
    h.notify?.();
  } else if (e.data?.type === "plugin-theme") {
    const theme = e.data?.theme;
    if (theme !== "dark" && theme !== "light") return;
    document.documentElement.classList.toggle("dark", theme === "dark");
  } else if (e.data?.type === "plugin-screen-tap") {
    const event = e.data?.event;
    if (
      !event ||
      ![event.x, event.y, event.screenWidth, event.screenHeight].every(Number.isFinite) ||
      event.screenWidth <= 0 ||
      event.screenHeight <= 0
    ) {
      return;
    }
    const tapEvent = {
      x: event.x,
      y: event.y,
      screenWidth: event.screenWidth,
      screenHeight: event.screenHeight,
      timestamp: Number.isFinite(event.timestamp) ? event.timestamp : Date.now(),
    };
    for (const listener of _screenTapListeners) {
      try {
        listener(tapEvent);
      } catch (err) {
        console.error("[plugin] onScreenTap listener error:", err);
      }
    }
  } else if (e.data?.type === "plugin-mode-change") {
    const mode = e.data?.mode;
    if (!SCREEN_MODES.includes(mode)) return;
    for (const listener of _modeListeners) {
      try {
        listener(mode);
      } catch (err) {
        console.error("[plugin] onModeChange listener error:", err);
      }
    }
  }
});

function _call(method, ...args) {
  return new Promise((resolve, reject) => {
    const id = ++_reqId;
    _pending[id] = { resolve, reject };
    parent.postMessage({ type: "plugin-api", id, method, args }, "*");
  });
}

// Extract the next complete SSE event ("data:" payloads) from the stream buffer,
// or null when there is not yet a full event. Returns "[DONE]" for the sentinel.
function _extractEvent(state) {
  state.buffer = state.buffer.replace(/\r\n/g, "\n");
  const idx = state.buffer.indexOf("\n\n");
  if (idx === -1) return null;
  const raw = state.buffer.slice(0, idx);
  state.buffer = state.buffer.slice(idx + 2);
  const dataLines = [];
  for (const line of raw.split("\n")) {
    if (line.startsWith("data:")) dataLines.push(line.slice(5).replace(/^ /, ""));
  }
  const data = dataLines.join("\n");
  if (data === "[DONE]") return "[DONE]";
  try {
    return JSON.parse(data);
  } catch {
    return null;
  }
}

function _streamNext(id, state) {
  const ev = _extractEvent(state);
  if (ev === "[DONE]") {
    delete _streams[id];
    return Promise.resolve({ value: undefined, done: true });
  }
  if (ev !== null) {
    return Promise.resolve({ value: ev, done: false });
  }
  if (state.error) {
    delete _streams[id];
    return Promise.reject(new Error(state.error));
  }
  if (state.done) {
    delete _streams[id];
    return Promise.resolve({ value: undefined, done: true });
  }
  return new Promise((resolve, reject) => {
    state.notify = () => {
      state.notify = null;
      _streamNext(id, state).then(resolve, reject);
    };
  });
}

// Opens a streaming request and resolves to an async-iterable of parsed chunks.
function _stream(method, args) {
  return new Promise((resolve) => {
    const id = ++_reqId;
    const state = { buffer: "", done: false, error: null, notify: null };
    _streams[id] = state;
    parent.postMessage({ type: "plugin-api", id, method, args: [args], stream: true }, "*");
    resolve({
      [Symbol.asyncIterator]() {
        return { next: () => _streamNext(id, state) };
      },
    });
  });
}

async function __proxyFetch(url, init) {
  const parsed = new URL(url, window.location.href);

  // Standard fetch behavior for same-origin (including relative) URLs.
  if (parsed.origin === window.location.origin) {
    return fetch(parsed.pathname + parsed.search + parsed.hash, init);
  }

  const targetHost = parsed.origin;
  const proxyUrl = `/proxy/request${parsed.pathname}${parsed.search}`;
  const headers = new Headers(init?.headers);
  headers.set("X-Target-Host", targetHost);
  headers.set("X-Plugin-ID", window.__plugin_id__);
  return fetch(proxyUrl, { ...init, headers });
}

const _params = new URLSearchParams(window.location.search);

const _theme = _params.get("theme");
if (_theme === "dark" || _theme === "light") {
  document.documentElement.classList.toggle("dark", _theme === "dark");
}

window.$u = Object.freeze({
  get deviceId() {
    return _params.get("deviceId") || "";
  },

  get plugin() {
    return window.__plugin__;
  },

  shell(cmd) {
    const opts = typeof cmd === "string" ? { command: cmd } : cmd;
    return _call("shell", opts);
  },

  screenshotAsBase64() {
    return _call("screenshotAsBase64");
  },

  setScreenImage(blob) {
    return _call("setScreenImage", blob);
  },

  inputText(text) {
    return _call("inputText", text);
  },

  pressKey(key, metaState = 0) {
    return _call("pressKey", { key, metaState });
  },

  tap(x, y, screenWidth, screenHeight) {
    return _call("tap", { x, y, screenWidth, screenHeight });
  },

  appList(scope = "user") {
    return _call("appList", scope);
  },

  appStart(appId, activity) {
    return _call("appStart", appId, activity);
  },

  appStop(appId) {
    return _call("appStop", appId);
  },

  appCurrent() {
    return _call("appCurrent");
  },

  /**
   * Subscribes to screen taps captured by the host while the screen mode is
   * "recording" (see setMode). Returns an unsubscribe function.
   */
  onScreenTap(callback) {
    if (typeof callback !== "function") {
      throw new TypeError("onScreenTap expects a function");
    }
    _screenTapListeners.add(callback);
    return () => _screenTapListeners.delete(callback);
  },

  /**
   * Sets the current screen mode: "control" (touch enabled), "readonly"
   * (touch disabled), or "recording" (taps are captured, not injected).
   */
  setMode(mode) {
    if (!SCREEN_MODES.includes(mode)) {
      return Promise.reject(new TypeError(`setMode expects one of ${SCREEN_MODES.join(", ")}`));
    }
    return _call("setMode", mode);
  },

  /**
   * Subscribes to screen-mode changes. Returns an unsubscribe function.
   */
  onModeChange(callback) {
    if (typeof callback !== "function") {
      throw new TypeError("onModeChange expects a function");
    }
    _modeListeners.add(callback);
    return () => _modeListeners.delete(callback);
  },

  /**
   * Calls the configured OpenAI-compatible provider.
   * Mirrors the OpenAI SDK: params is a chat create body ({messages, model?, ...}).
   * With stream:true resolves to an async-iterable of chunk objects.
   */
  openai(params) {
    const stream = typeof params === "object" && params?.stream === true;
    return stream ? _stream("openai", params) : _call("openai", params);
  },

  /**
   * Calls the configured Jev (TypeSafe System One) decision endpoint.
   * params is a decision body ({state, questions, model?}); resolves to the
   * raw Jev response ({id, model, answers, usage, ...}). Jev has no streaming.
   */
  jev(params) {
    return _call("jev", params);
  },

  fetch: __proxyFetch,
});

let __lastHtmlHeight = 0;
let __lastBodyHeight = 0;
let __lastHeight = 0;
function __pluginResize() {
  let newHeight = Math.max(document.body.scrollHeight, document.documentElement.scrollHeight);
  if (__lastBodyHeight !== document.body.scrollHeight) {
    /* body 变化但是 html 没有变化：需要先调整 iframe 高度，触发 html 高度更新 */
    if (__lastHtmlHeight === document.documentElement.scrollHeight) {
      newHeight = document.body.scrollHeight;
    }
  }
  __lastHtmlHeight = document.documentElement.scrollHeight;
  __lastBodyHeight = document.body.scrollHeight;
  if (newHeight === __lastHeight) return;
  __lastHeight = newHeight;
  parent.postMessage({ type: "plugin-resize", height: newHeight }, "*");
};
function __initResize() {
  if (!document.body) return requestAnimationFrame(__initResize);
  const ro = new ResizeObserver(__pluginResize)
  ro.observe(document.body);
  ro.observe(document.documentElement);
  __pluginResize();
}
__initResize();
