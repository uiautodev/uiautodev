<script lang="ts">
  import { Terminal } from "@xterm/xterm";
  import { FitAddon } from "@xterm/addon-fit";
  import "@xterm/xterm/css/xterm.css";
  import { onDestroy } from "svelte";
  import { get } from "svelte/store";
  import { t } from "svelte-i18n";
  import { Plus, X } from "lucide-svelte";
  import { getTerminalURL, sendTerminalResize, type TerminalMessage } from "$lib/api/api";
  import { themeStore } from "$lib/stores/theme.svelte";

  let { deviceId, active = true, class: className = "", ...restProps } = $props();

  type TerminalStatus = "connecting" | "open" | "closed" | "failed";

  interface TerminalTab {
    id: number;
    index: number;
    status: TerminalStatus;
  }

  const ANSI_LIGHT = {
    black: "#1f2937",
    red: "#b91c1c",
    green: "#15803d",
    yellow: "#a16207",
    blue: "#1d4ed8",
    magenta: "#a21caf",
    cyan: "#0e7490",
    white: "#6b7280",
    brightBlack: "#4b5563",
    brightRed: "#dc2626",
    brightGreen: "#16a34a",
    brightYellow: "#ca8a04",
    brightBlue: "#2563eb",
    brightMagenta: "#c026d3",
    brightCyan: "#0891b2",
    brightWhite: "#9ca3af",
  };

  const ANSI_DARK = {
    black: "#3f3f46",
    red: "#f87171",
    green: "#4ade80",
    yellow: "#facc15",
    blue: "#60a5fa",
    magenta: "#e879f9",
    cyan: "#22d3ee",
    white: "#d4d4d8",
    brightBlack: "#71717a",
    brightRed: "#fca5a5",
    brightGreen: "#86efac",
    brightYellow: "#fde047",
    brightBlue: "#93c5fd",
    brightMagenta: "#f0abfc",
    brightCyan: "#67e8f9",
    brightWhite: "#fafafa",
  };

  /**
   * xterm writes theme colors as inline CSS, so the oklch() tokens from
   * layout.css work as-is; only a missing token needs a fallback.
   */
  function resolveColor(token: string, fallback: string): string {
    if (typeof document === "undefined") return fallback;
    return getComputedStyle(document.documentElement).getPropertyValue(token).trim() || fallback;
  }

  function createTerminalTheme() {
    const dark = themeStore.resolved === "dark";
    const background = resolveColor("--background", dark ? "#09090b" : "#ffffff");
    const foreground = resolveColor("--foreground", dark ? "#fafafa" : "#111827");
    return {
      background,
      foreground,
      cursor: foreground,
      cursorAccent: background,
      selectionBackground: resolveColor("--muted-foreground", dark ? "#3f3f46" : "#cbd5e1"),
      ...(dark ? ANSI_DARK : ANSI_LIGHT),
    };
  }

  function translate(key: string, values?: Record<string, string | number>): string {
    return get(t)(key, values ? { values } : undefined) || key;
  }

  function statusClass(status: TerminalStatus): string {
    switch (status) {
      case "open":
        return "bg-emerald-500";
      case "connecting":
        return "bg-amber-500";
      default:
        return "bg-muted-foreground";
    }
  }

  /**
   * One browser-style tab: its own xterm instance and websocket. Switching tabs
   * only hides the pane so the shell keeps running.
   */
  class TerminalSession {
    private readonly term: Terminal;
    private readonly fit = new FitAddon();
    private readonly dataSubscription: { dispose(): void };
    private socket: WebSocket | null = null;
    private element: HTMLDivElement | null = null;
    private observer: ResizeObserver | null = null;
    private disposed = false;
    private gotExit = false;
    private failureReported = false;

    private readonly id: string;
    private readonly onStatus: (status: TerminalStatus) => void;

    constructor(id: string, onStatus: (status: TerminalStatus) => void) {
      this.id = id;
      this.onStatus = onStatus;
      this.term = new Terminal({
        fontSize: 12,
        fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, "Liberation Mono", monospace',
        cursorBlink: true,
        scrollback: 5000,
        theme: createTerminalTheme(),
      });
      this.term.loadAddon(this.fit);
      this.dataSubscription = this.term.onData((data) => {
        const socket = this.socket;
        if (socket?.readyState !== WebSocket.OPEN) return;
        socket.send(new TextEncoder().encode(data));
      });
    }

    attach(element: HTMLDivElement) {
      if (this.disposed || this.element) return;
      this.element = element;
      this.term.open(element);
      this.observer = new ResizeObserver(() => this.refit());
      this.observer.observe(element);
      this.connect();
    }

    activate() {
      requestAnimationFrame(() => {
        if (this.disposed) return;
        this.refit();
        this.term.focus();
      });
    }

    applyTheme() {
      if (this.disposed) return;
      this.term.options.theme = createTerminalTheme();
    }

    dispose() {
      if (this.disposed) return;
      this.disposed = true;
      this.observer?.disconnect();
      this.observer = null;
      this.dataSubscription.dispose();
      const socket = this.socket;
      this.socket = null;
      socket?.close();
      this.term.dispose();
      this.element = null;
    }

    private connect() {
      this.refit();
      this.onStatus("connecting");
      const socket = new WebSocket(getTerminalURL(this.id, this.term.rows, this.term.cols));
      socket.binaryType = "arraybuffer";
      this.socket = socket;
      socket.onopen = () => {
        if (this.disposed) return;
        this.onStatus("open");
        sendTerminalResize(socket, this.term.rows, this.term.cols);
        this.term.focus();
      };
      socket.onmessage = (event) => {
        if (typeof event.data === "string") {
          this.handleControl(event.data);
          return;
        }
        this.term.write(new Uint8Array(event.data as ArrayBuffer));
      };
      socket.onerror = () => this.reportFailure();
      socket.onclose = () => {
        if (this.socket !== socket) return;
        this.socket = null;
        if (!this.gotExit) this.reportFailure();
      };
    }

    private handleControl(raw: string) {
      let message: TerminalMessage;
      try {
        message = JSON.parse(raw) as TerminalMessage;
      } catch {
        return;
      }
      if (message.type === "exit") {
        this.gotExit = true;
        this.onStatus("closed");
        this.notice(translate("device.terminal.exited", { code: message.code ?? 0 }));
        return;
      }
      if (message.type === "error") {
        this.failureReported = true;
        this.onStatus("failed");
        this.notice(message.message || translate("device.terminal.disconnected"));
      }
    }

    private reportFailure() {
      if (this.disposed || this.gotExit || this.failureReported) return;
      this.failureReported = true;
      this.onStatus("failed");
      this.notice(translate("device.terminal.disconnected"));
    }

    private notice(text: string) {
      this.term.writeln(`\r\n\x1b[2m${text}\x1b[0m`);
    }

    private refit() {
      if (this.disposed || !this.element) return;
      if (this.element.clientWidth === 0 || this.element.clientHeight === 0) return;
      try {
        this.fit.fit();
      } catch {
        return;
      }
      if (this.socket) sendTerminalResize(this.socket, this.term.rows, this.term.cols);
    }
  }

  let tabs = $state<TerminalTab[]>([]);
  let activeId = $state<number | null>(null);
  let initialized = $state(false);
  let nextTabId = 1;
  let nextTabIndex = 1;
  const sessions = new Map<number, TerminalSession>();
  const paneElements: Record<number, HTMLDivElement | undefined> = {};

  function updateStatus(id: number, status: TerminalStatus) {
    const tab = tabs.find((item) => item.id === id);
    if (tab) tab.status = status;
  }

  function activateTab(id: number) {
    activeId = id;
    sessions.get(id)?.activate();
  }

  function addTab() {
    const id = nextTabId++;
    const session = new TerminalSession(deviceId, (status) => updateStatus(id, status));
    sessions.set(id, session);
    tabs.push({ id, index: nextTabIndex++, status: "connecting" });
    activateTab(id);
  }

  function closeTab(id: number) {
    const index = tabs.findIndex((item) => item.id === id);
    const session = sessions.get(id);
    sessions.delete(id);
    session?.dispose();
    delete paneElements[id];
    if (index >= 0) tabs.splice(index, 1);
    if (activeId !== id) return;
    const next = tabs[Math.min(index, tabs.length - 1)];
    if (next) {
      activateTab(next.id);
    } else {
      activeId = null;
    }
  }

  function attachPane(node: HTMLDivElement, id: number) {
    paneElements[id] = node;
    sessions.get(id)?.attach(node);
    return {
      destroy() {
        delete paneElements[id];
      },
    };
  }

  $effect(() => {
    // Recolour every live terminal when the light/dark theme changes.
    void themeStore.resolved;
    for (const session of sessions.values()) session.applyTheme();
  });

  $effect(() => {
    // Opening a shell costs an adb round-trip, so defer it until the outer
    // terminal tab is shown; afterwards sessions stay alive while hidden.
    if (!active || initialized) return;
    initialized = true;
    addTab();
  });

  onDestroy(() => {
    for (const session of sessions.values()) session.dispose();
    sessions.clear();
  });
</script>

<div class={`flex h-full min-h-0 flex-col ${className}`} {...restProps}>
  <div class="flex shrink-0 items-center gap-1 overflow-x-auto border-b border-border pb-1">
    {#each tabs as tab (tab.id)}
      <div
        class="group flex shrink-0 items-center gap-1 rounded-t-md border border-b-0 px-2 py-1 text-xs transition-colors"
        class:border-border={tab.id === activeId}
        class:bg-muted={tab.id === activeId}
        class:text-foreground={tab.id === activeId}
        class:border-transparent={tab.id !== activeId}
        class:text-muted-foreground={tab.id !== activeId}>
        <button type="button" class="flex items-center gap-1.5" onclick={() => activateTab(tab.id)}>
          <span class={`size-1.5 rounded-full ${statusClass(tab.status)}`}></span>
          <span>{$t("device.terminal.tabTitle", { values: { index: tab.index } })}</span>
        </button>
        <button
          type="button"
          class="rounded p-0.5 opacity-60 hover:bg-accent hover:text-accent-foreground hover:opacity-100"
          aria-label={$t("device.terminal.close")}
          title={$t("device.terminal.close")}
          onclick={() => closeTab(tab.id)}>
          <X class="size-3" />
        </button>
      </div>
    {/each}
    <button
      type="button"
      class="ml-1 shrink-0 rounded p-1 text-muted-foreground hover:bg-accent hover:text-accent-foreground"
      aria-label={$t("device.terminal.new")}
      title={$t("device.terminal.new")}
      onclick={addTab}>
      <Plus class="size-4" />
    </button>
  </div>
  <div class="relative mt-1 min-h-0 flex-1">
    {#each tabs as tab (tab.id)}
      <div
        class="terminal-pane absolute inset-0 rounded-md border border-border bg-background"
        class:hidden={tab.id !== activeId}
        use:attachPane={tab.id}>
      </div>
    {/each}
    {#if tabs.length === 0}
      <div class="flex h-full items-center justify-center text-sm text-muted-foreground">
        {$t("device.terminal.empty")}
      </div>
    {/if}
  </div>
</div>

<style>
  .terminal-pane :global(.xterm) {
    padding: 4px;
  }
</style>
