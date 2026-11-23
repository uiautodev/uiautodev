<script lang="ts">
  import { locale, t } from "svelte-i18n";
  import { LOCALES, setLocale, type LocaleCode } from "$lib/i18n/index.svelte";
  import { Button } from "$lib/components/ui/button/index.js";
  import * as Tooltip from "$lib/components/ui/tooltip/index.js";
  import { Languages } from "lucide-svelte";

  let current = $derived(($locale ?? "zh-CN") as LocaleCode);
  let next = $derived<LocaleCode>(current === "zh-CN" ? "en" : "zh-CN");
  let nextLabel = $derived(LOCALES.find((l) => l.code === next)?.label ?? "");

  function toggle() {
    void setLocale(next);
  }
</script>

<Tooltip.Root>
  <Tooltip.Trigger>
    {#snippet child({ props })}
      <Button {...props} onclick={toggle} variant="ghost" size="icon" aria-label={$t("common.language")}>
        <Languages />
      </Button>
    {/snippet}
  </Tooltip.Trigger>
  <Tooltip.Content side="bottom">
    <p>{nextLabel}</p>
  </Tooltip.Content>
</Tooltip.Root>
