<script lang="ts">
  import { api, baseWSURL } from "$lib/api/api";
  import type { APIDevice } from "$lib/api/schemas";
  import Slider from "$lib/components/ui/slider/slider.svelte";
  import * as Resizable from "$lib/components/ui/resizable/index.js";
  import { onMount } from "svelte";
  import { t } from "svelte-i18n";
  import AppHeader from "$lib/components/AppHeader.svelte";
  import ScreenAndroid from "$lib/components/scrcpy/screen-android.svelte";

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

  function toggleAll(event: Event) {
    const checked = (event.target as HTMLInputElement).checked;
    if (checked) {
      deviceIds = devices.map((d) => d.id);
    } else {
      deviceIds = [];
    }
  }

  $effect(() => {
    console.log("Checked devices:", Array.from(deviceIds));
  });

  onMount(async () => {
    devices = await fetchDevices();
  });
</script>

{#snippet myphone(deviceId: string)}
  <div class="inline-block p-2 relative" style={`width: calc(1/${colCount}*100%);`}>
    {#if activatedDeviceId === deviceId}
      <div class="opacity-50 pointer-events-none absolute inset-0 bg-muted flex items-center justify-center z-10">
        {$t("multi.using")}
      </div>
    {/if}
    <ScreenAndroid maxFps={5} maxSize={512} {deviceId} control={false} onclick={() => (activatedDeviceId = deviceId)} />
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
                <ScreenAndroid
                  maxFps={30}
                  maxSize={1024}
                  deviceId={activatedDeviceId}
                  control={true}
                  class="h-full w-full" />
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
                {@render myphone(deviceId)}
              {/each}
            </div>
          </Resizable.Pane>
        </Resizable.PaneGroup>
      {/if}
    </main>
  </div>
</div>
