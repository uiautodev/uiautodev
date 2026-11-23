<script lang="ts">
  import * as AlertDialog from "$lib/components/ui/alert-dialog/index.js";
  import { Spinner } from "$lib/components/ui/spinner";
  import { Switch } from "$lib/components/ui/switch/index.js";
  import { getDeviceEncoders } from "$lib/api/api.js";
  import type { DeviceEncoder } from "$lib/api/schemas.js";
  import type { VideoEncoderSelection } from "./types.js";
  import { WebCodecsVideoDecoder } from "@yume-chan/scrcpy-decoder-webcodecs";
  import { t } from "svelte-i18n";
  import { Check } from "lucide-svelte";

  interface Props {
    active: boolean;
    deviceId: string;
    selected?: VideoEncoderSelection | null;
    onapply?: (value: VideoEncoderSelection | null) => void;
  }

  let { active = $bindable(false), deviceId, selected = null, onapply }: Props = $props();

  // Without WebCodecs the stream is decoded by TinyH264, which only supports
  // Baseline, so request it by default. With WebCodecs it is unnecessary and
  // may break hardware encoders that do not support it.
  const baselineDefault = !WebCodecsVideoDecoder.isSupported;

  let encoders = $state<DeviceEncoder[]>([]);
  let loading = $state(false);
  let error = $state("");
  let pending = $state<VideoEncoderSelection | null>(selected);
  const baselineEnabled = $derived(pending?.baseline ?? baselineDefault);

  function typeLabel(type: DeviceEncoder["type"]): string {
    switch (type) {
      case "hw":
        return $t("screen.encoderDialog.typeHw");
      case "sw":
        return $t("screen.encoderDialog.typeSw");
      default:
        return $t("screen.encoderDialog.typeHybrid");
    }
  }

  function isSelected(name: string): boolean {
    return pending?.name === name;
  }

  function select(encoder: DeviceEncoder) {
    pending = { name: encoder.name, codec: encoder.codec, baseline: pending?.baseline ?? baselineDefault };
  }

  function setBaseline(value: boolean) {
    if (!pending) return;
    pending = { ...pending, baseline: value };
  }

  function apply() {
    onapply?.(pending);
    active = false;
  }

  $effect(() => {
    if (!active) return;
    pending = selected;
    encoders = [];
    error = "";
    loading = true;
    let cancelled = false;
    getDeviceEncoders(deviceId)
      .then((list) => {
        if (!cancelled) encoders = list.video;
      })
      .catch((e) => {
        if (!cancelled) error = e instanceof Error ? e.message : String(e);
      })
      .finally(() => {
        if (!cancelled) loading = false;
      });
    return () => {
      cancelled = true;
    };
  });
</script>

<AlertDialog.Root bind:open={active}>
  <AlertDialog.Content>
    <AlertDialog.Header>
      <AlertDialog.Title>{$t("screen.encoderDialog.title")}</AlertDialog.Title>
      <AlertDialog.Description>{$t("screen.encoderDialog.description")}</AlertDialog.Description>
    </AlertDialog.Header>

    <div class="max-h-80 space-y-2 overflow-y-auto pr-1">
      <button
        type="button"
        class="flex w-full items-start gap-2 rounded-md border p-2 text-left transition-colors hover:bg-muted {pending ===
        null
          ? 'border-primary bg-primary/5'
          : 'border-border'}"
        onclick={() => (pending = null)}>
        <Check class="mt-0.5 size-4 shrink-0 {pending === null ? 'text-primary' : 'invisible'}" />
        <span class="min-w-0">
          <span class="block text-sm font-medium">{$t("screen.encoderDialog.auto")}</span>
          <span class="block text-xs text-muted-foreground">{$t("screen.encoderDialog.autoDescription")}</span>
        </span>
      </button>

      {#if loading}
        <div class="flex items-center justify-center py-6">
          <Spinner class="size-6" />
        </div>
      {:else if error}
        <p class="py-3 text-sm text-red-500">{error}</p>
      {:else if encoders.length === 0}
        <p class="py-3 text-sm text-muted-foreground">{$t("screen.encoderDialog.empty")}</p>
      {:else}
        {#each encoders as encoder (encoder.name)}
          <button
            type="button"
            class="flex w-full items-start gap-2 rounded-md border p-2 text-left transition-colors hover:bg-muted {isSelected(
              encoder.name,
            )
              ? 'border-primary bg-primary/5'
              : 'border-border'}"
            onclick={() => select(encoder)}>
            <Check class="mt-0.5 size-4 shrink-0 {isSelected(encoder.name) ? 'text-primary' : 'invisible'}" />
            <span class="min-w-0 flex-1">
              <span class="block break-all font-mono text-xs">{encoder.name}</span>
              <span class="mt-1 flex flex-wrap gap-1 text-[10px] text-muted-foreground">
                <span class="rounded bg-muted px-1.5 py-0.5 uppercase">{encoder.codec}</span>
                <span class="rounded bg-muted px-1.5 py-0.5">{typeLabel(encoder.type)}</span>
                {#if encoder.vendor}
                  <span class="rounded bg-muted px-1.5 py-0.5">{$t("screen.encoderDialog.vendor")}</span>
                {/if}
              </span>
            </span>
          </button>
        {/each}
      {/if}
    </div>

    {#if pending?.codec === "h264"}
      <div class="mt-3 flex items-start justify-between gap-3 rounded-md border border-border p-2">
        <div class="min-w-0">
          <span class="block text-sm font-medium">{$t("screen.encoderDialog.baseline")}</span>
          <span class="block text-xs text-muted-foreground">{$t("screen.encoderDialog.baselineDescription")}</span>
        </div>
        <Switch
          checked={baselineEnabled}
          onCheckedChange={setBaseline}
          aria-label={$t("screen.encoderDialog.baseline")} />
      </div>
    {/if}

    <AlertDialog.Footer>
      <AlertDialog.Cancel>{$t("screen.encoderDialog.cancel")}</AlertDialog.Cancel>
      <AlertDialog.Action onclick={apply} disabled={loading}>{$t("screen.encoderDialog.apply")}</AlertDialog.Action>
    </AlertDialog.Footer>
  </AlertDialog.Content>
</AlertDialog.Root>
