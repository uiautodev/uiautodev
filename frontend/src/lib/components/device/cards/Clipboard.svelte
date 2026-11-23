<script lang="ts">
  import { getClipboard, setClipboard } from "$lib/api/api";
  import * as Card from "$lib/components/ui/card/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import Textarea from "$lib/components/ui/textarea/textarea.svelte";
  import { UseClipboard } from "$lib/hooks/use-clipboard.svelte";
  import { t } from "svelte-i18n";
  import type { DeviceClipboard, DeviceClipboardImage } from "$lib/api/schemas";

  const { deviceId, platform = "", ...restProps } = $props();

  const localClipboard = new UseClipboard({ delay: 1500 });
  // Only Android can host clipboard images through MediaStore; HarmonyOS
  // testhelper exposes text only.
  const supportsImages = $derived(platform === "android");

  let text = $state("");
  let image = $state<DeviceClipboardImage | null>(null);
  let loading = $state(false);
  let error = $state("");
  let status = $state("");
  let fileInput = $state<HTMLInputElement>();

  const imagePreview = $derived(image ? `data:${image.mime};base64,${image.data}` : "");

  function errorMessage(err: unknown): string {
    const message = (err as { response?: { data?: { message?: string } } })?.response?.data?.message;
    if (message) return message;
    return err instanceof Error ? err.message : String(err);
  }

  function applyContent(content: DeviceClipboard) {
    if (content.kind === "image" && content.image) {
      image = content.image;
      text = "";
      return;
    }
    image = null;
    text = content.text ?? "";
  }

  function toBase64(bytes: Uint8Array): string {
    let binary = "";
    const chunk = 0x8000;
    for (let i = 0; i < bytes.length; i += chunk) {
      binary += String.fromCharCode(...bytes.subarray(i, i + chunk));
    }
    return btoa(binary);
  }

  async function readFromDevice() {
    if (!deviceId) return;
    loading = true;
    error = "";
    status = "";
    try {
      const content = await getClipboard(deviceId);
      applyContent(content);
      status = content.kind === "image" ? $t("device.clipboard.readImageSuccess") : $t("device.clipboard.readSuccess");
    } catch (err) {
      error = $t("device.clipboard.readFailed", { values: { error: errorMessage(err) } });
    } finally {
      loading = false;
    }
  }

  async function writeToDevice() {
    if (!deviceId) return;
    loading = true;
    error = "";
    status = "";
    try {
      if (image) {
        await setClipboard(deviceId, { kind: "image", image });
        status = $t("device.clipboard.writeImageSuccess");
      } else {
        await setClipboard(deviceId, { kind: "text", text });
        status = $t("device.clipboard.writeSuccess");
      }
    } catch (err) {
      error = $t("device.clipboard.writeFailed", { values: { error: errorMessage(err) } });
    } finally {
      loading = false;
    }
  }

  async function copyImageToHost(content: DeviceClipboardImage) {
    if (typeof ClipboardItem === "undefined" || !navigator.clipboard?.write) {
      throw new Error("clipboard images are not supported by this browser");
    }
    const blob = await (await fetch(`data:${content.mime};base64,${content.data}`)).blob();
    await navigator.clipboard.write([new ClipboardItem({ [content.mime]: blob })]);
  }

  async function copyToHost() {
    error = "";
    status = "";
    try {
      if (image) {
        await copyImageToHost(image);
        status = $t("device.clipboard.copiedImage");
        return;
      }
      const result = await localClipboard.copy(text);
      status = result === "success" ? $t("device.clipboard.copied") : $t("device.clipboard.copyFailed");
    } catch {
      error = image ? $t("device.clipboard.copyImageFailed") : $t("device.clipboard.copyFailed");
    }
  }

  function clearImage() {
    image = null;
    if (fileInput) fileInput.value = "";
  }

  async function onFileChange(event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) return;
    error = "";
    status = "";
    if (!file.type.startsWith("image/")) {
      error = $t("device.clipboard.invalidImage");
      input.value = "";
      return;
    }
    if (file.size > 10 * 1024 * 1024) {
      error = $t("device.clipboard.imageTooLarge");
      input.value = "";
      return;
    }
    const bytes = new Uint8Array(await file.arrayBuffer());
    image = { mime: file.type, data: toBase64(bytes) };
    text = "";
  }
</script>

<Card.Root {...restProps}>
  <Card.Header>
    <Card.Title>{$t("device.clipboard.title")}</Card.Title>
  </Card.Header>
  <Card.Content class="flex flex-col gap-2">
    {#if image}
      <div class="flex flex-col gap-2">
        <img
          src={imagePreview}
          alt={$t("device.clipboard.imagePreview")}
          class="max-h-40 w-auto max-w-full self-start rounded-md border border-border object-contain" />
        <div class="flex items-center gap-2">
          <span class="text-xs text-muted-foreground">{image.mime}</span>
          <Button size="sm" variant="ghost" onclick={clearImage}>{$t("device.clipboard.removeImage")}</Button>
        </div>
      </div>
    {:else}
      <Textarea
        bind:value={text}
        class="min-h-20"
        placeholder={$t("device.clipboard.placeholder")}
        aria-label={$t("device.clipboard.title")} />
    {/if}
    <div class="flex flex-wrap gap-2">
      <Button onclick={readFromDevice} disabled={loading || !deviceId}>
        {loading ? $t("device.clipboard.working") : $t("device.clipboard.read")}
      </Button>
      <Button onclick={writeToDevice} disabled={loading || !deviceId} variant="secondary">
        {$t("device.clipboard.write")}
      </Button>
      <Button onclick={copyToHost} disabled={!text && !image} variant="outline">
        {$t("device.clipboard.copy")}
      </Button>
      {#if supportsImages}
        <Button onclick={() => fileInput?.click()} disabled={loading || !deviceId} variant="outline">
          {$t("device.clipboard.chooseImage")}
        </Button>
      {/if}
    </div>
    <input
      bind:this={fileInput}
      type="file"
      accept="image/*"
      class="hidden"
      aria-label={$t("device.clipboard.chooseImage")}
      onchange={onFileChange} />
    {#if error}
      <p class="text-sm text-destructive">{error}</p>
    {:else if status}
      <p class="text-sm text-muted-foreground">{status}</p>
    {/if}
  </Card.Content>
</Card.Root>
