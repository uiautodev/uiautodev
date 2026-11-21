<script lang="ts">
  import ButtonTooltip from "./button-tooltip.svelte";
  import { CircleDot, Video } from "lucide-svelte";
  import { t } from "svelte-i18n";
  import type { ScreenHandler } from "./types";

  interface Props {
    screenHandler: ScreenHandler;
    inline?: boolean;
  }

  let { screenHandler, inline = false }: Props = $props();
  let recording = $state(false);

  let disabled = $derived(!screenHandler.startRecord || !screenHandler.stopRecord);

  async function handleClick() {
    if (disabled || recording) return;

    await screenHandler.startRecord?.();
    recording = true;
  }

  async function handleStop() {
    if (!recording) return;

    await screenHandler.stopRecord?.();
    recording = false;
  }
</script>

<ButtonTooltip
  size={inline ? undefined : "sm"}
  variant={inline ? "ghost" : "default"}
  side={inline ? "bottom" : "right"}
  {disabled}
  class={[inline ? "h-7 px-2 rounded cursor-pointer" : "cursor-pointer", recording && "text-red-500"]}
  tooltip={recording ? $t("screen.toolbar.stopRecording") : $t("screen.toolbar.startRecording")}
  onclick={recording ? handleStop : handleClick}>
  {#if recording}
    <CircleDot class="animate-pulse" />
  {:else}
    <Video />
  {/if}
</ButtonTooltip>
