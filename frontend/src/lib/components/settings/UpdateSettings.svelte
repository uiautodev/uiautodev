<script lang="ts">
  import { onMount } from "svelte";
  import { Button } from "$lib/components/ui/button/index.js";
  import * as Card from "$lib/components/ui/card/index.js";
  import Spinner from "$lib/components/ui/spinner/spinner.svelte";
  import { ArrowUpCircle, Download, ExternalLink, RefreshCw } from "lucide-svelte";
  import { openExternalURL } from "$lib/compat";
  import { t } from "svelte-i18n";
  import { updateStore } from "$lib/stores/update.svelte";

  let info = $derived(updateStore.info);
  let checking = $derived(updateStore.checking);
  let starting = $derived(updateStore.installing);
  let percent = $derived(updateStore.percent);
  let error = $derived(updateStore.error);

  onMount(async () => {
    // An update may already be running when the native menu started it.
    if (await updateStore.resume()) return;
    await updateStore.check();
  });
</script>

<Card.Root>
  <Card.Header>
    <Card.Title class="flex items-center gap-2">
      <ArrowUpCircle class="text-primary" />
      {$t("config.update.title")}
    </Card.Title>
    <Card.Description>{$t("config.update.desc")}</Card.Description>
  </Card.Header>
  <Card.Content class="space-y-4">
    <div class="flex items-center gap-3">
      <span class="text-sm text-muted-foreground">{$t("config.update.currentVersion")}</span>
      <span class="rounded bg-muted px-1.5 py-px font-mono text-xs">v{info?.current ?? "..."}</span>
      {#if info?.available}
        <span class="text-muted-foreground">→</span>
        <span class="rounded bg-primary/10 px-1.5 py-px font-mono text-xs text-primary">v{info.latest}</span>
      {/if}
      <Button
        variant="outline"
        size="sm"
        class="ml-auto"
        onclick={() => updateStore.check()}
        disabled={checking || starting}>
        {#if checking}
          <Spinner />
        {:else}
          <RefreshCw />
        {/if}
        {$t("config.update.check")}
      </Button>
    </div>

    {#if error}
      <p class="text-sm text-destructive">{error}</p>
    {/if}

    {#if starting}
      <div class="space-y-2">
        <div class="h-2 w-full overflow-hidden rounded-full bg-muted">
          <div class="h-full rounded-full bg-primary transition-all" style={`width:${percent}%`}></div>
        </div>
        <p class="text-sm text-muted-foreground">{Math.round(percent)}%</p>
      </div>
    {:else if info}
      {#if info.available}
        <div class="space-y-3">
          {#if info.notes}
            <pre
              class="max-h-48 overflow-auto whitespace-pre-wrap rounded-md border border-border bg-muted p-3 text-xs text-foreground">{info.notes}</pre>
          {/if}
          <div class="flex flex-wrap items-center gap-2">
            {#if info.desktop}
              <Button onclick={() => updateStore.install()} disabled={starting}>
                <Download />
                {$t("config.update.install")}
              </Button>
            {:else}
              <Button onclick={() => openExternalURL(info?.release_url || "https://get.uiauto.dev")}>
                <ExternalLink />
                {$t("config.update.openRelease")}
              </Button>
            {/if}
            {#if info.release_url}
              <Button variant="ghost" size="sm" onclick={() => openExternalURL(info!.release_url!)}>
                <ExternalLink />
                {$t("config.update.releaseNotes")}
              </Button>
            {/if}
          </div>
        </div>
      {:else}
        <p class="text-sm text-muted-foreground">
          {info.dev ? $t("config.update.devBuild") : $t("config.update.upToDate")}
        </p>
      {/if}
    {/if}
  </Card.Content>
</Card.Root>
