<script lang="ts">
  import { onMount, untrack } from "svelte";
  import { goto } from "$app/navigation";
  import { page } from "$app/state";
  import { Button, buttonVariants } from "$lib/components/ui/button/index.js";
  import * as Card from "$lib/components/ui/card/index.js";
  import Input from "$lib/components/ui/input/input.svelte";
  import * as Tabs from "$lib/components/ui/tabs/index.js";
  import { cn } from "$lib/utils.js";
  import { Bot, Network, Plug, Puzzle, RefreshCw, Scale, Unplug, ArrowUpCircle, ShieldCheck } from "lucide-svelte";
  import { getManagementAuthStatus, loginManagementAuth, setupManagementAuth } from "$lib/api/api.js";
  import { CopyButton } from "$lib/components/ui/copy-button";
  import { t } from "svelte-i18n";
  import AppHeader from "$lib/components/AppHeader.svelte";
  import PluginSettings from "$lib/components/settings/PluginSettings.svelte";
  import AISettings from "$lib/components/settings/AISettings.svelte";
  import JevSettings from "$lib/components/settings/JevSettings.svelte";
  import MCPSettings from "$lib/components/settings/MCPSettings.svelte";
  import ClusterSettings from "$lib/components/settings/ClusterSettings.svelte";
  import AccessSettings from "$lib/components/settings/AccessSettings.svelte";
  import UpdateSettings from "$lib/components/settings/UpdateSettings.svelte";
  import { managementAuth } from "$lib/stores/management-auth.svelte";

  let status = $state<"loading" | "locked" | "unlocked">("loading");
  let needSetup = $state(false);
  let suggestedPassword = $state("");
  let passwordInput = $state("");
  let authError = $state("");
  let configFile = $state("");
  let pluginDir = $state("");
  let submitting = $state(false);

  let token = $state(managementAuth.token);

  // Tab ids double as URL hashes so existing deep links keep working. Panels
  // stay mounted (bits-ui only hides the inactive ones), which preserves
  // unsaved form input and avoids refetching settings on every switch.
  const TABS = ["plugin", "ai", "jev", "mcp", "cluster", "access", "update"] as const;
  let tab = $state<string>("plugin");

  // Keep the original vertical settings navigation: each entry is a tab styled
  // like the old ghost button, with the active one highlighted.
  const navItem = cn(
    buttonVariants({ variant: "ghost" }),
    "w-full justify-start data-[state=active]:bg-secondary data-[state=active]:text-secondary-foreground",
  );

  function tabFromHash(hash: string): string | undefined {
    const name = hash.replace(/^#/, "");
    return (TABS as readonly string[]).includes(name) ? name : undefined;
  }

  $effect(() => {
    const next = tabFromHash(page.url.hash);
    // untrack(tab) keeps this effect keyed on the hash alone; reading tab here
    // would re-run it on every click and snap the selection back to the old hash.
    if (next && next !== untrack(() => tab)) tab = next;
  });

  $effect(() => {
    if (status !== "unlocked") return;
    const hash = `#${tab}`;
    if (page.url.hash === hash) return;
    goto(`${page.url.pathname}${page.url.search}${hash}`, {
      replaceState: true,
      noScroll: true,
      keepFocus: true,
    });
  });

  async function init() {
    try {
      const auth = await getManagementAuthStatus();
      pluginDir = auth.plugin_dir ?? "";
      if (auth.auth_required === false) {
        // Trusted environment (desktop): no password needed.
        token = "local";
        managementAuth.token = token;
        status = "unlocked";
        return;
      }
      configFile = auth.config_file ?? "";
      if (managementAuth.token) {
        // Reuse the session unlocked earlier in this SPA session.
        token = managementAuth.token;
        status = "unlocked";
        return;
      }
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
      managementAuth.token = token;
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
      managementAuth.token = token;
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
    managementAuth.token = "";
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
        <Tabs.Root bind:value={tab} orientation="vertical" class="flex-row gap-6">
          <Tabs.List
            aria-label={$t("config.title")}
            class="sticky top-0 h-auto w-48 shrink-0 flex-col items-stretch gap-1 self-start rounded-none bg-transparent p-0">
            <Tabs.Trigger value="plugin" class={navItem}>
              <Puzzle />
              {$t("config.nav.plugin")}
            </Tabs.Trigger>
            <Tabs.Trigger value="ai" class={navItem}>
              <Bot />
              {$t("config.nav.ai")}
            </Tabs.Trigger>
            <Tabs.Trigger value="jev" class={navItem}>
              <Scale />
              {$t("config.nav.jev")}
            </Tabs.Trigger>
            <Tabs.Trigger value="mcp" class={navItem}>
              <Plug />
              {$t("config.nav.mcp")}
            </Tabs.Trigger>
            <Tabs.Trigger value="cluster" class={navItem}>
              <Network />
              {$t("config.nav.cluster")}
            </Tabs.Trigger>
            <Tabs.Trigger value="access" class={navItem}>
              <ShieldCheck />
              {$t("config.nav.access")}
            </Tabs.Trigger>
            <Tabs.Trigger value="update" class={navItem}>
              <ArrowUpCircle />
              {$t("config.nav.update")}
            </Tabs.Trigger>
          </Tabs.List>
          <Tabs.Content value="plugin" class="min-w-0">
            <PluginSettings {token} {pluginDir} onAuthExpired={resetAuth} />
          </Tabs.Content>
          <Tabs.Content value="ai" class="min-w-0">
            <AISettings {token} onAuthExpired={resetAuth} />
          </Tabs.Content>
          <Tabs.Content value="jev" class="min-w-0">
            <JevSettings {token} onAuthExpired={resetAuth} />
          </Tabs.Content>
          <Tabs.Content value="mcp" class="min-w-0">
            <MCPSettings {token} onAuthExpired={resetAuth} />
          </Tabs.Content>
          <Tabs.Content value="cluster" class="min-w-0">
            <ClusterSettings {token} onAuthExpired={resetAuth} />
          </Tabs.Content>
          <Tabs.Content value="access" class="min-w-0">
            <AccessSettings {token} onAuthExpired={resetAuth} />
          </Tabs.Content>
          <Tabs.Content value="update" class="min-w-0">
            <UpdateSettings />
          </Tabs.Content>
        </Tabs.Root>
      </div>
    {/if}
  </main>
</div>
