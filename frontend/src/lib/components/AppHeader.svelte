<script lang="ts">
  import { goto } from "$app/navigation";
  import { Button } from "$lib/components/ui/button/index.js";
  import { Layers, Settings, Smartphone } from "lucide-svelte";
  import { t } from "svelte-i18n";
  import McpIcon from "$lib/components/icons/mcp.svelte";
  import LanguageSwitcher from "$lib/components/ui/language-switcher/language-switcher.svelte";
  import ThemeToggle from "$lib/components/ui/theme-toggle/theme-toggle.svelte";
  import UpdateButton from "$lib/components/ui/update-button/update-button.svelte";
  import UserMenu from "$lib/components/UserMenu.svelte";

  let { active = "" }: { active?: string } = $props();
</script>

<header class="shrink-0 border-b border-border bg-card py-2">
  <div class="max-w-4xl mx-auto px-4 flex items-center">
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
      <LanguageSwitcher />
      <ThemeToggle />
    </div>
  </div>
</header>
