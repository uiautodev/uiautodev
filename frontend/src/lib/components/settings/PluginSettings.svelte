<script lang="ts">
  import { Button, AsyncButton } from "$lib/components/ui/button/index.js";
  import * as Card from "$lib/components/ui/card/index.js";
  import * as Alert from "$lib/components/ui/alert-dialog/index.js";
  import Input from "$lib/components/ui/input/input.svelte";
  import { Switch } from "$lib/components/ui/switch/index.js";
  import * as UnderlineTabs from "$lib/components/ui/underline-tabs/index.js";
  import { Download, PackageOpen, RefreshCw, Trash2 } from "lucide-svelte";
  import { listPlugins, setPluginEnabled, installPlugin, uninstallPlugin } from "$lib/api/api.js";
  import type { Plugin } from "$lib/api/schemas.js";
  import { isOfficialPlugin } from "$lib/plugin.js";
  import PluginMarketSettings from "./PluginMarketSettings.svelte";
  import { alertStore } from "$lib/stores/alert.svelte";
  import { cn } from "$lib/utils.js";
  import { t } from "svelte-i18n";

  let {
    token,
    pluginDir = "",
    onAuthExpired,
  } = $props<{
    token: string;
    pluginDir?: string;
    onAuthExpired: () => void;
  }>();

  let plugins = $state<Plugin[]>([]);
  let loadingPlugins = $state(false);
  let refreshed = $state(false);
  let installError = $state("");
  let installFiles = $state<FileList | undefined>(undefined);
  let uploading = $state(false);
  let uninstallError = $state("");

  let activeTab = $state("installed");

  type PluginFilter = "all" | "official" | "third-party";
  let pluginFilter = $state<PluginFilter>("all");

  const filterOptions: { value: PluginFilter; label: string }[] = [
    { value: "all", label: "plugin.filterAll" },
    { value: "official", label: "plugin.official" },
    { value: "third-party", label: "plugin.thirdParty" },
  ];

  function matchesFilter(plugin: Plugin, filter: PluginFilter): boolean {
    const official = isOfficialPlugin(plugin.id);
    if (filter === "official") return official;
    if (filter === "third-party") return !official && !plugin.builtin;
    return true;
  }

  const filterCounts = $derived({
    all: plugins.length,
    official: plugins.filter((p) => matchesFilter(p, "official")).length,
    "third-party": plugins.filter((p) => matchesFilter(p, "third-party")).length,
  });

  const filteredPlugins = $derived(plugins.filter((p) => matchesFilter(p, pluginFilter)));
  async function refreshPlugins(manual = false) {
    if (!token) return;
    loadingPlugins = true;
    try {
      const list = await listPlugins("all");
      list.sort((a, b) => a.id.localeCompare(b.id));
      plugins = list;
      if (manual) refreshed = true;
    } catch (e) {
      console.error("failed to load plugins", e);
    } finally {
      loadingPlugins = false;
    }
  }

  $effect(() => {
    if (token) refreshPlugins();
  });

  async function handleToggleEnabled(plugin: Plugin) {
    try {
      await setPluginEnabled(token, plugin.id, !plugin.enabled);
      plugins = plugins.map((p) => (p.id === plugin.id ? { ...p, enabled: !plugin.enabled } : p));
    } catch (e) {
      console.error("failed to toggle plugin", e);
      if (isUnauthorized(e)) onAuthExpired();
    }
  }

  async function handleInstall() {
    const file = installFiles?.[0];
    if (!file) return;
    uploading = true;
    installError = "";
    try {
      await installPlugin(token, file);
      await refreshPlugins();
      installFiles = undefined;
      alertStore.success($t("plugin.installSuccess"));
    } catch (e) {
      const message = e instanceof Error ? e.message : $t("plugin.installFailed");
      installError = message;
      alertStore.error(message, { title: $t("plugin.installFailed") });
      if (isUnauthorized(e)) onAuthExpired();
    } finally {
      uploading = false;
    }
  }

  async function handleUninstall(plugin: Plugin) {
    if (!plugin) return;
    uninstallError = "";
    try {
      await uninstallPlugin(token, plugin.id);
      await refreshPlugins();
    } catch (e) {
      uninstallError = e instanceof Error ? e.message : $t("plugin.uninstallFailed");
      if (isUnauthorized(e)) onAuthExpired();
    }
  }

  function isUnauthorized(e: unknown): boolean {
    return (e as { response?: { status?: number } })?.response?.status === 401;
  }
</script>

<div class="space-y-4">
  <UnderlineTabs.Root bind:value={activeTab}>
    <UnderlineTabs.List>
      <UnderlineTabs.Trigger value="installed">
        {$t("plugin.tabInstalled")} ({plugins.length})
      </UnderlineTabs.Trigger>
      <UnderlineTabs.Trigger value="market">{$t("plugin.tabMarket")}</UnderlineTabs.Trigger>
      <UnderlineTabs.Trigger value="upload">{$t("plugin.tabUpload")}</UnderlineTabs.Trigger>
    </UnderlineTabs.List>
  </UnderlineTabs.Root>

  {#if activeTab === "market"}
    <PluginMarketSettings {token} {onAuthExpired} onInstalled={refreshPlugins} />
  {:else if activeTab === "upload"}
    <Card.Root>
      <Card.Header>
        <Card.Title class="flex items-center gap-2">
          <PackageOpen />
          {$t("plugin.installTitle")}
        </Card.Title>
        <Card.Description>{$t("plugin.installDesc")}</Card.Description>
      </Card.Header>
      <Card.Content>
        <div class="flex items-center gap-2">
          <Input type="file" accept=".zip,application/zip" bind:files={installFiles} class="flex-1" />
          <Button onclick={handleInstall} disabled={uploading || !installFiles?.length}>
            <Download />
            {uploading ? $t("plugin.installing") : $t("plugin.install")}
          </Button>
        </div>
        {#if installError}
          <p class="text-sm text-destructive mt-2">{installError}</p>
        {/if}
      </Card.Content>
    </Card.Root>
  {:else}
    <Card.Root>
      <Card.Header>
        <Card.Title class="flex items-center gap-2">
          <PackageOpen />
          {$t("plugin.listTitle", { values: { count: plugins.length } })}
        </Card.Title>
        <Card.Description>
          {$t("plugin.listDescPrefix")}{#if pluginDir}<code class="font-mono break-all">{pluginDir}</code>{/if}
        </Card.Description>
        <Card.Action>
          <AsyncButton
            variant="ghost"
            size="sm"
            onclick={() => refreshPlugins(true)}
            disabled={loadingPlugins}
            loading={loadingPlugins}
            bind:success={refreshed}
            loadingLabel={$t("plugin.refreshing")}
            successLabel={$t("plugin.refreshed")}>
            <RefreshCw />
            {$t("plugin.refresh")}
          </AsyncButton>
        </Card.Action>
      </Card.Header>
      <Card.Content>
        {#if loadingPlugins && plugins.length === 0}
          <p class="text-sm text-muted-foreground py-4 text-center">{$t("plugin.loading")}</p>
        {:else if plugins.length === 0}
          <p class="text-sm text-muted-foreground py-4 text-center">{$t("plugin.empty")}</p>
        {:else}
          <div class="flex items-center gap-2 pb-3">
            {#each filterOptions as opt (opt.value)}
              <Button
                variant={pluginFilter === opt.value ? "default" : "outline"}
                size="sm"
                onclick={() => (pluginFilter = opt.value)}>
                {$t(opt.label)} ({filterCounts[opt.value]})
              </Button>
            {/each}
          </div>
          {#if filteredPlugins.length === 0}
            <p class="text-sm text-muted-foreground py-4 text-center">{$t("plugin.filterEmpty")}</p>
          {:else}
            <ul class="divide-y divide-border">
              {#each filteredPlugins as plugin (plugin.id)}
                <li class="py-3 flex items-start gap-3">
                  <div class="flex-1 min-w-0">
                    <div class="flex items-center gap-2">
                      <span class="font-medium text-foreground">{plugin.manifest.name}</span>
                      {#if plugin.manifest.version}
                        <span class="rounded bg-muted px-1.5 py-px text-xs font-medium">
                          v{plugin.manifest.version}
                        </span>
                      {/if}
                      {#if plugin.builtin}
                        <span class="rounded bg-muted text-foreground px-1.5 py-px text-xs font-medium"
                          >{$t("plugin.builtin")}</span>
                      {:else if isOfficialPlugin(plugin.id)}
                        <span class="rounded bg-primary/10 text-primary px-1.5 py-px text-xs font-medium"
                          >{$t("plugin.official")}</span>
                      {:else}
                        <span class="rounded bg-muted text-muted-foreground px-1.5 py-px text-xs font-medium"
                          >{$t("plugin.thirdParty")}</span>
                      {/if}
                      {#if plugin.manifest.display === "tab"}
                        <span class="rounded bg-accent text-accent-foreground px-1.5 py-px text-xs font-medium"
                          >{$t("plugin.tagTab")}</span>
                      {/if}
                    </div>
                    <p class="text-xs text-muted-foreground mt-0.5">
                      ID: <code class="font-mono">{plugin.id}</code>
                    </p>
                    {#if plugin.manifest.description}
                      <p class="text-sm text-muted-foreground mt-0.5">{plugin.manifest.description}</p>
                    {/if}
                    {#if plugin.manifest.platforms?.length}
                      <p class="text-xs text-muted-foreground mt-0.5">
                        {$t("plugin.platformsPrefix")}
                        {plugin.manifest.platforms.join(", ")}
                      </p>
                    {/if}
                  </div>
                  <div class="flex items-center gap-2 shrink-0">
                    <Switch
                      checked={plugin.enabled}
                      onCheckedChange={() => handleToggleEnabled(plugin)}
                      aria-label={plugin.enabled ? $t("plugin.disable") : $t("plugin.enable")} />
                    <Alert.Root>
                      <Alert.Trigger>
                        {#snippet child({ props })}
                          <Button
                            {...props}
                            size="icon"
                            variant="ghost"
                            disabled={plugin.builtin}
                            class={cn(
                              "text-destructive hover:text-destructive hover:bg-destructive/10",
                              plugin.builtin && "opacity-40",
                            )}>
                            <Trash2 />
                          </Button>
                        {/snippet}
                      </Alert.Trigger>
                      <Alert.Portal>
                        <Alert.Overlay />
                        <Alert.Content>
                          <Alert.Header>
                            <Alert.Title>{$t("plugin.uninstallTitle")}</Alert.Title>
                            <Alert.Description>
                              {$t("plugin.uninstallConfirm", { values: { name: plugin.manifest.name } })}
                              {#if uninstallError}
                                <span class="block mt-2 text-destructive">{uninstallError}</span>
                              {/if}
                            </Alert.Description>
                          </Alert.Header>
                          <Alert.Footer>
                            <Alert.Cancel>{$t("common.cancel")}</Alert.Cancel>
                            <Alert.Action
                              class="bg-destructive text-white hover:bg-destructive/90"
                              onclick={() => handleUninstall(plugin)}>
                              {$t("plugin.uninstall")}
                            </Alert.Action>
                          </Alert.Footer>
                        </Alert.Content>
                      </Alert.Portal>
                    </Alert.Root>
                  </div>
                </li>
              {/each}
            </ul>
          {/if}
        {/if}
      </Card.Content>
    </Card.Root>
  {/if}
</div>
