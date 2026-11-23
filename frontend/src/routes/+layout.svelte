<script lang="ts">
  import { base } from "$app/paths";
  import { goto } from "$app/navigation";
  import "./layout.css";
  import { onMount } from "svelte";
  import { getServerConfig, isWailsEnv, openExternalURL } from "$lib/compat";
  import type { ServerConfig } from "$lib/api/schemas.js";
  import { GitBranch, Laugh, Smile } from "lucide-svelte";
  import QqIcon from "$lib/components/icons/qq.svelte";
  import { t } from "svelte-i18n";
  import * as Tooltip from "$lib/components/ui/tooltip/index.js";
  import { hoverStore } from "$lib/stores/hover.svelte";
  import { initI18n } from "$lib/i18n/index.svelte";
  import { AlertStack } from "$lib/components/ui/alert/index.js";
  import { updateStore } from "$lib/stores/update.svelte";

  let { children } = $props();
  let serverConfig = $state<ServerConfig>();
  let feedbackHovered = $state(false);
  let latestVersion = $derived(updateStore.latestVersion);
  let updateAvailable = $derived(updateStore.available);

  initI18n();

  const version = $derived(serverConfig?.version ?? "unknown");

  function openExternalLinkAnchor(e: MouseEvent) {
    if (!isWailsEnv()) return;
    const target = e.target as HTMLAnchorElement;
    if (target.tagName === "A" && target.href.startsWith("http")) {
      e.preventDefault();
      void openExternalURL(target.href);
    }
  }

  onMount(() => {
    (async () => {
      serverConfig = await getServerConfig();
    })();
    updateStore.checkVersion();

    // Cmd+, (macOS) / Ctrl+, (Windows/Linux) opens the settings page. The
    // desktop app also registers this accelerator in the native menu, so the
    // handler covers the web/server build.
    const handleSettingsShortcut = (e: KeyboardEvent) => {
      if (e.key !== "," || e.repeat) return;
      if (!(e.metaKey || e.ctrlKey) || e.altKey || e.shiftKey) return;
      e.preventDefault();
      goto("/config");
    };
    window.addEventListener("keydown", handleSettingsShortcut);

    const unsubscribeEvents: Array<() => void> = [];
    let disposed = false;
    if (isWailsEnv()) {
      void import("@wailsio/runtime").then(({ Events }) => {
        if (disposed) return;
        unsubscribeEvents.push(
          Events.On("open-update-settings", () => {
            goto("/config#update");
          }),
          Events.On("open-settings", () => {
            goto("/config");
          }),
        );
      });
    }

    return () => {
      disposed = true;
      window.removeEventListener("keydown", handleSettingsShortcut);
      for (const unsubscribe of unsubscribeEvents) unsubscribe();
    };
  });
</script>

<svelte:head>
  <title>{$t("common.appName")}</title>
  <link rel="icon" href="{base}/favicon.ico" />
  <script>
    var _hmt = _hmt || [];
    (function () {
      var hm = document.createElement("script");
      hm.src = "https://hm.baidu.com/hm.js?fd7a01a1bdc85d14fde4563dcc75be5b";
      var s = document.getElementsByTagName("script")[0];
      s.parentNode.insertBefore(hm, s);
    })();
  </script>
</svelte:head>

<div class="flex flex-col h-screen">
  <div class="flex-1 min-h-0 overflow-y-auto">
    <Tooltip.Provider>
      {@render children()}
    </Tooltip.Provider>
  </div>
  <footer
    class="shrink-0 border-t border-gray-300 dark:border-slate-700 p-2 text-sm text-gray-500 dark:text-gray-400 grid grid-cols-[1fr_auto_1fr] items-center gap-4">
    <div class="flex justify-start items-center min-w-0">
      {#if hoverStore.visible}
        <div class="font-mono truncate">
          x: {Math.round(hoverStore.x)} y: {Math.round(hoverStore.y)} ({(hoverStore.percentX * 100).toFixed(1)}%, {(
            hoverStore.percentY * 100
          ).toFixed(1)}%)
        </div>
      {/if}
    </div>
    <div class="flex justify-center items-center gap-4 min-w-0 overflow-hidden">
      <a
        href="https://uiauto.dev"
        class="underline"
        onclick={openExternalLinkAnchor}
        target="_blank"
        rel="noopener noreferrer">UiautoDev</a>
      <span class="truncate">&copy; 2026 UiautoDev</span>
      <span class="shrink-0 flex items-center gap-1">
        <GitBranch size={14} />
        v{version}
      </span>
      {#if updateAvailable && latestVersion}
        <button
          onclick={() => goto("/config#update")}
          class="flex items-center gap-1.5 shrink-0 text-red-600 hover:underline transition-colors dark:text-red-400"
          title={$t("common.newVersionAvailable", { values: { version: latestVersion } })}>
          <span class="rounded-sm bg-red-500 px-1 py-0.5 text-[10px] font-bold leading-none tracking-wide text-white">
            {$t("common.new")}
          </span>
          <span class="truncate">v{latestVersion}</span>
        </button>
      {/if}
    </div>
    <div class="max-md:hidden flex justify-end items-center gap-4">
      <span class="flex items-center gap-1 whitespace-nowrap">
        <QqIcon size={14} />
        {$t("common.qqGroupNumber")}
      </span>
      <span class="text-gray-300 dark:text-gray-600 shrink-0">|</span>
      <button
        class="flex items-center gap-1 hover:text-gray-700 dark:hover:text-gray-300 transition-colors"
        title={$t("common.feedback")}
        aria-label={$t("common.feedback")}
        onmouseenter={() => (feedbackHovered = true)}
        onmouseleave={() => (feedbackHovered = false)}
        onclick={() => openExternalURL("https://github.com/uiautodev/uiautodev/issues")}>
        {#if feedbackHovered}
          <Laugh size={18} />
        {:else}
          <Smile size={18} />
        {/if}
      </button>
    </div>
  </footer>
</div>

<AlertStack />
