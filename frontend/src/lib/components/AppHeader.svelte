<script lang="ts">
  import { goto } from "$app/navigation";
  import { Button } from "$lib/components/ui/button/index.js";
  import * as Tooltip from "$lib/components/ui/tooltip/index.js";
  import { Layers, Maximize2, Minimize2, Settings, Smartphone } from "lucide-svelte";
  import { t } from "svelte-i18n";
  import McpIcon from "$lib/components/icons/mcp.svelte";
  import LanguageSwitcher from "$lib/components/ui/language-switcher/language-switcher.svelte";
  import ThemeToggle from "$lib/components/ui/theme-toggle/theme-toggle.svelte";
  import UpdateButton from "$lib/components/ui/update-button/update-button.svelte";
  import UserMenu from "$lib/components/UserMenu.svelte";
  import { containerClass, type WidthMode } from "$lib/stores/width";
  import { toggleWidth } from "$lib/stores/width.svelte";

  let { active = "", widthMode }: { active?: string; widthMode?: WidthMode } = $props();

  const container = $derived(containerClass(widthMode));
  const widthLabel = $derived($t(widthMode === "wide" ? "common.width.narrow" : "common.width.wide"));
</script>

<header class="shrink-0 border-b border-border bg-card py-2">
  <div class="{container} flex items-center">
    <div class="flex items-center gap-1">
      <Button variant={active === "device" ? "secondary" : "ghost"} size="sm" onclick={() => goto("/")}>
        <Smartphone class="size-4 mr-1" />
        {$t("home.title")}
      </Button>

      <Button
        variant={active === "groupControl" ? "secondary" : "ghost"}
        size="sm"
        onclick={() => goto("/multi-device")}>
        <Layers class="size-4 mr-1" />
        {$t("home.groupControl")}
      </Button>

      <Button variant={active === "mcp" ? "secondary" : "ghost"} size="sm" onclick={() => goto("/mcp")}>
        <McpIcon class="size-4 mr-1" />
        {$t("home.mcp")}
      </Button>

      <Button variant={active === "config" ? "secondary" : "ghost"} size="sm" onclick={() => goto("/config")}>
        <Settings class="size-4 mr-1" />
        {$t("home.config")}
      </Button>
    </div>
    <div class="ml-auto flex items-center gap-2">
      <UserMenu />
      <UpdateButton />
      {#if widthMode}
        <Tooltip.Root>
          <Tooltip.Trigger>
            {#snippet child({ props })}
              <Button {...props} onclick={toggleWidth} variant="ghost" size="icon" aria-label={widthLabel}>
                {#if widthMode === "wide"}
                  <Minimize2 />
                {:else}
                  <Maximize2 />
                {/if}
              </Button>
            {/snippet}
          </Tooltip.Trigger>
          <Tooltip.Content side="bottom">
            <p>{widthLabel}</p>
          </Tooltip.Content>
        </Tooltip.Root>
      {/if}
      <LanguageSwitcher />
      <ThemeToggle />
    </div>
  </div>
</header>
