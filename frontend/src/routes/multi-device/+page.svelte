<script lang="ts">
  import { api, deleteDevicePresence, deleteDevicePresenceKeepalive, putPresenceBatch } from "$lib/api/api";
  import type { APIDevice, Occupancy } from "$lib/api/schemas";
  import Slider from "$lib/components/ui/slider/slider.svelte";
  import * as Resizable from "$lib/components/ui/resizable/index.js";
  import { onMount, onDestroy } from "svelte";
  import { t } from "svelte-i18n";
  import AppHeader from "$lib/components/AppHeader.svelte";
  import ScreenAndroid from "$lib/components/scrcpy/screen-android.svelte";
  import PresenceBadge from "$lib/components/device/PresenceBadge.svelte";
  import AvatarStack from "$lib/components/AvatarStack.svelte";
  import { identityStore } from "$lib/stores/identity.svelte";
  import { Button } from "$lib/components/ui/button/index.js";
  import { Eye } from "lucide-svelte";

  const refreshIntervalMs = 5_000;
  const heartbeatIntervalMs = 30_000;

  async function fetchDevices(): Promise<APIDevice[]> {
    const res = await api.get("/api/v1/devices", {
      params: { platform: "android" },
    });
    return res.data;
  }

  let devices: APIDevice[] = $state([]);
  let deviceIds: string[] = $state([]);
  let activatedDeviceId: string | null = $state(null);
  let colCount = $state(4);
  let occupancy: Record<string, Occupancy> = $state({});
  // Devices the user switched to observing. The 30s heartbeat must renew them
  // in view mode: asking for control again would return busy and flip the
  // running stream back to the placeholder.
  let observedIds: Record<string, true> = $state({});
  let timers: Array<ReturnType<typeof setInterval>> = [];

  const mineId = $derived(identityStore.me?.id ?? "");

  function modeFor(deviceId: string): "control" | "view" {
    return observedIds[deviceId] ? "view" : "control";
  }

  async function refreshPresence() {
    const known = new Set(devices.map((device) => device.id));
    const ids = new Set(Array.from(deviceIds).filter((id) => known.has(id)));
    if (activatedDeviceId && known.has(activatedDeviceId)) ids.add(activatedDeviceId);
    if (ids.size === 0) return;
    try {
      const entries = Object.fromEntries(
        Array.from(ids).map((id) => [id, { mode: modeFor(id) as "control" | "view" }]),
      );
      occupancy = { ...occupancy, ...(await putPresenceBatch(entries)) };
    } catch (e) {
      console.error("failed to refresh group presence", e);
    }
  }

  async function observe(deviceId: string) {
    observedIds[deviceId] = true;
    try {
      const result = await putPresenceBatch({ [deviceId]: { mode: "view" } });
      occupancy = { ...occupancy, ...result };
    } catch (e) {
      console.error("failed to observe device", e);
    }
  }

  async function activate(deviceId: string) {
    delete observedIds[deviceId];
    activatedDeviceId = deviceId;
    await refreshPresence();
  }

  function toggleAll(event: Event) {
    const checked = (event.target as HTMLInputElement).checked;
    if (checked) {
      deviceIds = devices.map((d) => d.id);
    } else {
      deviceIds = [];
    }
  }

  function isBusy(deviceId: string): boolean {
    return occupancy[deviceId]?.state === "busy";
  }

  // Releasing sessions for unchecked devices keeps the devices available to
  // others instead of waiting for the idle timeout.
  let previousSelection: string[] = [];
  $effect(() => {
    const current = [...deviceIds];
    const removed = previousSelection.filter((id) => !current.includes(id));
    previousSelection = current;
    for (const id of removed) void deleteDevicePresence(id).catch(() => {});
    for (const id of removed) delete observedIds[id];
  });

  onMount(async () => {
    void identityStore.ensure();
    devices = await fetchDevices();
    await refreshPresence();
    timers.push(
      setInterval(async () => {
        devices = await fetchDevices();
      }, refreshIntervalMs),
      setInterval(() => {
        if (typeof document !== "undefined" && document.hidden) return;
        void refreshPresence();
      }, heartbeatIntervalMs),
    );
  });

  onDestroy(() => {
    for (const timer of timers) clearInterval(timer);
    timers = [];
    for (const id of Array.from(new Set(deviceIds))) {
      deleteDevicePresenceKeepalive(id);
    }
  });
</script>

{#snippet myphone(device: APIDevice)}
  <div class="inline-block p-2 relative" style={`width: calc(1/${colCount}*100%);`}>
    {#if isBusy(device.id)}
      <div
        class="aspect-[9/16] w-full rounded-md border border-border bg-muted/60 flex flex-col items-center justify-center gap-2 p-2">
        <AvatarStack occupants={occupancy[device.id]?.occupants ?? []} {mineId} size={24} max={3} />
        <span class="text-center text-xs text-muted-foreground">{$t("presence.state.busy")}</span>
        {#if occupancy[device.id]?.can_observe}
          <Button size="sm" variant="outline" onclick={() => observe(device.id)}>
            <Eye class="size-4 mr-1" />
            {$t("presence.observeOnly")}
          </Button>
        {/if}
      </div>
    {:else}
      {#if activatedDeviceId === device.id}
        <div class="opacity-50 pointer-events-none absolute inset-0 bg-muted flex items-center justify-center z-10">
          {$t("multi.using")}
        </div>
      {/if}
      <ScreenAndroid
        maxFps={5}
        maxSize={512}
        deviceId={device.id}
        control={false}
        onclick={() => void activate(device.id)} />
    {/if}
  </div>
{/snippet}

<div class="flex flex-col h-full bg-background">
  <AppHeader active="groupControl" />
  <div class="flex flex-1 min-h-0 overflow-hidden">
    <aside class="w-64 bg-background border-r border-border overflow-y-auto shadow-sm">
      <div class="p-4">
        <div class="mb-4 pb-3 border-b border-border">
          <label
            class="flex items-center gap-2 text-sm font-medium text-foreground cursor-pointer hover:text-foreground transition-colors">
            <input
              type="checkbox"
              checked={deviceIds.length === devices.length && devices.length > 0}
              onchange={toggleAll}
              class="w-4 h-4 accent-primary rounded border-input focus:ring-2 focus:ring-ring cursor-pointer" />
            <span>{$t("multi.selectAll", { values: { count: devices.length } })}</span>
          </label>
        </div>
        <div class="space-y-2">
          {#each devices as device}
            <div class="rounded-lg p-3 hover:bg-muted transition-colors">
              <label class="flex items-start gap-2 cursor-pointer">
                <input
                  type="checkbox"
                  value={device.id}
                  bind:group={deviceIds}
                  class="w-4 h-4 accent-primary rounded border-input focus:ring-2 focus:ring-ring cursor-pointer mt-0.5" />
                <div class="flex-1">
                  <div class="text-sm font-medium text-foreground">
                    {device.props.model}
                  </div>
                  <div class="text-xs text-muted-foreground mt-0.5">
                    {device.id}
                  </div>
                  {#if device.occupancy}
                    <div class="mt-1">
                      <PresenceBadge occupancy={device.occupancy} {mineId} showAvatars={false} />
                    </div>
                  {/if}
                </div>
              </label>
            </div>
          {/each}
        </div>
      </div>
    </aside>

    <main class="flex-1 overflow-auto bg-muted/40">
      {#if deviceIds.length === 0}
        <div class="flex items-center justify-center h-full">
          <div class="text-center">
            <div class="text-muted-foreground mb-2">
              <svg class="w-16 h-16 mx-auto" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  stroke-width="2"
                  d="M12 18h.01M8 21h8a2 2 0 002-2V5a2 2 0 00-2-2H8a2 2 0 00-2 2v14a2 2 0 002 2z" />
              </svg>
            </div>
            <p class="text-muted-foreground text-lg font-medium">{$t("multi.noSelected")}</p>
            <p class="text-muted-foreground/70 text-sm mt-1">{$t("multi.selectHint")}</p>
          </div>
        </div>
      {:else}
        <Resizable.PaneGroup direction="horizontal" class="h-full">
          <Resizable.Pane>
            {#if activatedDeviceId}
              {#key activatedDeviceId}
                {#if isBusy(activatedDeviceId)}
                  <div class="flex h-full w-full flex-col items-center justify-center gap-3">
                    <AvatarStack occupants={occupancy[activatedDeviceId]?.occupants ?? []} {mineId} size={32} max={4} />
                    <span class="text-sm text-muted-foreground">{$t("presence.state.busy")}</span>
                  </div>
                {:else}
                  <ScreenAndroid
                    maxFps={30}
                    maxSize={1024}
                    deviceId={activatedDeviceId}
                    control={!observedIds[activatedDeviceId]}
                    class="h-full w-full" />
                {/if}
              {/key}
            {/if}
          </Resizable.Pane>
          <Resizable.Handle withHandle />
          <Resizable.Pane>
            <div class="flex items-center ml-2 gap-4">
              <Slider type="single" bind:value={colCount} min={1} max={10} step={1} class="w-100" />
              {colCount}
            </div>
            <div class="flex flex-wrap relative">
              {#each Array.from(deviceIds) as deviceId}
                {@const device = devices.find((d) => d.id === deviceId)}
                {#if device}
                  {@render myphone(device)}
                {/if}
              {/each}
            </div>
          </Resizable.Pane>
        </Resizable.PaneGroup>
      {/if}
    </main>
  </div>
</div>
