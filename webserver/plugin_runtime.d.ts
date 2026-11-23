interface UiautodevPluginManifest {
  id?: string;
  name: string;
  version?: string;
  description?: string;
  homepage?: string;
  height?: number;
  platforms?: string[];
  allowedHosts?: string[];
}

interface ShellResult {
  output: string;
}

/** Which installed apps a listing includes. */
type AppScope = "user" | "system" | "all";

/** The foreground app (and, on Android/HarmonyOS, its activity/ability). */
interface CurrentApp {
  app_id: string;
  activity?: string;
}

/**
 * A screen tap captured while the screen mode is Recording (see setMode).
 * x/y are in the streamed video coordinate space defined by screenWidth/screenHeight
 * (device pixels; when crop is enabled these are the cropped dimensions).
 */
interface ScreenTapEvent {
  x: number;
  y: number;
  screenWidth: number;
  screenHeight: number;
  timestamp: number;
}

/**
 * The device screen interaction mode.
 * - Control: touch/keys operate the device (default).
 * - Readonly: touch/key injection disabled (view only).
 * - Recording: taps are captured as ScreenTapEvent instead of injected.
 */
declare enum ScreenMode {
  Control = "control",
  Readonly = "readonly",
  Recording = "recording",
}

/** OpenAI-compatible chat completion message (mirrors the OpenAI SDK). */
interface OpenAIChatCompletionMessage {
  role: "system" | "user" | "assistant" | "developer" | "tool";
  content: string | null | Array<{ type: string; text?: string; [key: string]: any }>;
  name?: string;
  [key: string]: any;
}

/** Request body for a chat completion. Mirrors the OpenAI SDK create() params. */
interface OpenAIChatCompletionParams {
  messages: OpenAIChatCompletionMessage[];
  model?: string;
  stream?: boolean;
  temperature?: number;
  max_tokens?: number;
  [key: string]: any;
}

/** A single complete chat completion response (stream:false). */
interface OpenAIChatCompletion {
  id: string;
  object: string;
  model: string;
  choices: Array<{
    index: number;
    message?: OpenAIChatCompletionMessage;
    delta?: { role?: string; content?: string | null; [key: string]: any };
    finish_reason?: string | null;
  }>;
  created?: number;
  [key: string]: any;
}

/** A single streamed chunk (stream:true). Mirrors the OpenAI SDK chunk shape. */
interface OpenAIChatCompletionChunk {
  id: string;
  object: string;
  model: string;
  choices: Array<{
    index: number;
    delta?: { role?: string; content?: string | null; [key: string]: any };
    finish_reason?: string | null;
  }>;
  created?: number;
  [key: string]: any;
}

/** Jev question: a calibrated yes/no probability (0..1). */
interface JevNoulQuestion {
  type: "noul";
  instructions: string;
  [key: string]: any;
}

/** Jev question: pick one labelled option; criteria maps option key -> description. */
interface JevChoiceQuestion {
  type: "choice";
  instructions: string;
  criteria: Record<string, string>;
  [key: string]: any;
}

/** Jev question: rate on an ordered scale; criteria are tier labels (low to high). */
interface JevScoreQuestion {
  type: "score";
  instructions: string;
  criteria: string[];
  [key: string]: any;
}

type JevQuestion = JevNoulQuestion | JevChoiceQuestion | JevScoreQuestion;

/** Request body for a Jev decision. questions is keyed by caller-chosen ids. */
interface JevDecisionParams {
  state: string | Record<string, any> | any[];
  questions: Record<string, JevQuestion>;
  model?: string;
  [key: string]: any;
}

/** A structured answer for one question, keyed by the question id. */
interface JevAnswer {
  type: "noul" | "choice" | "score";
  noul?: number;
  choice?: string;
  score?: number;
  probabilities?: Record<string, number>;
  legend?: Record<string, string>;
  confidence?: number;
  [key: string]: any;
}

/** The raw Jev decision response. */
interface JevDecision {
  id: string;
  model: string;
  model_version?: string;
  provider?: string;
  answers: Record<string, JevAnswer>;
  usage?: Record<string, any>;
  latency_ms?: number;
  [key: string]: any;
}

interface UiautodevAPI {
  readonly deviceId: string;
  readonly plugin: UiautodevPluginManifest;
  shell(cmd: string): Promise<ShellResult>;
  shell(opts: { command: string; timeoutMs?: number }): Promise<ShellResult>;
  /** Returns a data:image/png;base64,... string */
  screenshotAsBase64(): Promise<string>;
  /**
   * 将指定图片显示为 screen 上的静止画面（覆盖在实时画面上，作用同 Freeze）。
   * 传入 Blob 对象，或传 null 清除静止画面。
   */
  setScreenImage(image: Blob | null): Promise<void>;
  /** 向设备当前焦点输入文本 */
  inputText(text: string): Promise<void>;
  /**
   * 按下设备按键 key（如 home、back、enter、tab 等）；metaState 为组合键修饰符位掩码。
   */
  pressKey(key: string, metaState?: number): Promise<void>;
  /**
   * 在设备屏幕上执行一次点击。x/y 与 screenWidth/screenHeight 使用同一
   * 坐标系（视频坐标系，通常等于设备分辨率；开启裁剪时为裁剪后尺寸），
   * 后端据此按比例缩放到真实分辨率。与 onScreenTap 回调字段一致，可直接回放。
   */
  tap(x: number, y: number, screenWidth: number, screenHeight: number): Promise<void>;
  /**
   * 订阅设备屏幕上捕获的点击事件（需先 setMode 为 recording）。返回取消订阅函数。
   */
  onScreenTap(callback: (event: ScreenTapEvent) => void): () => void;
  /**
   * 设置当前屏幕模式。control=可操作（默认）；readonly=只读（禁用触摸/按键）；
   * recording=点击被捕获为 onScreenTap 而不操作设备。
   */
  setMode(mode: ScreenMode): Promise<void>;
  /**
   * 订阅屏幕模式变化。返回取消订阅函数。
   */
  onModeChange(callback: (mode: ScreenMode) => void): () => void;
  /**
   * Proxies an HTTP request through the server to bypass CORS.
   * Compatible with the standard fetch API.
   * Requires allowed_hosts to be set in plugin.json.
   **/
  fetch(url: string, init?: RequestInit): Promise<Response>;
  /**
   * 调用已配置的 OpenAI 兼容接口。入参与 OpenAI SDK 的 chat.completions.create
   * 一致；当 stream:true 时返回一个可异步迭代的 chunk 流。
   */
  openai(params: OpenAIChatCompletionParams): Promise<OpenAIChatCompletion | AsyncIterable<OpenAIChatCompletionChunk>>;
  /**
   * 调用已配置的 Jev（TypeSafe System One）决策接口。入参为决策请求体
   * （{state, questions, model?}），返回上游原始响应 {id, model, answers, ...}。
   * Jev 不支持流式。
   */
  jev(params: JevDecisionParams): Promise<JevDecision>;
  /**
   * 列出设备上已安装的应用 ID。scope: user=用户应用（默认），system=系统应用，all=全部。
   * 返回 app ID 数组（Android 包名 / iOS bundle ID / HarmonyOS bundle name）。
   */
  appList(scope?: AppScope): Promise<string[]>;
  /**
   * 启动应用。Android 未传 activity 时会自动解析启动入口，解析失败直接报错。
   * 返回实际启动的应用信息。
   */
  appStart(appId: string, activity?: string): Promise<CurrentApp>;
  /** 停止应用。 */
  appStop(appId: string): Promise<void>;
  /** 获取当前前台应用。 */
  appCurrent(): Promise<CurrentApp>;
}

declare var $u: UiautodevAPI;
