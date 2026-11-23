<script lang="ts">
  import { onMount } from "svelte";
  import { AsyncButton } from "$lib/components/ui/button/index.js";
  import * as Card from "$lib/components/ui/card/index.js";
  import Input from "$lib/components/ui/input/input.svelte";
  import { Textarea } from "$lib/components/ui/textarea/index.js";
  import { Switch } from "$lib/components/ui/switch/index.js";
  import * as Select from "$lib/components/ui/select/index.js";
  import { Network, Save, ShieldCheck } from "lucide-svelte";
  import { t } from "svelte-i18n";
  import { getAccessConfig, setAccessConfig } from "$lib/api/api.js";
  import {
    CONFLICT_POLICY_LIST,
    IDLE_TIMEOUT_LIST,
    type AccessConfig,
    type ConflictPolicy,
    type IdleTimeout,
  } from "$lib/api/schemas.js";

  let { token, onAuthExpired } = $props<{
    token: string;
    onAuthExpired: () => void;
  }>();

  let config = $state<AccessConfig | null>(null);
  let configFile = $state("");
  let proxiesText = $state("");
  let loading = $state(true);
  let saving = $state(false);
  let saved = $state(false);
  let error = $state("");

  function isUnauthorized(e: unknown): boolean {
    return (e as { response?: { status?: number } })?.response?.status === 401;
  }

  const identityWarning = $derived(
    !!config && config.auth.require_identity && config.auth.trusted_proxies.length === 0,
  );

  async function load() {
    if (!token) return;
    loading = true;
    error = "";
    try {
      const res = await getAccessConfig(token);
      config = res.config;
      configFile = res.config_file;
      proxiesText = res.config.auth.trusted_proxies.join("\n");
    } catch (e) {
      console.error("failed to load access config", e);
      error = e instanceof Error ? e.message : $t("common.loadFailed");
      if (isUnauthorized(e)) onAuthExpired();
    } finally {
      loading = false;
    }
  }

  async function save() {
    if (!config) return;
    saving = true;
    error = "";
    try {
      const payload: AccessConfig = {
        auth: {
          ...config.auth,
          trusted_proxies: proxiesText
            .split("\n")
            .map((line) => line.trim())
            .filter((line) => line.length > 0),
        },
        presence: config.presence,
      };
      const res = await setAccessConfig(token, payload);
      config = res.config;
      configFile = res.config_file;
      proxiesText = res.config.auth.trusted_proxies.join("\n");
      saved = true;
    } catch (e) {
      console.error("failed to save access config", e);
      const message = (e as { response?: { data?: { message?: string } } })?.response?.data?.message;
      error = message || (e instanceof Error ? e.message : $t("common.saveFailed"));
      if (isUnauthorized(e)) onAuthExpired();
    } finally {
      saving = false;
    }
  }

  onMount(load);
</script>

<Card.Root>
  <Card.Header>
    <Card.Title class="flex items-center gap-2">
      <ShieldCheck />
      {$t("access.title")}
    </Card.Title>
    <Card.Description>{$t("access.desc")}</Card.Description>
  </Card.Header>
  <Card.Content class="space-y-6">
    {#if loading}
      <p class="text-sm text-muted-foreground">{$t("common.loading")}</p>
    {:else if config}
      <section class="space-y-3">
        <h3 class="flex items-center gap-2 text-sm font-semibold text-foreground">
          <Network class="size-4" />
          {$t("access.identitySection")}
        </h3>
        <div>
          <label for="access-proxies" class="mb-1 block text-sm font-medium text-foreground">
            {$t("access.trustedProxies")}
          </label>
          <Textarea
            id="access-proxies"
            bind:value={proxiesText}
            class="min-h-24 font-mono text-sm"
            spellcheck="false"
            placeholder={$t("access.trustedProxiesPlaceholder")} />
          <p class="mt-1.5 text-xs text-muted-foreground">{$t("access.trustedProxiesHint")}</p>
        </div>
        <div class="flex items-center justify-between gap-4">
          <div>
            <p class="text-sm font-medium text-foreground">{$t("access.requireIdentity")}</p>
            <p class="text-xs text-muted-foreground">{$t("access.requireIdentityHint")}</p>
          </div>
          <Switch bind:checked={config.auth.require_identity} />
        </div>
        <div>
          <label for="access-signout" class="mb-1 block text-sm font-medium text-foreground">
            {$t("access.signoutUrl")}
          </label>
          <Input
            id="access-signout"
            bind:value={config.auth.signout_url}
            placeholder="/oauth2/sign_out"
            spellcheck="false" />
          <p class="mt-1.5 text-xs text-muted-foreground">{$t("access.signoutUrlHint")}</p>
        </div>
        {#if identityWarning}
          <p class="rounded-md border border-amber-200 bg-amber-50 p-2 text-xs text-amber-900 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-200">
            {$t("access.requireIdentityWarning")}
          </p>
        {/if}
      </section>

      <section class="space-y-3 border-t border-border pt-4">
        <h3 class="text-sm font-semibold text-foreground">{$t("access.presenceSection")}</h3>
        <div class="flex items-center justify-between gap-4">
          <div>
            <p class="text-sm font-medium text-foreground">{$t("access.presenceEnabled")}</p>
            <p class="text-xs text-muted-foreground">{$t("access.presenceEnabledHint")}</p>
          </div>
          <Switch bind:checked={config.presence.enabled} />
        </div>
        <div class="grid gap-3 sm:grid-cols-2">
          <div>
            <p class="mb-1 block text-sm font-medium text-foreground">{$t("access.idleTimeout")}</p>
            <Select.Root
              type="single"
              value={config.presence.idle_timeout}
              onValueChange={(value) => (config!.presence.idle_timeout = value as IdleTimeout)}>
              <Select.Trigger class="w-full">
                {$t(`access.idle.${config.presence.idle_timeout}`)}
              </Select.Trigger>
              <Select.Content>
                {#each IDLE_TIMEOUT_LIST as value (value)}
                  <Select.Item value={value}>{$t(`access.idle.${value}`)}</Select.Item>
                {/each}
              </Select.Content>
            </Select.Root>
          </div>
          <div>
            <p class="mb-1 block text-sm font-medium text-foreground">{$t("access.conflictPolicy")}</p>
            <Select.Root
              type="single"
              value={config.presence.conflict_policy}
              onValueChange={(value) => (config!.presence.conflict_policy = value as ConflictPolicy)}>
              <Select.Trigger class="w-full">
                {$t(`access.policy.${config.presence.conflict_policy}`)}
              </Select.Trigger>
              <Select.Content>
                {#each CONFLICT_POLICY_LIST as value (value)}
                  <Select.Item value={value}>{$t(`access.policy.${value}`)}</Select.Item>
                {/each}
              </Select.Content>
            </Select.Root>
          </div>
        </div>
        <div class="flex items-center justify-between gap-4">
          <div>
            <p class="text-sm font-medium text-foreground">{$t("access.allowObservers")}</p>
            <p class="text-xs text-muted-foreground">{$t("access.allowObserversHint")}</p>
          </div>
          <Switch bind:checked={config.presence.allow_observers} />
        </div>
        {#if configFile}
          <div class="space-y-1 text-xs text-muted-foreground">
            <p>{$t("access.configFile")}</p>
            <code class="block rounded bg-muted px-2 py-1 font-mono text-[11px] break-all">{configFile}</code>
          </div>
        {/if}
      </section>

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
    {:else if error}
      <p class="text-sm text-destructive">{error}</p>
    {/if}
  </Card.Content>
</Card.Root>
