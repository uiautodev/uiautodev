import axios from "axios";
import { isWailsEnv } from "$lib/wails-env";
import type {
  APIDevice,
  UINode,
  ServerConfig,
  Plugin,
  OpenAIConfig,
  JevConfig,
  MarketList,
  MarketSource,
  DeviceMeta,
  DeviceEncoderList,
  DeviceAppList,
  CurrentApp,
  DeviceClipboard,
  AppScope,
  VersionCheckResult,
  ClusterSlave,
  ClusterSlaveCreate,
  ClusterSlaveTokenResult,
  ClusterSlaveUpdate,
  UpdateInfo,
  UpdateProgress,
  Me,
  Occupancy,
  DeviceAccessMode,
  AccessConfig,
  AccessConfigResponse,
} from "./schemas";

export type DumpBackend = "yadb" | "uiautomator";

/**
 * Fetch UI tree (main screen) for a device by id.
 * `backend` selects the Android dump method; omit it to use the default (yadb).
 * Returns a `UINode` or `null` according to the API spec.
 */
export async function getUiTree(deviceId: string, backend?: DumpBackend): Promise<UINode | null> {
  const res = await api.get<UINode | null>(`/api/v1/devices/${deviceId}/uitree`, {
    params: backend ? { backend } : {},
  });
  return res.data;
}

/**
 * Subscribe to device list changes over a WebSocket. The backend re-reads the
 * list and only pushes when it changed, so callers no longer need to poll.
 * Returns an unsubscribe function.
 */
export function subscribeDeviceList(
  platform: string,
  handlers: {
    onDevices: (devices: APIDevice[]) => void;
    onError?: (message: string) => void;
  },
): () => void {
  let stopped = false;
  let socket: WebSocket | null = null;
  let retryTimer: ReturnType<typeof setTimeout> | null = null;
  let retries = 0;

  const scheduleReconnect = () => {
    if (stopped) return;
    const delay = Math.min(1000 * 2 ** retries++, 10000);
    retryTimer = setTimeout(connect, delay);
  };

  const connect = () => {
    if (stopped) return;
    socket = new WebSocket(`${baseWSURL}/stream/devices?platform=${encodeURIComponent(platform)}`);
    socket.onopen = () => {
      retries = 0;
    };
    socket.onmessage = (event) => {
      try {
        const message = JSON.parse(event.data as string) as { devices?: APIDevice[]; error?: string };
        if (message.error) {
          handlers.onError?.(message.error);
          return;
        }
        handlers.onDevices(message.devices ?? []);
      } catch (error) {
        console.error("invalid device list message", error);
      }
    };
    socket.onclose = () => {
      socket = null;
      scheduleReconnect();
    };
    socket.onerror = () => {
      socket?.close();
    };
  };

  connect();
  return () => {
    stopped = true;
    if (retryTimer) clearTimeout(retryTimer);
    socket?.close();
  };
}

/** Terminal control frames exchanged as JSON text frames; binary frames are PTY data. */
export type TerminalMessageType = "resize" | "exit" | "error";

export interface TerminalMessage {
  type: TerminalMessageType;
  rows?: number;
  cols?: number;
  code?: number;
  message?: string;
}

/** WebSocket URL of the interactive terminal session for a local Android device. */
export function getTerminalURL(deviceId: string, rows: number, cols: number): string {
  const params = new URLSearchParams({ rows: String(rows), cols: String(cols) });
  return `${baseWSURL}/stream/devices/${encodeURIComponent(deviceId)}/terminal?${params.toString()}`;
}

/** Tell the remote PTY about a new window size. */
export function sendTerminalResize(socket: WebSocket, rows: number, cols: number): void {
  if (socket.readyState !== WebSocket.OPEN) return;
  const message: TerminalMessage = { type: "resize", rows, cols };
  socket.send(JSON.stringify(message));
}

export async function getScreenshot(deviceId: string): Promise<Blob> {
  const res = await api.get<string>(`/api/v1/devices/${deviceId}/screenshot`);
  const base64Data = res.data;
  const binaryString = atob(base64Data);
  const bytes = new Uint8Array(binaryString.length);
  for (let i = 0; i < binaryString.length; i++) {
    bytes[i] = binaryString.charCodeAt(i);
  }
  return new Blob([bytes], { type: "image/png" });
}

/**
 * Query UI tree using XPath and return matching nodes.
 */
export async function queryXPath(tree: UINode, xpath: string): Promise<UINode[]> {
  const res = await api.post<UINode[]>("/api/v1/ui/xpath_query", { uitree: tree, xpath });
  return res.data;
}

/**
 * Convert a UI tree to uiautomator-style XML.
 */
export async function treeToXML(tree: UINode): Promise<string> {
  const res = await api.post<string>("/api/v1/ui/xml", { uitree: tree });
  return res.data;
}

export async function getDeviceInfo(deviceId: string): Promise<APIDevice> {
  const res = await api.get<APIDevice>(`/api/v1/devices/${deviceId}`);
  return res.data;
}

export async function setDeviceMeta(deviceId: string, meta: DeviceMeta): Promise<DeviceMeta> {
  const res = await api.put<DeviceMeta>(`/api/v1/devices/${deviceId}/meta`, meta);
  return res.data;
}

export async function deleteDeviceMeta(deviceId: string): Promise<void> {
  await api.delete(`/api/v1/devices/${deviceId}/meta`);
}

/**
 * Press a key on the device. metaState is a bitmask of modifier keys
 * (AndroidKeyEventMeta), used for combination key events on HarmonyOS.
 */
export async function pressKey(deviceId: string, key: string, metaState = 0): Promise<void> {
  await api.post(`/api/v1/devices/${deviceId}/actions/press_key`, { key, metaState });
}

export async function inputText(deviceId: string, text: string): Promise<void> {
  await api.post(`/api/v1/devices/${deviceId}/actions/input_text`, { text });
}

export async function clearText(deviceId: string): Promise<void> {
  await api.post(`/api/v1/devices/${deviceId}/actions/clear_text`);
}

/** Read the device clipboard; text or an image according to `kind`. */
export async function getClipboard(deviceId: string): Promise<DeviceClipboard> {
  const res = await api.get<DeviceClipboard>(`/api/v1/devices/${encodeURIComponent(deviceId)}/clipboard`);
  return res.data;
}

/** Replace the device clipboard with text or an image. */
export async function setClipboard(deviceId: string, content: DeviceClipboard): Promise<void> {
  await api.put(`/api/v1/devices/${encodeURIComponent(deviceId)}/clipboard`, content);
}

/**
 * A tap position in the streamed video coordinate space, matching
 * screenWidth/screenHeight (device resolution, or cropped dimensions).
 */
export interface TapPoint {
  x: number;
  y: number;
  screenWidth: number;
  screenHeight: number;
}

export async function tapDevice(deviceId: string, point: TapPoint): Promise<void> {
  await api.post(`/api/v1/devices/${deviceId}/actions/tap`, point);
}

export interface WindowSize {
  width: number;
  height: number;
}

export async function getWindowSize(deviceId: string): Promise<WindowSize> {
  const res = await api.get<WindowSize>(`/api/v1/devices/${deviceId}/window/size`);
  return res.data;
}

export async function getDeviceEncoders(deviceId: string): Promise<DeviceEncoderList> {
  const res = await api.get<DeviceEncoderList>(`/api/v1/devices/${deviceId}/encoders`);
  return res.data;
}

function deviceAppPath(deviceId: string, appId: string): string {
  return `/api/v1/devices/${encodeURIComponent(deviceId)}/apps/${encodeURIComponent(appId)}`;
}

export async function listDeviceApps(deviceId: string, scope: AppScope = "user"): Promise<string[]> {
  const res = await api.get<DeviceAppList>(`/api/v1/devices/${encodeURIComponent(deviceId)}/apps`, {
    params: { scope },
  });
  return res.data.apps ?? [];
}

export async function getCurrentDeviceApp(deviceId: string): Promise<CurrentApp> {
  const res = await api.get<CurrentApp>(`/api/v1/devices/${encodeURIComponent(deviceId)}/apps/current`);
  return res.data;
}

export async function startDeviceApp(deviceId: string, appId: string, activity?: string): Promise<CurrentApp> {
  const res = await api.post<CurrentApp>(`${deviceAppPath(deviceId, appId)}/start`, {
    activity: activity ?? "",
  });
  return res.data;
}

export async function stopDeviceApp(deviceId: string, appId: string): Promise<void> {
  await api.post(`${deviceAppPath(deviceId, appId)}/stop`);
}

export async function getServerConfig(): Promise<ServerConfig> {
  const response = await api.get<ServerConfig>("/api/v1/config");
  return response.data;
}

export async function getLatestVersion(): Promise<VersionCheckResult> {
  const response = await api.get<VersionCheckResult>("/api/v1/version/latest");
  return response.data;
}

export async function getUpdateInfo(): Promise<UpdateInfo> {
  const response = await api.get<UpdateInfo>("/api/v1/update");
  return response.data;
}

export async function startUpdate(): Promise<void> {
  await api.post("/api/v1/update");
}

export async function getUpdateProgress(): Promise<UpdateProgress> {
  const response = await api.get<UpdateProgress>("/api/v1/update/status");
  return response.data;
}

export async function setLocalePreference(locale: string): Promise<void> {
  await api.put("/api/v1/locale", { locale });
}

// --- Management auth ---

export interface ManagementAuthStatus {
  initialized: boolean;
  suggested_password?: string;
  auth_required?: boolean;
  config_file?: string;
  plugin_dir?: string;
}

export async function getManagementAuthStatus(): Promise<ManagementAuthStatus> {
  const res = await api.get<ManagementAuthStatus>("/api/v1/plugins/auth");
  return res.data;
}

export async function setupManagementAuth(password: string): Promise<string> {
  const res = await api.post<{ token: string }>("/api/v1/plugins/auth/setup", { password });
  return res.data.token;
}

export async function loginManagementAuth(password: string): Promise<string> {
  const res = await api.post<{ token: string }>("/api/v1/plugins/auth", { password });
  return res.data.token;
}

export async function listPlugins(enabled?: "all" | "enabled"): Promise<Plugin[]> {
  const res = await api.get<Plugin[]>("/api/v1/plugins", {
    params: enabled ? { enabled } : {},
  });
  return res.data ?? [];
}

export async function setPluginEnabled(token: string, id: string, enabled: boolean): Promise<void> {
  await api.put(
    `/api/v1/plugins/${id}`,
    { enabled },
    {
      headers: { Authorization: `Bearer ${token}` },
    },
  );
}

export async function installPlugin(token: string, file: File): Promise<void> {
  const formData = new FormData();
  formData.append("file", file);
  await api.post("/api/v1/plugins", formData, {
    headers: { Authorization: `Bearer ${token}` },
  });
}

export async function uninstallPlugin(token: string, id: string): Promise<void> {
  await api.delete(`/api/v1/plugins/${id}`, {
    headers: { Authorization: `Bearer ${token}` },
  });
}

// --- Plugin market ---

export interface MarketQuery {
  q?: string;
  tag?: string;
  platform?: string;
  page?: number;
  page_size?: number;
}

export async function listMarketPlugins(token: string, params: MarketQuery = {}): Promise<MarketList> {
  const res = await api.get<MarketList>("/api/v1/market/plugins", {
    params,
    headers: { Authorization: `Bearer ${token}` },
  });
  return res.data;
}

export async function installMarketPlugin(token: string, id: string, version?: string): Promise<void> {
  await api.post(`/api/v1/market/plugins/${id}/install`, version ? { version } : {}, {
    headers: { Authorization: `Bearer ${token}` },
  });
}

export async function getMarketSource(token: string): Promise<MarketSource> {
  const res = await api.get<MarketSource>("/api/v1/market/source", {
    headers: { Authorization: `Bearer ${token}` },
  });
  return res.data;
}

export async function setMarketSource(token: string, url: string): Promise<MarketSource> {
  const res = await api.put<MarketSource>(
    "/api/v1/market/source",
    { url },
    { headers: { Authorization: `Bearer ${token}` } },
  );
  return res.data;
}

// --- Cluster ---

export async function listClusterSlaves(token: string): Promise<ClusterSlave[]> {
  const res = await api.get<ClusterSlave[]>("/api/v1/cluster/slaves", {
    headers: { Authorization: `Bearer ${token}` },
  });
  return res.data ?? [];
}

export async function createClusterSlave(token: string, input: ClusterSlaveCreate): Promise<ClusterSlaveTokenResult> {
  const res = await api.post<ClusterSlaveTokenResult>("/api/v1/cluster/slaves", input, {
    headers: { Authorization: `Bearer ${token}` },
  });
  return res.data;
}

export async function updateClusterSlave(
  token: string,
  id: string,
  updates: ClusterSlaveUpdate,
): Promise<ClusterSlave> {
  const res = await api.patch<ClusterSlave>(`/api/v1/cluster/slaves/${id}`, updates, {
    headers: { Authorization: `Bearer ${token}` },
  });
  return res.data;
}

export async function deleteClusterSlave(token: string, id: string): Promise<void> {
  await api.delete(`/api/v1/cluster/slaves/${id}`, {
    headers: { Authorization: `Bearer ${token}` },
  });
}

export async function rotateClusterSlaveToken(token: string, id: string): Promise<ClusterSlaveTokenResult> {
  const res = await api.post<ClusterSlaveTokenResult>(
    `/api/v1/cluster/slaves/${id}/rotate-token`,
    {},
    { headers: { Authorization: `Bearer ${token}` } },
  );
  return res.data;
}

// --- MCP ---

export async function getMCPToken(managementToken = ""): Promise<string> {
  const res = await api.get<{ token: string }>(
    "/api/v1/mcp/token",
    managementToken ? { headers: { Authorization: `Bearer ${managementToken}` } } : {},
  );
  return res.data.token;
}

export async function setMCPToken(token: string, mcpToken: string): Promise<string> {
  const res = await api.put<{ token: string }>(
    "/api/v1/mcp/token",
    { token: mcpToken },
    { headers: { Authorization: `Bearer ${token}` } },
  );
  return res.data.token;
}

// --- OpenAI provider config ---
export async function getOpenAIConfig(token: string): Promise<OpenAIConfig> {
  const res = await api.get<OpenAIConfig>("/api/v1/openai/config", {
    headers: { Authorization: `Bearer ${token}` },
  });
  return res.data;
}

export async function setOpenAIConfig(token: string, config: OpenAIConfig): Promise<OpenAIConfig> {
  const res = await api.put<OpenAIConfig>("/api/v1/openai/config", config, {
    headers: { Authorization: `Bearer ${token}` },
  });
  return res.data;
}

export async function getOpenAIModels(token: string, baseURL: string, apiKey: string, proxy = ""): Promise<string[]> {
  const res = await api.post<string[]>(
    "/api/v1/openai/models",
    { base_url: baseURL, api_key: apiKey, proxy },
    {
      headers: { Authorization: `Bearer ${token}` },
    },
  );
  return res.data;
}

/**
 * Proxy an OpenAI chat completion through the configured provider so plugins
 * can call it without the api_key. Uses a raw fetch (not the `api` client so the
 * OpenAI-shaped response is returned as-is); streaming is handled by the
 * dedicated SSE endpoint in PluginIframe.
 */
export async function postOpenAICompletion(params: Record<string, any>): Promise<any> {
  const res = await fetch(`${baseURL}/api/v1/openai/chat/completions`, {
    method: "POST",
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: JSON.stringify(params),
  });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`openai request failed (HTTP ${res.status}): ${text}`);
  }
  return res.json();
}

// --- Jev provider config ---
export async function getJevConfig(token: string): Promise<JevConfig> {
  const res = await api.get<JevConfig>("/api/v1/jev/config", {
    headers: { Authorization: `Bearer ${token}` },
  });
  return res.data;
}

export async function setJevConfig(token: string, config: JevConfig): Promise<JevConfig> {
  const res = await api.put<JevConfig>("/api/v1/jev/config", config, {
    headers: { Authorization: `Bearer ${token}` },
  });
  return res.data;
}

/**
 * Proxy a Jev decision through the configured endpoint so plugins can call it
 * without the api_key. Returns the raw Jev response ({id, model, answers, ...}).
 */
export async function postJevDecision(params: Record<string, any>): Promise<any> {
  const res = await fetch(`${baseURL}/api/v1/jev/decisions`, {
    method: "POST",
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: JSON.stringify(params),
  });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`jev request failed (HTTP ${res.status}): ${text}`);
  }
  return res.json();
}

// --- Identity and presence ---

export async function getMe(): Promise<Me> {
  const res = await api.get<Me>("/api/v1/me");
  return res.data;
}

export async function getPresence(): Promise<Record<string, Occupancy>> {
  const res = await api.get<{ devices: Record<string, Occupancy> }>("/api/v1/presence");
  return res.data.devices ?? {};
}

export async function putDevicePresence(
  deviceId: string,
  mode: DeviceAccessMode = "control",
  takeover = false,
): Promise<Occupancy> {
  const res = await api.put<Occupancy>(`/api/v1/devices/${encodeURIComponent(deviceId)}/presence`, {
    mode,
    takeover,
  });
  return res.data;
}

export async function deleteDevicePresence(deviceId: string): Promise<void> {
  await api.delete(`/api/v1/devices/${encodeURIComponent(deviceId)}/presence`);
}

/**
 * Release a session when the page is unloading. `fetch` with keepalive (unlike
 * axios) survives the navigation.
 */
export function deleteDevicePresenceKeepalive(deviceId: string): void {
  void fetch(`${baseURL}/api/v1/devices/${encodeURIComponent(deviceId)}/presence`, {
    method: "DELETE",
    credentials: "same-origin",
    keepalive: true,
  }).catch(() => {});
}

export async function putPresenceBatch(
  devices: Record<string, { mode: DeviceAccessMode; takeover?: boolean }>,
): Promise<Record<string, Occupancy>> {
  const res = await api.put<{ devices: Record<string, Occupancy> }>("/api/v1/presence", { devices });
  return res.data.devices ?? {};
}

export async function getAccessConfig(token: string): Promise<AccessConfigResponse> {
  const res = await api.get<AccessConfigResponse>("/api/v1/access/config", {
    headers: { Authorization: `Bearer ${token}` },
  });
  return res.data;
}

export async function setAccessConfig(token: string, config: AccessConfig): Promise<AccessConfigResponse> {
  const res = await api.put<AccessConfigResponse>("/api/v1/access/config", config, {
    headers: { Authorization: `Bearer ${token}` },
  });
  return res.data;
}

// Wails 桌面环境的前端由 AssetServer 提供（与 API 不同源），需用绝对地址。
const inWails = isWailsEnv();

export const baseURL = inWails ? "http://localhost:44944" : "";

function getWSURL() {
  if (inWails) {
    return baseURL.replace(/^http/, "ws");
  }
  const loc = window.location;
  const scheme = loc.protocol === "https:" ? "wss:" : "ws:";
  return `${scheme}//${loc.host}`;
}

export const baseWSURL = getWSURL();
export const api = axios.create({
  baseURL,
});

/**
 * Sets the per-process desktop token that guards cluster management routes.
 * The desktop server has no management password and enables CORS, so the token
 * is only obtainable through the Wails bridge.
 */
export function setAPIToken(token: string): void {
  if (token) {
    api.defaults.headers.common["X-Uiautodev-Token"] = token;
  } else {
    delete api.defaults.headers.common["X-Uiautodev-Token"];
  }
}

api.interceptors.response.use(
  (response) => {
    const res = response.data;
    if (res.code !== 0) {
      return Promise.reject(new Error(res.message || "Error"));
    }
    return res;
  },
  (error) => {
    return Promise.reject(error);
  },
);
