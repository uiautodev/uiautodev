export const ConnectionStatus = {
  Connecting: "connecting",
  Connected: "connected",
  Disconnected: "disconnected",
} as const;

export type ConnectionStatus = (typeof ConnectionStatus)[keyof typeof ConnectionStatus];

export interface ConnectionState {
  status: ConnectionStatus;
  /** Seconds left until the next retry, or `0` when none is pending. */
  retrySeconds: number;
}

export interface ConnectHandle {
  dispose(): void;
}

export interface ConnectionOptions {
  /**
   * Establish a connection. Resolve with a handle that tears the connection
   * down, or reject if the attempt fails. `onDisconnect` must be called when an
   * established connection drops unexpectedly.
   */
  connect(onDisconnect: () => void): Promise<ConnectHandle>;
  onChange?: (state: ConnectionState) => void;
  baseDelay?: number;
  maxDelay?: number;
  /** A connection alive at least this long resets the backoff on disconnect. */
  healthyThreshold?: number;
}

/**
 * Transport-agnostic reconnect controller. On `disconnected` it retries with
 * exponential backoff, reporting the current status and a live countdown to the
 * next attempt; `reconnect()` forces an immediate attempt.
 */
export class ConnectionController {
  #status: ConnectionStatus = ConnectionStatus.Connecting;
  #handle: ConnectHandle | undefined;
  #timer: ReturnType<typeof setTimeout> | undefined;
  #tick: ReturnType<typeof setInterval> | undefined;
  #retryAt: number | null = null;
  #connectedAt = 0;
  #delay: number;
  #generation = 0;
  #disposed = false;
  #connect: ConnectionOptions["connect"];
  #onChange: (state: ConnectionState) => void;
  #baseDelay: number;
  #maxDelay: number;
  #healthyThreshold: number;

  constructor(options: ConnectionOptions) {
    this.#connect = options.connect;
    this.#onChange = options.onChange ?? (() => {});
    this.#baseDelay = options.baseDelay ?? 100;
    this.#maxDelay = options.maxDelay ?? 30000;
    this.#healthyThreshold = options.healthyThreshold ?? 5000;
    this.#delay = this.#baseDelay;
  }

  start(): void {
    this.#delay = this.#baseDelay;
    void this.#attempt();
  }

  reconnect(): void {
    if (this.#disposed) return;
    this.#clearRetry();
    this.#delay = this.#baseDelay;
    void this.#attempt();
  }

  dispose(): void {
    this.#disposed = true;
    this.#clearRetry();
    this.#disposeHandle();
  }

  async #attempt(): Promise<void> {
    if (this.#disposed) return;
    const generation = ++this.#generation;
    this.#setStatus(ConnectionStatus.Connecting);
    this.#disposeHandle();

    let handle: ConnectHandle;
    try {
      handle = await this.#connect(() => this.#handleDisconnect(generation));
    } catch {
      if (this.#disposed || generation !== this.#generation) return;
      this.#setDisconnected();
      return;
    }

    if (this.#disposed || generation !== this.#generation) {
      handle.dispose();
      return;
    }
    this.#handle = handle;
    this.#connectedAt = Date.now();
    this.#setStatus(ConnectionStatus.Connected);
  }

  #handleDisconnect(generation: number): void {
    if (this.#disposed || generation !== this.#generation) return;
    this.#disposeHandle();
    if (Date.now() - this.#connectedAt >= this.#healthyThreshold) {
      this.#delay = this.#baseDelay;
    }
    this.#setDisconnected();
  }

  #setDisconnected(): void {
    if (this.#status === ConnectionStatus.Disconnected) return;
    this.#setStatus(ConnectionStatus.Disconnected);
    this.#schedule();
  }

  #setStatus(next: ConnectionStatus): void {
    if (this.#status === next) return;
    this.#status = next;
    if (next === ConnectionStatus.Connected) {
      // A previous disconnect may have left a retry pending; cancel it so it
      // cannot tear down the connection we just established.
      this.#clearRetry();
    }
    this.#emit();
  }

  #schedule(): void {
    if (this.#disposed) return;
    this.#clearRetry();
    const delay = this.#delay;
    this.#delay = Math.min(this.#maxDelay, Math.max(this.#baseDelay, this.#delay * 2));
    this.#retryAt = Date.now() + delay;
    this.#emit();
    this.#tick = setInterval(() => this.#emit(), 250);
    this.#timer = setTimeout(() => {
      this.#timer = undefined;
      this.#clearRetry();
      void this.#attempt();
    }, delay);
  }

  #emit(): void {
    this.#onChange({
      status: this.#status,
      retrySeconds: this.#retryAt === null ? 0 : Math.max(0, Math.ceil((this.#retryAt - Date.now()) / 1000)),
    });
  }

  #clearRetry(): void {
    if (this.#timer) {
      clearTimeout(this.#timer);
      this.#timer = undefined;
    }
    if (this.#tick) {
      clearInterval(this.#tick);
      this.#tick = undefined;
    }
    this.#retryAt = null;
  }

  #disposeHandle(): void {
    const handle = this.#handle;
    this.#handle = undefined;
    handle?.dispose();
  }
}
