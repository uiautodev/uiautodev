<script lang="ts">
  import { Button } from "$lib/components/ui/button/index.js";
  import { onMount } from "svelte";
  import { api, subscribeDeviceList } from "$lib/api/api.js";
  import type { APIDevice, DeviceMeta } from "$lib/api/schemas.js";
  import { RefreshCcw, Mouse, Eye } from "lucide-svelte";
  import * as UnderlineTabs from "$lib/components/ui/underline-tabs";
  import * as Popover from "$lib/components/ui/popover/index.js";
  import * as Select from "$lib/components/ui/select/index.js";
  import AndroidIcon from "$lib/components/icons/android.svelte";
  import AppleIcon from "$lib/components/icons/apple.svelte";
  import HarmonyIcon from "$lib/components/icons/harmony.svelte";
  import DemoIcon from "$lib/components/icons/demo.svelte";
  import { t } from "svelte-i18n";
  import AppHeader from "$lib/components/AppHeader.svelte";
  import DeviceRemark from "$lib/components/device/DeviceRemark.svelte";
  import DeviceIosSettings from "$lib/components/device/DeviceIosSettings.svelte";
  import PresenceBadge from "$lib/components/device/PresenceBadge.svelte";
  import { identityStore } from "$lib/stores/identity.svelte";

  let devices = $state<APIDevice[]>([]);
  let loading = $state(true);
  let error = $state<string | null>(null);
  let inFlight = false;
  const PLATFORM_STORAGE_KEY = "uiautodev-device-platform";
  const PLATFORMS = ["android", "ios", "harmony", "demo"] as const;

  function getStoredPlatform(): string {
    if (typeof window === "undefined") return "android";
    try {
      const stored = window.localStorage.getItem(PLATFORM_STORAGE_KEY);
      return stored && PLATFORMS.includes(stored as (typeof PLATFORMS)[number]) ? stored : "android";
    } catch {
      return "android";
    }
  }

  let platformFilter = $state(getStoredPlatform());
  let nodeFilter = $state("all");

  $effect(() => {
    try {
      window.localStorage.setItem(PLATFORM_STORAGE_KEY, platformFilter);
    } catch {
      // Ignore storage write failures (quota / private mode / disabled storage)
    }
  });

  const nodeOptions = $derived.by(() => {
    const nodes = new Map<string, string>();
    for (const device of devices) {
      if (device.slave_id) nodes.set(device.slave_id, device.slave_name || device.slave_id);
    }
    return [...nodes.entries()];
  });

  const visibleDevices = $derived(
    nodeFilter === "all" ? devices : devices.filter((device) => device.slave_id === nodeFilter),
  );

  const selectedNode = $derived(nodeOptions.find(([id]) => id === nodeFilter));
  const mineId = $derived(identityStore.me?.id ?? "");

  const PLATFORM_LABEL_KEYS: Record<string, string> = {
    android: "home.platform.android",
    ios: "home.platform.ios",
    harmony: "home.platform.harmony",
    demo: "home.platform.demo",
  };

  function platformLabel(platform: string | undefined): string {
    if (!platform) return $t("home.platform.unknown");
    const key = PLATFORM_LABEL_KEYS[platform];
    return key ? $t(key) : platform;
  }

  function isDeviceOnline(device: APIDevice): boolean {
    if (device.disabled) return false;
    return device.remote ? device.slave_online === true : true;
  }

  function applyIncremental(next: APIDevice[]): APIDevice[] {
    const existing = new Map(devices.map((d) => [d.id, d]));
    return next.map((d) => {
      const prev = existing.get(d.id);
      if (!prev) return d;
      if (JSON.stringify(prev) === JSON.stringify(d)) return prev;
      return { ...prev, ...d };
    });
  }

  function handleMetaSaved(deviceId: string, meta: DeviceMeta) {
    devices = devices.map((d) => (d.id === deviceId ? { ...d, meta } : d));
  }

  async function refresh() {
    if (inFlight) return;
    inFlight = true;
    try {
      const res = await api.get("/api/v1/devices", {
        params: { platform: platformFilter },
      });
      devices = applyIncremental(res.data);
      error = null;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      inFlight = false;
      loading = false;
    }
  }

  onMount(() => {
    return () => {
      if (popTimer) clearTimeout(popTimer);
    };
  });

  // The backend pushes the device list over a WebSocket and only sends a
  // message when it changed, so the page does not poll the HTTP endpoint.
  $effect(() => {
    const platform = platformFilter;
    loading = true;
    return subscribeDeviceList(platform, {
      onDevices: (next) => {
        devices = applyIncremental(next);
        error = null;
        loading = false;
      },
      onError: (message) => {
        error = message;
        loading = false;
      },
    });
  });

  let popOpen = $state(false);
  let popText = $state("");
  let popTimer: ReturnType<typeof setTimeout> | undefined;

  function showPop(text: string) {
    popText = text;
    popOpen = true;
    if (popTimer) clearTimeout(popTimer);
    popTimer = setTimeout(() => (popOpen = false), 1000);
  }

  async function handleManualRefresh() {
    await refresh();
    showPop(error ? $t("common.failed") : $t("common.confirmed"));
  }
</script>

{#snippet listDevices(devices: APIDevice[])}
  {#if devices.length === 0}
    <div class="bg-white dark:bg-slate-800 rounded-lg shadow-lg p-12 text-center">
      <svg
        xmlns="http://www.w3.org/2000/svg"
        class="h-16 w-16 mx-auto text-slate-400 mb-4"
        fill="none"
        viewBox="0 0 24 24"
        stroke="currentColor">
        <path
          stroke-linecap="round"
          stroke-linejoin="round"
          stroke-width="2"
          d="M12 18h.01M8 21h8a2 2 0 002-2V5a2 2 0 00-2-2H8a2 2 0 00-2 2v14a2 2 0 002 2z" />
      </svg>
      <h3 class="text-xl font-semibold text-slate-700 dark:text-slate-300 mb-2">{$t("home.noDevices")}</h3>
      <p class="text-slate-500 dark:text-slate-400">{$t("home.connectDevice")}</p>
    </div>
  {:else}
    <div
      class="bg-white dark:bg-slate-800 rounded-lg shadow-lg overflow-hidden border border-slate-200 dark:border-slate-700">
      <div class="overflow-x-auto">
        <table class="w-full">
          <thead class="bg-slate-100 dark:bg-slate-700 border-b border-slate-200 dark:border-slate-600">
            <tr>
              <th
                class="px-4 py-3 text-left text-xs font-semibold text-slate-700 dark:text-slate-300 uppercase tracking-wider w-12">
                {$t("home.table.index")}
              </th>
              <th
                class="px-4 py-3 text-left text-xs font-semibold text-slate-700 dark:text-slate-300 uppercase tracking-wider">
                {$t("home.table.platform")}
              </th>
              <th
                class="px-4 py-3 text-left text-xs font-semibold text-slate-700 dark:text-slate-300 uppercase tracking-wider">
                {$t("home.table.serial")}
              </th>
              <th
                class="px-4 py-3 text-left text-xs font-semibold text-slate-700 dark:text-slate-300 uppercase tracking-wider">
                {$t("home.table.model")}
              </th>
              <th
                class="px-4 py-3 text-left text-xs font-semibold text-slate-700 dark:text-slate-300 uppercase tracking-wider">
                {$t("home.table.product")}
              </th>
              <th
                class="px-4 py-3 text-left text-xs font-semibold text-slate-700 dark:text-slate-300 uppercase tracking-wider">
                {$t("home.table.remark")}
              </th>
              <th
                class="px-4 py-3 text-left text-xs font-semibold text-slate-700 dark:text-slate-300 uppercase tracking-wider">
                {$t("home.table.status")}
              </th>
              <th
                class="px-4 py-3 text-left text-xs font-semibold text-slate-700 dark:text-slate-300 uppercase tracking-wider">
                {$t("home.table.occupancy")}
              </th>
              <th
                class="px-4 py-3 text-right text-xs font-semibold text-slate-700 dark:text-slate-300 uppercase tracking-wider sticky right-0 bg-slate-100 dark:bg-slate-700">
                {$t("home.table.actions")}
              </th>
            </tr>
          </thead>
          <tbody class="divide-y divide-slate-200 dark:divide-slate-700">
            {#each visibleDevices as device, index (device.id)}
              <tr class="hover:bg-slate-50 dark:hover:bg-slate-700/50 transition-colors">
                <td class="px-4 py-4 whitespace-nowrap text-sm text-slate-500 dark:text-slate-400">
                  {index + 1}
                </td>
                <td class="px-4 py-4 whitespace-nowrap">
                  <div class="flex items-center gap-1.5">
                    <span
                      class="inline-flex w-fit items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-green-100 dark:bg-green-900 text-green-800 dark:text-green-200">
                      {platformLabel(device.platform)}
                    </span>
                    {#if device.slave_id}
                      <span
                        class="inline-flex w-fit items-center rounded-full bg-blue-100 px-2 py-0.5 text-[10px] font-medium text-blue-800 dark:bg-blue-950 dark:text-blue-200">
                        {device.slave_id}
                      </span>
                    {/if}
                    {#if device.slave_id && (device.chain?.length ?? 0) > 2}
                      <span
                        class="inline-flex w-fit items-center rounded-full bg-muted px-2 py-0.5 text-[10px] font-medium text-muted-foreground"
                        title={device.chain?.join(" → ")}>
                        {$t("home.nested")}
                      </span>
                    {/if}
                  </div>
                </td>
                <td class="px-4 py-4 whitespace-nowrap">
                  <span
                    class="text-sm font-mono text-slate-900 dark:text-slate-100 bg-slate-100 dark:bg-slate-700 px-2 py-1 rounded">
                    {device.props?.serial || "N/A"}
                  </span>
                </td>
                <td class="px-4 py-4 whitespace-nowrap text-sm text-slate-700 dark:text-slate-300">
                  {device.props?.model || "N/A"}
                </td>
                <td class="px-4 py-4 whitespace-nowrap text-sm text-slate-700 dark:text-slate-300">
                  {device.props?.product || "N/A"}
                </td>
                <td class="px-4 py-4 max-w-[220px]">
                  <DeviceRemark deviceId={device.id} meta={device.meta} onsaved={handleMetaSaved} />
                </td>
                <td class="px-4 py-4 whitespace-nowrap">
                  <span class="inline-flex items-center">
                    <span class="mr-2 h-2 w-2 rounded-full {isDeviceOnline(device) ? 'bg-green-500' : 'bg-red-500'}"
                    ></span>
                    <span class="text-sm text-slate-600 dark:text-slate-400">
                      {isDeviceOnline(device) ? $t("home.status.connected") : $t("home.status.offline")}
                    </span>
                  </span>
                </td>
                <td class="px-4 py-4 whitespace-nowrap">
                  <PresenceBadge occupancy={device.occupancy} {mineId} />
                </td>
                <td class="px-4 py-4 whitespace-nowrap text-right sticky right-0 bg-white dark:bg-slate-800">
                  <div class="flex items-center justify-end gap-1">
                    {#if device.platform === "ios"}
                      <DeviceIosSettings deviceId={device.id} meta={device.meta} onsaved={handleMetaSaved} />
                    {/if}
                    {#if device.occupancy?.can_observe && device.occupancy?.in_use && !device.occupancy?.mine}
                      <Button
                        href={`/device/${device.id}?mode=view`}
                        size="sm"
                        variant="outline"
                        class="text-xs"
                        disabled={!isDeviceOnline(device)}>
                        <Eye />
                        {$t("presence.observeOnly")}
                      </Button>
                    {/if}
                    <Button href={`/device/${device.id}`} size="sm" class="text-xs" disabled={!isDeviceOnline(device)}>
                      <Mouse />
                      {$t("home.control")}
                    </Button>
                  </div>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>

      <!-- Device Count Footer -->
      <div class="bg-slate-50 dark:bg-slate-700/60 px-6 py-3 border-t border-slate-200 dark:border-slate-700">
        <p class="text-sm text-slate-600 dark:text-slate-400">
          {$t("home.total", { values: { count: visibleDevices.length } })}
        </p>
      </div>
    </div>
  {/if}
{/snippet}

<div class="flex flex-col h-full bg-gradient-to-br from-slate-50 to-slate-100 dark:from-slate-900 dark:to-slate-800">
  <AppHeader active="device" />
  <main class="flex-1 min-h-0 overflow-y-auto">
    <div class="container mx-auto px-4 py-6 max-w-4xl">
      <div class="flex items-center gap-2 mb-2">
        <UnderlineTabs.Root bind:value={platformFilter} class="flex-1 min-w-0">
          <UnderlineTabs.List>
            <UnderlineTabs.Trigger value="android">
              <AndroidIcon size={32} active={platformFilter === "android"} />
              {$t("home.platform.android")}
            </UnderlineTabs.Trigger>
            <UnderlineTabs.Trigger value="ios">
              <AppleIcon active={platformFilter === "ios"} />
              {$t("home.platform.ios")}
            </UnderlineTabs.Trigger>
            <UnderlineTabs.Trigger value="harmony">
              <HarmonyIcon size={32} active={platformFilter === "harmony"} />
              {$t("home.platform.harmony")}
            </UnderlineTabs.Trigger>
            <UnderlineTabs.Trigger value="demo">
              <DemoIcon size={32} active={platformFilter === "demo"} />
              {$t("home.platform.demo")}
            </UnderlineTabs.Trigger>
          </UnderlineTabs.List>
        </UnderlineTabs.Root>
        {#if nodeOptions.length > 0}
          <Select.Root type="single" value={nodeFilter} onValueChange={(value) => (nodeFilter = value)}>
            <Select.Trigger size="sm" class="w-64 shrink-0">
              {#if selectedNode}
                <span class="shrink-0 font-mono text-xs text-muted-foreground">{selectedNode[0]}</span>
                <span class="min-w-0 flex-1 truncate">{selectedNode[1]}</span>
              {:else}
                {$t("home.allNodes")}
              {/if}
            </Select.Trigger>
            <Select.Content class="min-w-72">
              <Select.Item value="all">{$t("home.allNodes")}</Select.Item>
              {#each nodeOptions as [id, name] (id)}
                <Select.Item value={id}>
                  <span class="shrink-0 font-mono text-xs text-muted-foreground">{id}</span>
                  <span class="min-w-0 flex-1 truncate">{name}</span>
                </Select.Item>
              {/each}
            </Select.Content>
          </Select.Root>
        {/if}
        <Popover.Root bind:open={popOpen}>
          <Popover.Trigger>
            {#snippet child({ props })}
              <Button {...props} variant="outline" size="sm" onclick={handleManualRefresh}>
                <RefreshCcw class="size-4 mr-1" />
                {$t("common.refresh")}
              </Button>
            {/snippet}
          </Popover.Trigger>
          <Popover.Content side="top" class="w-auto px-3 py-1.5 text-xs">
            {popText}
          </Popover.Content>
        </Popover.Root>
      </div>
      <!-- Devices List -->
      {#if loading}
        <div class="bg-white dark:bg-slate-800 rounded-lg shadow-lg p-8 text-center">
          <div
            class="inline-block animate-spin rounded-full h-12 w-12 border-b-2 border-slate-900 dark:border-slate-100">
          </div>
          <p class="mt-4 text-slate-600 dark:text-slate-400">{$t("home.loading")}</p>
        </div>
      {:else if error}
        <div class="bg-red-50 dark:bg-red-900/20 rounded-lg shadow-lg p-8 border border-red-200 dark:border-red-800">
          <div class="flex items-start gap-4">
            <svg
              xmlns="http://www.w3.org/2000/svg"
              class="h-6 w-6 text-red-600 dark:text-red-400 flex-shrink-0 mt-0.5"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor">
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                stroke-width="2"
                d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            <div>
              <h3 class="text-lg font-semibold text-red-800 dark:text-red-200 mb-1">{$t("home.errorTitle")}</h3>
              <p class="text-red-700 dark:text-red-300">
                {error}
              </p>
            </div>
          </div>
        </div>
      {:else}
        {@render listDevices(devices)}
      {/if}
    </div>
  </main>
</div>
