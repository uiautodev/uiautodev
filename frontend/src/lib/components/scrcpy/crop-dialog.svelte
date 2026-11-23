<script lang="ts">
  import * as AlertDialog from "$lib/components/ui/alert-dialog/index.js";
  import Input from "$lib/components/ui/input/input.svelte";
  import { Button } from "$lib/components/ui/button";
  import { getWindowSize } from "$lib/api/api.js";

  interface Props {
    active: boolean;
    deviceId: string;
    initialCrop?: string;
    onapply?: (crop: string) => void;
    onclear?: () => void;
  }

  let { active = $bindable(false), deviceId, initialCrop = "", onapply, onclear }: Props = $props();

  let deviceWidth = $state(0);
  let deviceHeight = $state(0);
  let loading = $state(false);

  async function fetchDeviceSize() {
    loading = true;
    try {
      const size = await getWindowSize(deviceId);
      if (size?.width && size?.height) {
        deviceWidth = Number(size.width);
        deviceHeight = Number(size.height);
      }
    } catch (e) {
      console.warn("[crop-dialog] Failed to fetch window size:", e);
    } finally {
      loading = false;
    }
  }

  let customValue = $state(initialCrop);

  const hw = $derived(Math.round(deviceWidth / 2));
  const hh = $derived(Math.round(deviceHeight / 2));

  const presets = $derived([
    { label: "Left Half", value: `${hw}:${deviceHeight}:0:0` },
    { label: "Right Half", value: `${hw}:${deviceHeight}:${deviceWidth - hw}:0` },
    { label: "Top Half", value: `${deviceWidth}:${hh}:0:0` },
    { label: "Bottom Half", value: `${deviceWidth}:${hh}:0:${deviceHeight - hh}` },
  ]);

  function isValidCrop(v: string): boolean {
    const parts = v.split(":").map(Number);
    if (parts.length !== 4) return false;
    const [w, h, x, y] = parts;
    return (
      Number.isInteger(w) &&
      w > 0 &&
      Number.isInteger(h) &&
      h > 0 &&
      Number.isInteger(x) &&
      x >= 0 &&
      x < deviceWidth &&
      Number.isInteger(y) &&
      y >= 0 &&
      y < deviceHeight &&
      x + w <= deviceWidth &&
      y + h <= deviceHeight
    );
  }

  function applyPreset(value: string) {
    customValue = value;
  }

  function apply() {
    if (isValidCrop(customValue)) {
      onapply?.(customValue);
    }
    active = false;
  }

  function clear() {
    onclear?.();
    active = false;
  }

  // Sync initialCrop and fetch device size when dialog opens
  $effect(() => {
    if (active) {
      customValue = initialCrop;
      fetchDeviceSize();
    }
  });
</script>

<AlertDialog.Root bind:open={active}>
  <AlertDialog.Content>
    <AlertDialog.Header>
      <AlertDialog.Title>Crop Screen</AlertDialog.Title>
      <AlertDialog.Description>
        Screen size: {#if loading}Loading...{:else}
          <code class="bg-muted px-1 rounded text-sm font-mono">{deviceWidth} × {deviceHeight}</code>
        {/if}. Enter crop as <code class="bg-muted px-1 rounded text-sm font-mono">width:height:x:y</code>.
      </AlertDialog.Description>
    </AlertDialog.Header>

    <div class="space-y-4">
      <div class="space-y-2">
        <span class="text-sm font-medium">Quick Presets</span>
        <div class="grid grid-cols-2 gap-2">
          {#each presets as preset}
            <Button
              variant={customValue === preset.value ? "default" : "outline"}
              size="sm"
              onclick={() => applyPreset(preset.value)}>
              {preset.label}
            </Button>
          {/each}
        </div>
      </div>

      <div class="space-y-2">
        <label class="text-sm font-medium" for="crop-input">Custom</label>
        <Input
          id="crop-input"
          placeholder={`e.g. ${hw}:${hh}:0:0`}
          bind:value={customValue}
          onkeydown={(e) => {
            if (e.key === "Enter" && isValidCrop(customValue)) {
              apply();
            }
          }} />
        {#if customValue && !isValidCrop(customValue)}
          <p class="text-xs text-red-500">Invalid format. Expected width:height:x:y within screen bounds.</p>
        {/if}
      </div>
    </div>

    <AlertDialog.Footer>
      {#if initialCrop}
        <AlertDialog.Cancel onclick={clear}>Clear Crop</AlertDialog.Cancel>
      {/if}
      <AlertDialog.Cancel>Cancel</AlertDialog.Cancel>
      <AlertDialog.Action onclick={apply} disabled={!isValidCrop(customValue)}>Apply</AlertDialog.Action>
    </AlertDialog.Footer>
  </AlertDialog.Content>
</AlertDialog.Root>
