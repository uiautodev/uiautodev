<script lang="ts">
  import DumpOverlay from "../dump-overlay.svelte";
  import { deviceStore } from "$lib/stores/device.svelte";

  interface Props {
    screenWidth: number;
    screenHeight: number;
    canvasWidth: number;
    canvasHeight: number;
    isTiny: boolean;
    showRightBar: boolean;
  }

  let { screenWidth, screenHeight, canvasWidth, canvasHeight, isTiny, showRightBar }: Props = $props();

  // Dump mode is active when there's a frozen screen image or a UI hierarchy dump
  let active = $derived(!!deviceStore.dumpImage || !!deviceStore.node);
</script>

{#if active}
  <DumpOverlay
    image={deviceStore.dumpImage}
    style={`width: ${canvasWidth}px; height: ${canvasHeight}px`}
    class={[
      "absolute",
      "rounded-md",
      "max-h-full",
      "border",
      !isTiny && showRightBar && "mr-[40px]",
      !deviceStore.node && "pointer-events-none",
      !!deviceStore.dumpImage && "border-2 border-blue-500",
    ]}
    node={deviceStore.node}
    width={screenWidth}
    height={screenHeight} />
{/if}
