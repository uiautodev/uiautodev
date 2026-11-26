<script lang="ts">
  import { installDeviceApp } from "$lib/api/api";
  import { appPackageExtension } from "$lib/api/schemas";
  import * as Card from "$lib/components/ui/card/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import { t } from "svelte-i18n";

  const { deviceId, platform = "", ...restProps } = $props();

  const extension = $derived(appPackageExtension(platform));
  const accept = $derived(extension || undefined);

  let fileInput = $state<HTMLInputElement>();
  let selected = $state<File | null>(null);
  let installing = $state(false);
  let error = $state("");
  let status = $state("");

  function errorMessage(err: unknown): string {
    const message = (err as { response?: { data?: { message?: string } } })?.response?.data?.message;
    if (message) return message;
    return err instanceof Error ? err.message : String(err);
  }

  function resetSelection() {
    selected = null;
    if (fileInput) fileInput.value = "";
  }

  function chooseFile() {
    error = "";
    status = "";
    fileInput?.click();
  }

  function onFileChange(event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    error = "";
    status = "";
    if (!file) {
      selected = null;
      return;
    }
    if (!file.name.toLowerCase().endsWith(extension)) {
      resetSelection();
      error = $t("device.appInstall.invalidFile", { values: { ext: extension } });
      return;
    }
    selected = file;
  }

  async function install() {
    if (!deviceId || !selected || installing) return;
    const name = selected.name;
    installing = true;
    error = "";
    status = "";
    try {
      await installDeviceApp(deviceId, selected);
      status = $t("device.appInstall.success", { values: { name } });
      resetSelection();
    } catch (err) {
      error = $t("device.appInstall.failed", { values: { error: errorMessage(err) } });
    } finally {
      installing = false;
    }
  }
</script>

<Card.Root {...restProps}>
  <Card.Header>
    <Card.Title>{$t("device.appInstall.title")}</Card.Title>
    <Card.Description>
      {$t("device.appInstall.description", { values: { ext: extension } })}
    </Card.Description>
  </Card.Header>
  <Card.Content class="flex flex-col gap-2">
    <input
      bind:this={fileInput}
      type="file"
      {accept}
      class="hidden"
      aria-label={$t("device.appInstall.chooseFile")}
      onchange={onFileChange} />
    <div class="flex flex-wrap items-center gap-2">
      <Button onclick={chooseFile} disabled={installing} variant="outline">
        {$t("device.appInstall.chooseFile")}
      </Button>
      <Button onclick={install} disabled={!selected || installing}>
        {installing ? $t("device.appInstall.installing") : $t("device.appInstall.install")}
      </Button>
    </div>
    {#if selected}
      <p class="truncate text-sm text-muted-foreground">{selected.name}</p>
    {/if}
    {#if error}
      <p class="text-sm text-destructive">{error}</p>
    {:else if status}
      <p class="text-sm text-muted-foreground">{status}</p>
    {/if}
  </Card.Content>
</Card.Root>
