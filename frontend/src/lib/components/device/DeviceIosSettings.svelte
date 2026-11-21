<script lang="ts">
  import { Settings2 } from "lucide-svelte";
  import { Button, buttonVariants } from "$lib/components/ui/button/index.js";
  import Input from "$lib/components/ui/input/input.svelte";
  import * as Popover from "$lib/components/ui/popover/index.js";
  import { setDeviceMeta } from "$lib/api/api.js";
  import type { DeviceMeta } from "$lib/api/schemas.js";
  import { alertStore } from "$lib/stores/alert.svelte";
  import { cn } from "$lib/utils.js";
  import { t } from "svelte-i18n";

  const DEFAULT_WDA_PORT = 8100;
  const DEFAULT_WDA_MJPEG_PORT = 9100;
  const DEFAULT_WDA_BUNDLE_ID = "com.facebook.WebDriverAgentRunner.xctrunner";

  let {
    deviceId,
    meta = {},
    class: className,
    onsaved,
  }: {
    deviceId: string;
    meta?: DeviceMeta;
    class?: string;
    onsaved?: (deviceId: string, meta: DeviceMeta) => void;
  } = $props();

  let open = $state(false);
  let wdaDraft = $state<string | number>("");
  let mjpegDraft = $state<string | number>("");
  let bundleDraft = $state("");
  let saving = $state(false);

  function resetDrafts() {
    wdaDraft = meta.wda_port ? String(meta.wda_port) : "";
    mjpegDraft = meta.wda_mjpeg_port ? String(meta.wda_mjpeg_port) : "";
    bundleDraft = meta.wda_bundle_id ?? "";
  }

  function parsePort(value: string | number): number | undefined | null {
    const trimmed = String(value ?? "").trim();
    if (!trimmed) return undefined;
    const port = Number(trimmed);
    if (!Number.isInteger(port) || port < 1 || port > 65535) return null;
    return port;
  }

  async function save() {
    if (saving) return;
    const wda = parsePort(wdaDraft);
    const mjpeg = parsePort(mjpegDraft);
    if (wda === null || mjpeg === null) {
      alertStore.error($t("device.iosSettings.invalidPort"), {
        title: $t("device.iosSettings.title"),
      });
      return;
    }
    saving = true;
    try {
      const updated: DeviceMeta = { ...meta };
      if (wda === undefined) {
        delete updated.wda_port;
      } else {
        updated.wda_port = wda;
      }
      if (mjpeg === undefined) {
        delete updated.wda_mjpeg_port;
      } else {
        updated.wda_mjpeg_port = mjpeg;
      }
      const bundle = bundleDraft.trim();
      if (bundle) {
        updated.wda_bundle_id = bundle;
      } else {
        delete updated.wda_bundle_id;
      }
      const saved = await setDeviceMeta(deviceId, updated);
      onsaved?.(deviceId, saved ?? updated);
      open = false;
    } catch (e) {
      console.error("failed to save iOS ports", e);
      alertStore.error(e instanceof Error ? e.message : $t("common.saveFailed"), {
        title: $t("device.iosSettings.title"),
      });
    } finally {
      saving = false;
    }
  }
</script>

<Popover.Root bind:open>
  <Popover.Trigger
    class={cn(buttonVariants({ variant: "ghost", size: "icon" }), "size-7", className)}
    title={$t("device.iosSettings.title")}
    aria-label={$t("device.iosSettings.title")}
    onclick={resetDrafts}>
    <Settings2 class="size-4" />
  </Popover.Trigger>
  <Popover.Content side="bottom" align="end" class="w-96 space-y-3">
    <p class="text-sm font-medium text-foreground">{$t("device.iosSettings.title")}</p>
    <div class="space-y-1">
      <label class="text-xs text-muted-foreground" for="ios-wda-port">
        {$t("device.iosSettings.wdaPort")}
      </label>
      <Input
        id="ios-wda-port"
        type="number"
        min="1"
        max="65535"
        placeholder={String(DEFAULT_WDA_PORT)}
        bind:value={wdaDraft}
        disabled={saving}
        class="h-8" />
    </div>
    <div class="space-y-1">
      <label class="text-xs text-muted-foreground" for="ios-wda-mjpeg-port">
        {$t("device.iosSettings.wdaMjpegPort")}
      </label>
      <Input
        id="ios-wda-mjpeg-port"
        type="number"
        min="1"
        max="65535"
        placeholder={String(DEFAULT_WDA_MJPEG_PORT)}
        bind:value={mjpegDraft}
        disabled={saving}
        class="h-8" />
    </div>
    <p class="text-xs text-muted-foreground">
      {$t("device.iosSettings.defaultHint", {
        values: { wda: DEFAULT_WDA_PORT, mjpeg: DEFAULT_WDA_MJPEG_PORT },
      })}
    </p>
    <div class="space-y-1">
      <label class="text-xs text-muted-foreground" for="ios-wda-bundle-id">
        {$t("device.iosSettings.wdaBundleId")}
      </label>
      <Input
        id="ios-wda-bundle-id"
        type="text"
        placeholder={DEFAULT_WDA_BUNDLE_ID}
        bind:value={bundleDraft}
        disabled={saving}
        spellcheck="false"
        autocomplete="off"
        class="h-8" />
    </div>
    <div class="flex justify-end gap-2">
      <Button variant="ghost" size="sm" onclick={() => (open = false)} disabled={saving}>
        {$t("common.cancel")}
      </Button>
      <Button size="sm" onclick={save} disabled={saving}>{$t("common.save")}</Button>
    </div>
  </Popover.Content>
</Popover.Root>
