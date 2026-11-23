<script lang="ts">
  import * as Tabs from "$lib/components/ui/tabs/index.js";
  import Separator from "../../ui/separator/separator.svelte";
  import CurrentApp from "../cards/CurrentApp.svelte";
  import Clipboard from "../cards/Clipboard.svelte";
  import AutoGLM from "../cards/AutoGLM.svelte";
  import Plugin from "../cards/Plugin.svelte";
  import PluginIframe from "../cards/PluginIframe.svelte";
  import { useLocalStorage } from "$lib/utils/localstorage.svelte";
  import { deviceStore } from "$lib/stores/device.svelte";
  import Inspect from "../cards/inspect/Inspect.svelte";
  import MCP from "$lib/components/mcp/MCP.svelte";
  import Terminal from "../cards/Terminal.svelte";
  import TabContent from "./TabContent.svelte";
  import { api } from "$lib/api/api.js";
  import type { Plugin as PluginData } from "$lib/api/schemas.js";
  import { t } from "svelte-i18n";
  import { onMount } from "svelte";

  let { deviceId, platform, remote = false, ...restProps } = $props();

  // The interactive terminal relies on adb shell v2, which only local Android
  // devices speak; remote cluster devices would need a tunnel.
  const terminalEnabled = $derived(platform === "android" && !remote);

  // define cards here so page doesn't need to manage them
  const cards = [CurrentApp, Clipboard];
  let tabValue = useLocalStorage("device-cards-tab", "inspect");

  // 从屏幕或元素树选中元素时，自动切换到 inspect 标签
  $effect(() => {
    if (deviceStore.nodeSelected) {
      tabValue.current = "inspect";
    }
  });

  let allPlugins: PluginData[] = $state([]);
  let cardPlugins = $derived(allPlugins.filter((p) => p.manifest.display !== "tab"));
  let tabPlugins = $derived(allPlugins.filter((p) => p.manifest.display === "tab"));

  async function loadPlugins() {
    const query = new URLSearchParams({ platform, enabled: "enabled" });
    const res = await api.get(`/api/v1/plugins?${query.toString()}`);
    allPlugins = res.data ?? [];
  }

  onMount(() => {
    loadPlugins().catch((e) => console.error("failed to load plugins", e));
  });
</script>

<div class="card-container" {...restProps}>
  <Tabs.Root bind:value={tabValue.current} class="background-white h-full flex flex-col">
    <Tabs.List>
      <Tabs.Trigger value="inspect" class="w-30">{$t("device.tabs.inspect")}</Tabs.Trigger>
      <Tabs.Trigger disabled={!terminalEnabled} value="terminal" class="w-30"
        >{$t("device.tabs.terminal")}</Tabs.Trigger>
      <Tabs.Trigger value="mcp" class="w-30">{$t("device.tabs.mcp")}</Tabs.Trigger>
      <Tabs.Trigger disabled={platform === "ios" || platform === "demo"} value="common" class="w-30"
        >{$t("device.tabs.common")}</Tabs.Trigger>
      <Tabs.Trigger value="plugin" class="w-30">{$t("device.tabs.plugin")}</Tabs.Trigger>
      {#each tabPlugins as plugin}
        <Tabs.Trigger value={"plugin-tab-" + plugin.id} class="relative">
          {plugin.manifest.name}
          <span
            class="absolute -top-1 -right-1 text-[8px] bg-emerald-100 text-emerald-700 px-1 rounded-full leading-none"
            >{$t("device.tabs.pluginBadge")}</span>
        </Tabs.Trigger>
      {/each}
    </Tabs.List>
    <Separator class="mb-2" />
    <TabContent value="plugin">
      <Plugin plugins={cardPlugins} />
    </TabContent>
    <TabContent value="common">
      {#each cards as Card}
        <Card {deviceId} {platform} class="mb-2" />
      {/each}
    </TabContent>
    <TabContent value="ai">
      <AutoGLM />
    </TabContent>
    <TabContent value="inspect">
      <Inspect />
    </TabContent>
    <TabContent value="terminal">
      <Terminal {deviceId} active={tabValue.current === "terminal"} class="h-full" />
    </TabContent>
    <TabContent value="mcp">
      <MCP />
    </TabContent>
    {#each tabPlugins as plugin}
      <TabContent value={"plugin-tab-" + plugin.id}>
        <PluginIframe {plugin} {deviceId} class="w-full h-full" />
      </TabContent>
    {/each}
  </Tabs.Root>
</div>

<style>
  .card-container {
    container-type: inline-size;
  }
  @container (width >= 800px) {
  }
</style>
