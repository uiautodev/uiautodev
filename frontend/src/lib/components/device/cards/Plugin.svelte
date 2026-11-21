<script lang="ts">
  import { baseURL } from "$lib/api/api.js";
  import * as Card from "$lib/components/ui/card/index.js";
  import { page } from "$app/state";
  import { ExternalLink, GripVertical, Info, RefreshCw } from "lucide-svelte";
  import { Popover } from "bits-ui";
  import { dragHandleZone, dragHandle, type DndEvent } from "svelte-dnd-action";
  import PluginIframe from "./PluginIframe.svelte";
  import { reconcileColumns, savePluginColumns } from "$lib/stores/plugin-order.svelte";
  import type { Plugin } from "$lib/api/schemas.js";
  import { t } from "svelte-i18n";

  let { plugins = [], count = $bindable(), ...restProps } = $props();
  const deviceId = page.params.id as string;

  let cardPlugins = $derived(plugins.filter((p: Plugin) => p.manifest.display !== "tab"));
  let windowSearchParams = new URLSearchParams(window.location.search);
  let refreshCounts = $state<Record<string, number>>({});

  const flipDurationMs = 200;
  const dndType = "plugin-card";

  function columnsToPlugins(): { left: Plugin[]; right: Plugin[] } {
    const columns = reconcileColumns(cardPlugins);
    const byId = new Map(cardPlugins.map((p) => [p.id, p]));
    return {
      left: columns.left.map((id) => byId.get(id)).filter((p): p is Plugin => !!p),
      right: columns.right.map((id) => byId.get(id)).filter((p): p is Plugin => !!p),
    };
  }

  const initialColumns = columnsToPlugins();
  let left = $state<Plugin[]>(initialColumns.left);
  let right = $state<Plugin[]>(initialColumns.right);

  $effect(() => {
    const columns = columnsToPlugins();
    left = columns.left;
    right = columns.right;
  });

  function handleConsider(side: "left" | "right", e: CustomEvent<DndEvent<Plugin>>) {
    if (side === "left") left = e.detail.items;
    else right = e.detail.items;
  }

  function handleFinalize(side: "left" | "right", e: CustomEvent<DndEvent<Plugin>>) {
    handleConsider(side, e);
    savePluginColumns({ left: left.map((p) => p.id), right: right.map((p) => p.id) });
  }

  function refreshPlugin(id: string) {
    refreshCounts[id] = (refreshCounts[id] ?? 0) + 1;
  }

  function getIframeSrc(plugin: Plugin) {
    const base = baseURL || window.location.origin;
    const url = new URL(plugin.src, base);
    url.searchParams.set("deviceId", deviceId as string);
    url.searchParams.set("_t", Date.now().toString());
    windowSearchParams.forEach((value, key) => {
      url.searchParams.set(key, value);
    });
    return url.toString();
  }

  $effect(() => {
    count = cardPlugins.length;
  });
</script>

{#if cardPlugins.length > 0}
  <div class="text-sm text-muted-foreground mb-2">
    如何使用AI写插件: <a
      href="https://github.com/uiautodev-plugins/template"
      target="_blank"
      rel="noopener noreferrer"
      class="text-blue-500 hover:text-blue-700">点击查看</a>
  </div>
{/if}

<div class="mx-1 flex items-stretch gap-3">
  <div
    class="flex min-w-0 flex-1 flex-col gap-3"
    use:dragHandleZone={{ items: left, type: dndType, flipDurationMs }}
    onconsider={(e) => handleConsider("left", e)}
    onfinalize={(e) => handleFinalize("left", e)}>
    {#each left as plugin (plugin.id)}
      {@render pluginCard(plugin)}
    {/each}
  </div>
  <div
    class="flex min-w-0 flex-1 flex-col gap-3"
    use:dragHandleZone={{ items: right, type: dndType, flipDurationMs }}
    onconsider={(e) => handleConsider("right", e)}
    onfinalize={(e) => handleFinalize("right", e)}>
    {#each right as plugin (plugin.id)}
      {@render pluginCard(plugin)}
    {/each}
  </div>
</div>

{#snippet pluginCard(plugin: Plugin)}
  <Card.Root {...restProps} class="gap-2">
    <Card.Header>
      <Card.Title class="flex items-center justify-between gap-2">
        <div class="flex items-center gap-2">
          <button
            type="button"
            use:dragHandle
            aria-label={$t("plugin.dragToSort")}
            title={$t("plugin.dragToSort")}
            class="inline-flex items-center text-muted-foreground hover:text-foreground transition-colors cursor-grab active:cursor-grabbing">
            <GripVertical size={14} />
          </button>
          {plugin.manifest?.displayName || plugin.manifest.name}
          {#if plugin.manifest.version}
            <span class="rounded bg-muted px-1.5 py-px text-xs font-medium">
              v{plugin.manifest.version}
            </span>
          {/if}
        </div>
        <div class="flex items-center gap-1">
          <button
            type="button"
            aria-label={$t("plugin.refresh")}
            onclick={() => refreshPlugin(plugin.id)}
            title={$t("plugin.refresh")}
            class="inline-flex items-center text-muted-foreground hover:text-foreground transition-colors cursor-pointer">
            <RefreshCw size={14} />
          </button>
          <Popover.Root>
            <Popover.Trigger
              class="inline-flex items-center text-muted-foreground hover:text-foreground transition-colors cursor-pointer">
              <Info size={14} />
            </Popover.Trigger>
            <Popover.Content
              class="z-50 w-72 rounded-md border bg-popover p-4 text-popover-foreground shadow-md outline-none"
              side="bottom"
              sideOffset={4}>
              <div class="space-y-1.5 text-sm font-normal">
                <a
                  href={getIframeSrc(plugin)}
                  target="_blank"
                  rel="noopener noreferrer"
                  class="inline-flex items-center gap-1 text-muted-foreground hover:text-foreground transition-colors">
                  <ExternalLink size={12} />
                  {$t("plugin.openInNewTab")}
                </a>
                <p class="font-medium text-foreground">{plugin.manifest?.displayName || plugin.manifest.name}</p>
                {#if plugin.manifest.version}
                  <p class="text-xs text-muted-foreground">{$t("plugin.version")} v{plugin.manifest.version}</p>
                {/if}
                {#if plugin.manifest.description}
                  <p class="font-medium text-foreground">{$t("plugin.description")}</p>
                  <p class="text-muted-foreground">{plugin.manifest.description}</p>
                {/if}

                {#if plugin.manifest.homepage}
                  <p class="font-medium text-foreground">{$t("plugin.homepage")}</p>
                  <a
                    href={plugin.manifest.homepage}
                    target="_blank"
                    rel="noopener noreferrer"
                    class="inline-flex items-center gap-1 text-muted-foreground hover:text-foreground transition-colors underline">
                    {plugin.manifest.homepage}
                  </a>
                {/if}
              </div>
            </Popover.Content>
          </Popover.Root>
        </div>
      </Card.Title>
    </Card.Header>
    <Card.Content>
      {#key refreshCounts[plugin.id] ?? 0}
        <PluginIframe {plugin} {deviceId} class="w-full" />
      {/key}
    </Card.Content>
  </Card.Root>
{/snippet}
