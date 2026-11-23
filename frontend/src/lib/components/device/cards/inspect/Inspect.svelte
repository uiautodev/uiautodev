<script lang="ts">
  import { deviceStore, dumpBackendStore } from "$lib/stores/device.svelte";
  import ElementTree from "./ElementTree.svelte";
  import Properties from "./Properties.svelte";
  import XPathLocator from "./XPathLocator.svelte";
  import * as Resizable from "$lib/components/ui/resizable/index.js";
  import * as Select from "$lib/components/ui/select/index.js";
  import * as ButtonGroup from "$lib/components/ui/button-group/index.js";
  import { ScrollArea } from "$lib/components/ui/scroll-area/index.js";
  import { BrushCleaning, Import, RefreshCw, Save, Zap } from "lucide-svelte";
  import { Button } from "$lib/components/ui/button";
  import { exportUItoZip, importUIFromZip } from "./ui-export";
  import { alertStore } from "$lib/stores/alert.svelte";
  import { t } from "svelte-i18n";

  let isDumping = $derived(deviceStore.dumping);
  let isExporting = $state(false);
  let isImporting = $state(false);
  let fileInput: HTMLInputElement | undefined = $state();

  // The dump method only varies on Android (and the demo); other platforms
  // have a single path.
  let showBackendSelect = $derived(
    deviceStore.deviceInfo?.platform === "android" || deviceStore.deviceInfo?.platform === "demo",
  );
  const dumpBackendOptions = [
    { value: "yadb", label: "device.inspect.dumpBackendYadb", short: "yadb" },
    { value: "uiautomator", label: "device.inspect.dumpBackendUiautomator", short: "adb" },
  ] as const;
  let currentBackendOption = $derived(
    dumpBackendOptions.find((o) => o.value === dumpBackendStore.current) ?? dumpBackendOptions[0],
  );
  let dumpBackendLabel = $derived($t(currentBackendOption.label));

  async function dumpHierarchy() {
    await Promise.all([deviceStore.freezeScreen(), deviceStore.dumpHierarchy()]);
  }

  async function handleExport() {
    if (isExporting) return;
    isExporting = true;
    try {
      const saved = await exportUItoZip({
        node: deviceStore.node,
        image: deviceStore.dumpImage,
        device: deviceStore.deviceInfo,
      });
      if (saved) {
        alertStore.success($t("device.inspect.exportSuccess"));
      }
    } catch (e) {
      alertStore.error(e instanceof Error ? e.message : String(e), { title: $t("device.inspect.exportFailed") });
    } finally {
      isExporting = false;
    }
  }

  async function handleImportFile(event: Event) {
    const input = event.target as HTMLInputElement;
    const file = input.files?.[0];
    input.value = "";
    if (!file) return;
    isImporting = true;
    try {
      const { node, image } = await importUIFromZip(file);
      deviceStore.node = node;
      deviceStore.dumpImage = image;
      deviceStore.nodeSelected = null;
      deviceStore.matchedNodes = [];
      alertStore.success($t("device.inspect.importSuccess"), { title: $t("device.inspect.import") });
    } catch (e) {
      alertStore.error(e instanceof Error ? e.message : String(e), { title: $t("device.inspect.importFailed") });
    } finally {
      isImporting = false;
    }
  }
</script>

<div class="h-full flex flex-col p-1 pt-0">
  <Resizable.PaneGroup direction="horizontal" class="flex-1">
    <Resizable.Pane class="pr-2">
      <ScrollArea orientation="vertical" class="h-full">
        <XPathLocator />

        <!-- Action Buttons -->
        <div class="flex items-center gap-2">
          <ButtonGroup.Root>
            <Button onclick={dumpHierarchy} disabled={isDumping} variant="outline" size="sm">
              {#if isDumping}
                <RefreshCw size={16} class="mr-1 animate-spin" />
              {:else}
                <RefreshCw size={16} class="mr-1" />
              {/if}
              {$t("device.inspect.refresh")}
              {#if showBackendSelect}
                {#if currentBackendOption.value === "yadb"}
                  <span class="ml-1 inline-flex" title={$t("device.inspect.dumpBackendYadb")}>
                    <Zap size={14} />
                  </span>
                {:else}
                  <span class="ml-1">({currentBackendOption.short})</span>
                {/if}
              {/if}
            </Button>
            {#if showBackendSelect}
              <Select.Root
                type="single"
                value={dumpBackendStore.current}
                onValueChange={(v) => (dumpBackendStore.current = v as typeof dumpBackendStore.current)}>
                <Select.Trigger
                  size="sm"
                  class="px-2"
                  title={dumpBackendLabel}
                  aria-label={$t("device.inspect.dumpBackend")}>
                  <span class="sr-only">{$t("device.inspect.dumpBackend")}</span>
                </Select.Trigger>
                <Select.Content>
                  {#each dumpBackendOptions as option (option.value)}
                    <Select.Item value={option.value}>{$t(option.label)}</Select.Item>
                  {/each}
                </Select.Content>
              </Select.Root>
            {/if}
          </ButtonGroup.Root>
          <Button onclick={() => deviceStore.clear()} variant="outline" size="sm">
            <BrushCleaning size={16} class="mr-1" />
            {$t("device.inspect.reset")}
          </Button>
          <Button onclick={handleExport} disabled={isExporting || !deviceStore.node} variant="outline" size="sm">
            {#if isExporting}
              <RefreshCw size={16} class="mr-1 animate-spin" />
            {:else}
              <Save size={16} class="mr-1" />
            {/if}
            {isExporting ? $t("device.inspect.exporting") : $t("device.inspect.export")}
          </Button>
          <Button onclick={() => fileInput?.click()} disabled={isImporting} variant="outline" size="sm">
            {#if isImporting}
              <RefreshCw size={16} class="mr-1 animate-spin" />
            {:else}
              <Import size={16} class="mr-1" />
            {/if}
            {isImporting ? $t("device.inspect.importing") : $t("device.inspect.import")}
          </Button>
        </div>
        <input
          bind:this={fileInput}
          type="file"
          accept=".zip,application/zip"
          class="hidden"
          onchange={handleImportFile} />
        <Properties />
      </ScrollArea>
    </Resizable.Pane>
    <Resizable.Handle withHandle />
    <Resizable.Pane defaultSize={50} class="pl-2">
      <ScrollArea orientation="both" class="h-full w-full">
        <ElementTree />
      </ScrollArea>
    </Resizable.Pane>
  </Resizable.PaneGroup>
</div>
