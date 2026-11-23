<script lang="ts">
  import { onMount, untrack } from "svelte";
  import { goto } from "$app/navigation";
  import { page } from "$app/state";
  import { Button } from "$lib/components/ui/button/index.js";
  import * as Card from "$lib/components/ui/card/index.js";
  import Input from "$lib/components/ui/input/input.svelte";
  import * as UnderlineTabs from "$lib/components/ui/underline-tabs/index.js";
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
        <UnderlineTabs.Root bind:value={tab} class="gap-4">
          <UnderlineTabs.List aria-label={$t("config.title")}>
            <UnderlineTabs.Trigger value="plugin">
              <Puzzle />
              {$t("config.nav.plugin")}
            </UnderlineTabs.Trigger>
            <UnderlineTabs.Trigger value="ai">
              <Bot />
              {$t("config.nav.ai")}
            </UnderlineTabs.Trigger>
            <UnderlineTabs.Trigger value="jev">
              <Scale />
              {$t("config.nav.jev")}
            </UnderlineTabs.Trigger>
            <UnderlineTabs.Trigger value="mcp">
              <Plug />
              {$t("config.nav.mcp")}
            </UnderlineTabs.Trigger>
            <UnderlineTabs.Trigger value="cluster">
              <Network />
              {$t("config.nav.cluster")}
            </UnderlineTabs.Trigger>
            <UnderlineTabs.Trigger value="access">
              <ShieldCheck />
              {$t("config.nav.access")}
            </UnderlineTabs.Trigger>
            <UnderlineTabs.Trigger value="update">
              <ArrowUpCircle />
              {$t("config.nav.update")}
            </UnderlineTabs.Trigger>
          </UnderlineTabs.List>
          <UnderlineTabs.Content value="plugin">
            <PluginSettings {token} {pluginDir} onAuthExpired={resetAuth} />
          </UnderlineTabs.Content>
          <UnderlineTabs.Content value="ai">
            <AISettings {token} onAuthExpired={resetAuth} />
          </UnderlineTabs.Content>
          <UnderlineTabs.Content value="jev">
            <JevSettings {token} onAuthExpired={resetAuth} />
          </UnderlineTabs.Content>
          <UnderlineTabs.Content value="mcp">
            <MCPSettings {token} onAuthExpired={resetAuth} />
          </UnderlineTabs.Content>
          <UnderlineTabs.Content value="cluster">
            <ClusterSettings {token} onAuthExpired={resetAuth} />
          </UnderlineTabs.Content>
          <UnderlineTabs.Content value="access">
            <AccessSettings {token} onAuthExpired={resetAuth} />
          </UnderlineTabs.Content>
          <UnderlineTabs.Content value="update">
            <UpdateSettings />
          </UnderlineTabs.Content>
        </UnderlineTabs.Root>
      </div>
    {/if}
  </main>
</div>
