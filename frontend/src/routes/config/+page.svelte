<script lang="ts">
  import { onDestroy, onMount } from "svelte";
  import { goto } from "$app/navigation";
  import { page } from "$app/state";
  import { Button } from "$lib/components/ui/button/index.js";
  import * as Card from "$lib/components/ui/card/index.js";
  import Input from "$lib/components/ui/input/input.svelte";
  import { Bot, Network, Plug, Puzzle, RefreshCw, Scale, Unplug, ArrowUpCircle } from "lucide-svelte";
  import { getManagementAuthStatus, loginManagementAuth, setupManagementAuth } from "$lib/api/api.js";
  import { CopyButton } from "$lib/components/ui/copy-button";
  import { t } from "svelte-i18n";
  import AppHeader from "$lib/components/AppHeader.svelte";
  import PluginSettings from "$lib/components/settings/PluginSettings.svelte";
  import AISettings from "$lib/components/settings/AISettings.svelte";
  import JevSettings from "$lib/components/settings/JevSettings.svelte";
  import MCPSettings from "$lib/components/settings/MCPSettings.svelte";
  import ClusterSettings from "$lib/components/settings/ClusterSettings.svelte";
  import UpdateSettings from "$lib/components/settings/UpdateSettings.svelte";

  let status = $state<"loading" | "locked" | "unlocked">("loading");
  let needSetup = $state(false);
  let suggestedPassword = $state("");
  let passwordInput = $state("");
  let authError = $state("");
  let configFile = $state("");
  let pluginDir = $state("");
  let submitting = $state(false);

  let token = $state("");

  let anchorRaf = 0;

  function stopAnchorTracking() {
    if (anchorRaf) {
      cancelAnimationFrame(anchorRaf);
      anchorRaf = 0;
    }
  }

  // applyAnchor scrolls the target into view and keeps it pinned while the
  // settings panels load their content asynchronously, which changes the layout
  // height after the first scroll (otherwise the section drifts out of view).
  function applyAnchor(id: string) {
    stopAnchorTracking();
    const container = document.querySelector("main");
    let lastHeight = -1;
    let stableFrames = 0;
    let frames = 0;
    const tick = () => {
      const el = document.getElementById(id);
      if (!el || !container) {
        anchorRaf = 0;
        return;
      }
      el.scrollIntoView({ block: "start" });
      if (container.scrollHeight === lastHeight) {
        stableFrames++;
      } else {
        stableFrames = 0;
      }
      lastHeight = container.scrollHeight;
      frames++;
      if (stableFrames > 20 || frames > 360) {
        anchorRaf = 0;
        return;
      }
      anchorRaf = requestAnimationFrame(tick);
    };
    anchorRaf = requestAnimationFrame(tick);
  }

  onDestroy(stopAnchorTracking);

  const ANCHOR_MAP: Record<string, string> = {
    plugin: "config-plugin-section",
    ai: "config-ai-section",
    jev: "config-jev-section",
    mcp: "config-mcp-section",
    cluster: "config-cluster-section",
    update: "config-update-section",
  };

  function resolveSectionId(hash: string): string | undefined {
    const name = hash.replace(/^#/, "");
    if (!name) return undefined;
    return ANCHOR_MAP[name] ?? (Object.values(ANCHOR_MAP).includes(name) ? name : undefined);
  }

  function navigateToAnchor(name: string) {
    goto(`${page.url.pathname}${page.url.search}#${name}`, {
      replaceState: true,
      noScroll: true,
      keepFocus: true,
    });
    applyAnchor(ANCHOR_MAP[name] ?? name);
  }

  $effect(() => {
    if (status !== "unlocked") return;
    const sectionId = resolveSectionId(page.url.hash);
    if (sectionId) applyAnchor(sectionId);
  });

  $effect(() => {
    window.addEventListener("wheel", stopAnchorTracking, { passive: true });
    window.addEventListener("touchstart", stopAnchorTracking, { passive: true });
    return () => {
      window.removeEventListener("wheel", stopAnchorTracking);
      window.removeEventListener("touchstart", stopAnchorTracking);
    };
  });

  async function init() {
    try {
      const auth = await getManagementAuthStatus();
      pluginDir = auth.plugin_dir ?? "";
      if (auth.auth_required === false) {
        // Trusted environment (desktop): no password needed.
        token = "local";
        status = "unlocked";
        return;
      }
      configFile = auth.config_file ?? "";
      needSetup = !auth.initialized;
      if (needSetup) {
        suggestedPassword = auth.suggested_password ?? "";
        passwordInput = suggestedPassword;
      }
      status = "locked";
    } catch (e) {
      status = "locked";
    }
  }

  async function handleSetup() {
    if (!passwordInput.trim()) {
      authError = $t("config.setup.errorEmpty");
      return;
    }
    submitting = true;
    authError = "";
    try {
      token = await setupManagementAuth(passwordInput);
      passwordInput = "";
      status = "unlocked";
    } catch (e) {
      if (isConflict(e)) {
        // A password was configured elsewhere meanwhile: fall back to login.
        needSetup = false;
        authError = $t("config.errorConflict");
      } else {
        authError = e instanceof Error ? e.message : $t("config.errorSetupFailed");
      }
    } finally {
      submitting = false;
    }
  }

  async function handleLogin() {
    if (!passwordInput) return;
    submitting = true;
    authError = "";
    try {
      token = await loginManagementAuth(passwordInput);
      passwordInput = "";
      status = "unlocked";
    } catch (e) {
      authError = e instanceof Error ? e.message : $t("config.login.errorFailed");
    } finally {
      submitting = false;
    }
  }

  async function handleRegenerate() {
    try {
      const auth = await getManagementAuthStatus();
      if (auth.initialized) {
        needSetup = false;
        return;
      }
      const next = auth.suggested_password ?? "";
      suggestedPassword = next;
      passwordInput = next;
      authError = "";
    } catch (e) {
      console.error("failed to regenerate password suggestion", e);
    }
  }

  function resetAuth() {
    token = "";
    status = "locked";
    needSetup = false;
    authError = $t("config.expired");
  }

  function isConflict(e: unknown): boolean {
    return (e as { response?: { status?: number } })?.response?.status === 409;
  }

  onMount(() => {
    init();
  });
</script>

<div class="flex flex-col h-full bg-background">
  <AppHeader active="config" />

  <main class="flex-1 overflow-y-auto p-4">
    {#if status === "locked"}
      <div class="max-w-lg mx-auto">
        <Card.Root class="p-6">
          {#if needSetup}
            <Card.Header>
              <Card.Title class="flex items-center gap-2">
                <Unplug class="text-amber-500" />
                {$t("config.setup.title")}
              </Card.Title>
              <Card.Description>{$t("config.setup.desc")}</Card.Description>
            </Card.Header>
            <Card.Content class="space-y-4">
              <div>
                <label for="config-password" class="text-sm font-medium text-foreground block mb-1"
                  >{$t("config.passwordLabel")}</label>
                <div class="flex items-center gap-2">
                  <Input
                    id="config-password"
                    type="text"
                    bind:value={passwordInput}
                    placeholder={$t("config.setup.passwordPlaceholder")}
                    class="flex-1"
                    spellcheck="false"
                    autocomplete="off"
                    onkeydown={(e) => e.key === "Enter" && handleSetup()} />
                  <Button
                    size="icon"
                    variant="outline"
                    onclick={handleRegenerate}
                    title={$t("config.regenerateTitle")}
                    aria-label={$t("config.regenerateTitle")}>
                    <RefreshCw />
                  </Button>
                </div>
                {#if suggestedPassword}
                  <p class="text-xs text-muted-foreground mt-1.5">
                    {$t("config.suggested")}<code class="font-mono text-foreground">{suggestedPassword}</code>
                  </p>
                {/if}
              </div>
              {#if authError}
                <p class="text-sm text-destructive">{authError}</p>
              {/if}
              <Button class="w-full" onclick={handleSetup} disabled={submitting || !passwordInput}>
                {submitting ? $t("config.saving") : $t("config.setup.submit")}
              </Button>
            </Card.Content>
          {:else}
            <Card.Header>
              <Card.Title class="flex items-center gap-2">
                <Unplug class="text-amber-500" />
                {$t("config.login.title")}
              </Card.Title>
              <Card.Description>{$t("config.login.desc")}</Card.Description>
            </Card.Header>
            <Card.Content class="space-y-4">
              <div>
                <label for="config-password" class="text-sm font-medium text-foreground block mb-1"
                  >{$t("config.passwordLabel")}</label>
                <Input
                  id="config-password"
                  type="password"
                  bind:value={passwordInput}
                  placeholder={$t("config.login.passwordPlaceholder")}
                  onkeydown={(e) => e.key === "Enter" && handleLogin()} />
              </div>
              {#if authError}
                <p class="text-sm text-destructive">{authError}</p>
              {/if}
              <Button class="w-full" onclick={handleLogin} disabled={submitting || !passwordInput}>
                {submitting ? $t("config.verifying") : $t("config.login.submit")}
              </Button>
              {#if configFile}
                <div class="rounded-md border border-border bg-muted p-3">
                  <div class="flex items-center gap-2">
                    <code class="font-mono break-all flex-1 text-xs">{configFile}</code>
                    <CopyButton text={configFile} class="shrink-0" />
                  </div>
                  <p class="text-xs text-muted-foreground mt-1.5">
                    {$t("config.passwordHint")}
                  </p>
                </div>
              {/if}
            </Card.Content>
          {/if}
        </Card.Root>
      </div>
    {:else if status === "unlocked"}
      <div class="max-w-4xl mx-auto">
        <div class="flex gap-6">
          <nav class="w-48 shrink-0">
            <ul class="space-y-1 sticky top-0">
              <li>
                <Button variant="ghost" class="w-full justify-start" onclick={() => navigateToAnchor("plugin")}>
                  <Puzzle />
                  {$t("config.nav.plugin")}
                </Button>
              </li>
              <li>
                <Button variant="ghost" class="w-full justify-start" onclick={() => navigateToAnchor("ai")}>
                  <Bot />
                  {$t("config.nav.ai")}
                </Button>
              </li>
              <li>
                <Button variant="ghost" class="w-full justify-start" onclick={() => navigateToAnchor("jev")}>
                  <Scale />
                  {$t("config.nav.jev")}
                </Button>
              </li>
              <li>
                <Button variant="ghost" class="w-full justify-start" onclick={() => navigateToAnchor("mcp")}>
                  <Plug />
                  {$t("config.nav.mcp")}
                </Button>
              </li>
              <li>
                <Button variant="ghost" class="w-full justify-start" onclick={() => navigateToAnchor("cluster")}>
                  <Network />
                  {$t("config.nav.cluster")}
                </Button>
              </li>
              <li>
                <Button variant="ghost" class="w-full justify-start" onclick={() => navigateToAnchor("update")}>
                  <ArrowUpCircle />
                  {$t("config.nav.update")}
                </Button>
              </li>
            </ul>
          </nav>
          <div class="flex-1 min-w-0 space-y-4">
            <section id="config-plugin-section" class="scroll-mt-16">
              <PluginSettings {token} {pluginDir} onAuthExpired={resetAuth} />
            </section>
            <section id="config-ai-section" class="scroll-mt-16">
              <AISettings {token} onAuthExpired={resetAuth} />
            </section>
            <section id="config-jev-section" class="scroll-mt-16">
              <JevSettings {token} onAuthExpired={resetAuth} />
            </section>
            <section id="config-mcp-section" class="scroll-mt-16">
              <MCPSettings {token} onAuthExpired={resetAuth} />
            </section>
            <section id="config-cluster-section" class="scroll-mt-16">
              <ClusterSettings {token} onAuthExpired={resetAuth} />
            </section>
            <section id="config-update-section" class="scroll-mt-16">
              <UpdateSettings />
            </section>
          </div>
        </div>
      </div>
    {/if}
  </main>
</div>
