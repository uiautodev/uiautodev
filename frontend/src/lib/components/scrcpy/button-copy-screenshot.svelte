<script lang="ts">
  import ButtonTooltip from "./button-tooltip.svelte";
  import { Check, Images, X } from "lucide-svelte";
  import { Spinner } from "$lib/components/ui/spinner";
  import { t } from "svelte-i18n";

  type Props = {
    snapshot: () => Promise<Blob>;
    inline?: boolean;
  };

  let { snapshot, inline = false, ...restProps }: Props = $props();

  let copying = $state(false);
  let copied = $state(false);
  let error = $state(false);

  async function copyScreenshot() {
    copying = true;
    try {
      const blob = await snapshot();
      await navigator.clipboard.write([new ClipboardItem({ "image/png": blob })]);
      copied = true;
    } catch (e) {
      error = true;
      console.error("Failed to copy screenshot:", e);
    } finally {
      copying = false;
    }
    setTimeout(() => {
      copied = false;
      error = false;
    }, 2000);
  }
</script>

<ButtonTooltip
  {...restProps}
  size={inline ? undefined : "sm"}
  variant={inline ? "ghost" : "default"}
  side={inline ? "bottom" : "right"}
  tooltip={$t("screen.toolbar.copyScreenshot")}
  onclick={copyScreenshot}
  class={inline ? "h-7 px-2 rounded cursor-pointer" : "cursor-pointer"}>
  {#if copying}
    <Spinner />
  {:else if error}
    <X class="text-red-500" />
  {:else if copied}
    <Check />
  {:else}
    <Images />
  {/if}
</ButtonTooltip>
