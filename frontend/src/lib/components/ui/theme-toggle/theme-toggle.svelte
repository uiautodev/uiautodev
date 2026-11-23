<script lang="ts">
  import { Button } from "$lib/components/ui/button/index.js";
  import * as Tooltip from "$lib/components/ui/tooltip/index.js";
  import { Monitor, Moon, Sun } from "lucide-svelte";
  import { cycleTheme, themeStore } from "$lib/stores/theme.svelte";
  import { t } from "svelte-i18n";

  const label = $derived($t(`common.theme.${themeStore.mode}`));
</script>

<Tooltip.Root>
  <Tooltip.Trigger>
    {#snippet child({ props })}
      <Button {...props} onclick={cycleTheme} variant="ghost" size="icon" aria-label={label}>
        {#if themeStore.mode === "light"}
          <Sun />
        {:else if themeStore.mode === "dark"}
          <Moon />
        {:else}
          <Monitor />
        {/if}
      </Button>
    {/snippet}
  </Tooltip.Trigger>
  <Tooltip.Content side="bottom">
    <p>{label}</p>
  </Tooltip.Content>
</Tooltip.Root>
