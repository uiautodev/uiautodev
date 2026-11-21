<script lang="ts">
  import { onMount } from "svelte";
  import { Button } from "$lib/components/ui/button/index.js";
  import * as Card from "$lib/components/ui/card/index.js";
  import Input from "$lib/components/ui/input/input.svelte";
  import * as InputGroup from "$lib/components/ui/input-group/index.js";
  import { Search, Store } from "lucide-svelte";
  import { listMarketPlugins, installMarketPlugin, getMarketSource, setMarketSource } from "$lib/api/api.js";
  import type { MarketItem, MarketSource } from "$lib/api/schemas.js";
  import { isOfficialPlugin } from "$lib/plugin.js";
  import { t } from "svelte-i18n";

  let { token, onAuthExpired, onInstalled } = $props<{
    token: string;
    onAuthExpired: () => void;
    onInstalled?: () => void | Promise<void>;
  }>();

  let marketSource = $state<MarketSource | null>(null);
  let sourceInput = $state("");
  let savingSource = $state(false);
  let sourceSaved = $state(false);
  let sourceError = $state("");

  let marketItems = $state<MarketItem[]>([]);
  let marketLoading = $state(false);
  let marketError = $state("");
  let marketQuery = $state("");
  let marketPage = $state(1);
  let marketTotal = $state(0);
  let installingId = $state("");
  const marketPageSize = 20;
  const marketTotalPages = $derived(Math.max(1, Math.ceil(marketTotal / marketPageSize)));

  function isUnauthorized(e: unknown): boolean {
    return (e as { response?: { status?: number } })?.response?.status === 401;
  }

  // The backend or the market service being unreachable (network error or 5xx)
  // is surfaced as a generic maintenance message instead of a raw error.
  function isServerError(e: unknown): boolean {
    const status = (e as { response?: { status?: number } })?.response?.status;
    return status === undefined || status >= 500;
  }

  function marketErrorText(e: unknown, fallbackKey: string): string {
    if (isServerError(e)) return $t("plugin.marketUnavailable");
    const message = (e as { response?: { data?: { message?: string } } })?.response?.data?.message;
    return message || (e instanceof Error ? e.message : $t(fallbackKey));
  }

  async function loadMarketSource() {
    if (!token) return;
    try {
      const source = await getMarketSource(token);
      marketSource = source;
      sourceInput = source.is_custom ? source.url : "";
    } catch (e) {
      console.error("failed to load market source", e);
      if (isUnauthorized(e)) onAuthExpired();
    }
  }

  async function loadMarket() {
    if (!token) return;
    marketLoading = true;
    marketError = "";
    try {
      const res = await listMarketPlugins(token, {
        q: marketQuery || undefined,
        page: marketPage,
        page_size: marketPageSize,
      });
      marketItems = res.items ?? [];
      marketTotal = res.total ?? 0;
    } catch (e) {
      marketError = marketErrorText(e, "plugin.marketLoadFailed");
      if (isUnauthorized(e)) onAuthExpired();
    } finally {
      marketLoading = false;
    }
  }

  async function handleSaveSource() {
    savingSource = true;
    sourceError = "";
    sourceSaved = false;
    try {
      marketSource = await setMarketSource(token, sourceInput.trim());
      sourceInput = marketSource.is_custom ? marketSource.url : "";
      sourceSaved = true;
      marketPage = 1;
      await loadMarket();
    } catch (e) {
      sourceError = e instanceof Error ? e.message : $t("plugin.marketSourceSaveFailed");
      if (isUnauthorized(e)) onAuthExpired();
    } finally {
      savingSource = false;
    }
  }

  async function handleMarketInstall(item: MarketItem) {
    installingId = item.id;
    marketError = "";
    try {
      await installMarketPlugin(token, item.id);
      await onInstalled?.();
      await loadMarket();
    } catch (e) {
      marketError = marketErrorText(e, "plugin.marketInstallFailed");
      if (isUnauthorized(e)) onAuthExpired();
    } finally {
      installingId = "";
    }
  }

  async function handleMarketSearch() {
    marketPage = 1;
    await loadMarket();
  }

  async function changeMarketPage(delta: number) {
    const next = marketPage + delta;
    if (next < 1 || next > marketTotalPages) return;
    marketPage = next;
    await loadMarket();
  }

  onMount(() => {
    loadMarketSource();
    loadMarket();
  });
</script>

<Card.Root>
  <Card.Header>
    <Card.Title class="flex items-center gap-2">
      <Store />
      {$t("plugin.marketTitle")}
    </Card.Title>
    <Card.Description>{$t("plugin.marketDesc")}</Card.Description>
  </Card.Header>
  <Card.Content class="space-y-4">
    <div>
      <label for="market-source" class="text-sm font-medium text-foreground block mb-1">
        {$t("plugin.marketSourceLabel")}
      </label>
      <div class="flex items-center gap-2">
        <Input
          id="market-source"
          type="text"
          bind:value={sourceInput}
          placeholder={$t("plugin.marketSourcePlaceholder")}
          class="flex-1"
          spellcheck="false"
          autocomplete="off"
          onkeydown={(e) => e.key === "Enter" && handleSaveSource()} />
        <Button onclick={handleSaveSource} disabled={savingSource}>
          {$t("plugin.marketSourceSave")}
        </Button>
      </div>
      {#if marketSource && !marketSource.is_custom}
        <p class="text-xs text-muted-foreground mt-1.5">
          {$t("plugin.marketSourceDefault")}: <code class="font-mono">{marketSource.default_url}</code>
        </p>
      {/if}
      {#if sourceSaved}
        <p class="text-xs text-emerald-600 mt-1.5">{$t("plugin.marketSourceSaved")}</p>
      {/if}
      {#if sourceError}
        <p class="text-sm text-destructive mt-1.5">{sourceError}</p>
      {/if}
    </div>

    <InputGroup.Root>
      <InputGroup.Input
        type="search"
        bind:value={marketQuery}
        placeholder={$t("plugin.marketSearchPlaceholder")}
        onkeydown={(e) => e.key === "Enter" && handleMarketSearch()} />
      <InputGroup.Addon align="inline-end">
        <InputGroup.Button
          size="icon-sm"
          onclick={handleMarketSearch}
          aria-label={$t("plugin.marketSearchPlaceholder")}>
          <Search />
        </InputGroup.Button>
      </InputGroup.Addon>
    </InputGroup.Root>

    {#if marketLoading && marketItems.length === 0}
      <p class="text-sm text-muted-foreground py-4 text-center">{$t("plugin.marketLoading")}</p>
    {:else if marketItems.length === 0}
      <p class="text-sm text-muted-foreground py-4 text-center">{$t("plugin.marketEmpty")}</p>
    {:else}
      <ul class="divide-y divide-border">
        {#each marketItems as item (item.id)}
          <li class="py-3 flex items-start gap-3">
            <div class="flex-1 min-w-0">
              <div class="flex items-center gap-2 flex-wrap">
                <span class="font-medium text-foreground">{item.name}</span>
                <span class="rounded bg-muted px-1.5 py-px text-xs font-medium">v{item.latest_version}</span>
                {#if isOfficialPlugin(item.id)}
                  <span class="rounded bg-primary/10 text-primary px-1.5 py-px text-xs font-medium"
                    >{$t("plugin.official")}</span>
                {/if}
                {#if item.author}
                  <span class="text-xs text-muted-foreground">{item.author}</span>
                {/if}
                {#if item.update_available}
                  <span class="rounded bg-amber-500/15 text-amber-600 px-1.5 py-px text-xs font-medium">
                    {$t("plugin.marketUpdate")}
                  </span>
                {/if}
              </div>
              {#if item.description}
                <p class="text-sm text-muted-foreground mt-0.5">{item.description}</p>
              {/if}
              <p class="text-xs text-muted-foreground mt-0.5">
                <code class="font-mono">{item.id}</code>
                {#if item.installed_version}
                  · {$t("plugin.marketInstalled")} v{item.installed_version}
                {/if}
              </p>
            </div>
            <div class="shrink-0">
              <Button
                size="sm"
                variant={item.update_available ? "default" : "outline"}
                disabled={installingId === item.id || (item.installed && !item.update_available)}
                onclick={() => handleMarketInstall(item)}>
                {#if installingId === item.id}
                  {$t("plugin.marketInstalling")}
                {:else if item.installed && !item.update_available}
                  {$t("plugin.marketInstalled")}
                {:else if item.update_available}
                  {$t("plugin.marketUpdate")}
                {:else}
                  {$t("plugin.marketInstall")}
                {/if}
              </Button>
            </div>
          </li>
        {/each}
      </ul>
      {#if marketTotalPages > 1}
        <div class="flex items-center justify-center gap-3 pt-2">
          <Button size="sm" variant="outline" disabled={marketPage <= 1} onclick={() => changeMarketPage(-1)}>
            {$t("plugin.marketPrev")}
          </Button>
          <span class="text-xs text-muted-foreground">
            {$t("plugin.marketPage", { values: { page: marketPage, total: marketTotalPages } })}
          </span>
          <Button
            size="sm"
            variant="outline"
            disabled={marketPage >= marketTotalPages}
            onclick={() => changeMarketPage(1)}>
            {$t("plugin.marketNext")}
          </Button>
        </div>
      {/if}
    {/if}
    {#if marketError}
      <p class="text-sm text-destructive">{marketError}</p>
    {/if}
  </Card.Content>
</Card.Root>
