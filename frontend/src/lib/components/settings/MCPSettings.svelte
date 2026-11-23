<script lang="ts">
  import { onMount } from "svelte";
  import { AsyncButton } from "$lib/components/ui/button/index.js";
  import { buttonVariants } from "$lib/components/ui/button/button.svelte";
  import * as Card from "$lib/components/ui/card/index.js";
  import Input from "$lib/components/ui/input/input.svelte";
  import { Plug, RefreshCw, Save } from "lucide-svelte";
  import { getMCPToken, setMCPToken } from "$lib/api/api.js";
  import { t } from "svelte-i18n";

  let { token, onAuthExpired } = $props<{
    token: string;
    onAuthExpired: () => void;
  }>();

  let mcpToken = $state("");
  let loading = $state(true);
  let saving = $state(false);
  let saved = $state(false);
  let error = $state("");

  function isUnauthorized(e: unknown): boolean {
    return (e as { response?: { status?: number } })?.response?.status === 401;
  }

  async function load() {
    loading = true;
    error = "";
    try {
      mcpToken = await getMCPToken(token);
    } catch (e) {
      console.error("failed to load mcp token", e);
      error = e instanceof Error ? e.message : $t("common.loadFailed");
      if (isUnauthorized(e)) onAuthExpired();
    } finally {
      loading = false;
    }
  }

  async function save() {
    saving = true;
    error = "";
    try {
      mcpToken = await setMCPToken(token, mcpToken.trim());
      saved = true;
    } catch (e) {
      console.error("failed to save mcp token", e);
      error = e instanceof Error ? e.message : $t("common.saveFailed");
      if (isUnauthorized(e)) onAuthExpired();
    } finally {
      saving = false;
    }
  }

  function generate() {
    const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789";
    const bytes = new Uint8Array(16);
    crypto.getRandomValues(bytes);
    const rand = Array.from(bytes, (b) => alphabet.charAt(b % alphabet.length)).join("");
    mcpToken = "ua-" + rand;
    saved = false;
  }

  onMount(load);
</script>

<Card.Root>
  <Card.Header>
    <Card.Title class="flex items-center gap-2">
      <Plug />
      {$t("mcp.title")}
    </Card.Title>
    <Card.Description>{$t("mcp.desc")}</Card.Description>
  </Card.Header>
  <Card.Content class="space-y-4">
    <div>
      <label for="mcp-token" class="text-sm font-medium text-foreground block mb-1">{$t("mcp.tokenLabel")}</label>
      <div class="flex items-center gap-2">
        <Input
          id="mcp-token"
          type="text"
          placeholder={$t("mcp.tokenPlaceholder")}
          bind:value={mcpToken}
          disabled={loading}
          spellcheck="false"
          autocomplete="off"
          class="flex-1 font-mono" />
        <button
          type="button"
          class={buttonVariants({ variant: "outline", size: "icon" })}
          title={$t("mcp.generate")}
          aria-label={$t("mcp.generate")}
          disabled={loading}
          onclick={generate}>
          <RefreshCw />
        </button>
      </div>
      <p class="text-xs text-muted-foreground mt-1.5">{$t("mcp.emptyHint")}</p>
    </div>
    {#if error}
      <p class="text-sm text-destructive">{error}</p>
    {/if}
    <AsyncButton
      onclick={save}
      disabled={saving || loading}
      loading={saving}
      bind:success={saved}
      loadingLabel={$t("common.saving")}
      successLabel={$t("common.saved")}>
      <Save />
      {$t("common.save")}
    </AsyncButton>
  </Card.Content>
</Card.Root>
