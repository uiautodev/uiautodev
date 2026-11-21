<script lang="ts">
  import { goto } from "$app/navigation";
  import { Button } from "$lib/components/ui/button/index.js";
  import * as Tooltip from "$lib/components/ui/tooltip/index.js";
  import { ArrowUpCircle } from "lucide-svelte";
  import { t } from "svelte-i18n";
  import { updateStore } from "$lib/stores/update.svelte";

  let visible = $derived(updateStore.available);
  let label = $derived(
    $t("common.newVersionAvailableHint", { values: { version: updateStore.latestVersion } }),
  );

  function openUpdatePage() {
    goto("/config#update");
  }
</script>

{#if visible}
  <Tooltip.Root>
    <Tooltip.Trigger>
      {#snippet child({ props })}
        <Button
          {...props}
          onclick={openUpdatePage}
          variant="ghost"
          size="icon"
          class="relative text-primary"
          aria-label={label}>
          <ArrowUpCircle />
          <span class="absolute right-1 top-1 size-1.5 rounded-full bg-red-500"></span>
        </Button>
      {/snippet}
    </Tooltip.Trigger>
    <Tooltip.Content side="bottom">
      <p>{label}</p>
    </Tooltip.Content>
  </Tooltip.Root>
{/if}
