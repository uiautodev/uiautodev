<script lang="ts">
  import { onMount } from "svelte";
  import { AsyncButton } from "$lib/components/ui/button/index.js";
  import { buttonVariants } from "$lib/components/ui/button/button.svelte";
  import * as Card from "$lib/components/ui/card/index.js";
  import * as Command from "$lib/components/ui/command/index.js";
  import Input from "$lib/components/ui/input/input.svelte";
  import * as Popover from "$lib/components/ui/popover/index.js";
  import * as Select from "$lib/components/ui/select/index.js";
  import { cn } from "$lib/utils.js";
  import { Scale, Check, ChevronDown, Save } from "lucide-svelte";
  import { getJevConfig, setJevConfig } from "$lib/api/api.js";
  import { t } from "svelte-i18n";
  import type { JevConfig } from "$lib/api/schemas.js";

  let { token, onAuthExpired } = $props<{
    token: string;
    onAuthExpired: () => void;
  }>();

  let endpointURL = $state("");
  let apiKey = $state("");
  let apiKeySet = $state(false);
  let model = $state("");
  let proxyType = $state("direct");
  let proxyAddress = $state("");
  let loading = $state(true);
  let saving = $state(false);
  let saved = $state(false);
  let error = $state("");

  const endpointPresets = [
    { label: "jev.endpoint.preset.typesafe", value: "https://api.typesafe.ai/v1/systemone" },
    { label: "jev.endpoint.preset.openrouter", value: "https://openrouter.ai/api/alpha/decisions" },
    { label: "jev.endpoint.preset.vercel", value: "https://ai-gateway.vercel.sh/v1/evaluate" },
    { label: "jev.endpoint.preset.ollama", value: "http://localhost:11434/v1/systemone" },
  ];
  let endpointOpen = $state(false);

  const modelPresets = ["jev-latest", "jev-preview", "jev-1.13.0"];
  let modelOpen = $state(false);

  const proxyTypes = ["direct", "http", "https", "socks5", "socks5h"] as const;

  function splitProxy(value: string): { type: string; address: string } {
    const v = (value ?? "").trim();
    if (!v) return { type: "direct", address: "" };
    const match = v.match(/^([a-zA-Z][a-zA-Z0-9+.-]*):\/\/(.*)$/);
    const scheme = match?.[1].toLowerCase();
    if (scheme && (proxyTypes as readonly string[]).includes(scheme)) {
      return { type: scheme, address: match![2] };
    }
    return { type: "http", address: v };
  }

  function combineProxy(): string {
    if (proxyType === "direct") return "";
    const address = proxyAddress.trim().replace(/^[a-zA-Z][a-zA-Z0-9+.-]*:\/\//, "");
    return address ? `${proxyType}://${address}` : "";
  }

  function onProxyTypeChange(value: string) {
    proxyType = value;
  }

  function isUnauthorized(e: unknown): boolean {
    return (e as { response?: { status?: number } })?.response?.status === 401;
  }

  function applyConfig(cfg: JevConfig) {
    endpointURL = cfg.endpoint_url ?? "";
    apiKey = cfg.api_key ?? "";
    apiKeySet = cfg.api_key_set ?? Boolean(cfg.api_key);
    model = cfg.model ?? "";
    const proxy = splitProxy(cfg.proxy ?? "");
    proxyType = proxy.type;
    proxyAddress = proxy.address;
  }

  async function load() {
    if (!token) return;
    loading = true;
    error = "";
    try {
      applyConfig(await getJevConfig(token));
    } catch (e) {
      console.error("failed to load jev config", e);
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
      const cfg: JevConfig = {
        endpoint_url: endpointURL,
        api_key: apiKey.trim(),
        model,
        proxy: combineProxy(),
      };
      applyConfig(await setJevConfig(token, cfg));
      saved = true;
    } catch (e) {
      console.error("failed to save jev config", e);
      error = e instanceof Error ? e.message : $t("common.saveFailed");
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
      <Scale />
      {$t("jev.title")}
    </Card.Title>
    <Card.Description>{$t("jev.desc")}</Card.Description>
  </Card.Header>
  <Card.Content class="space-y-4">
    <div>
      <label for="jev-endpoint-url" class="text-sm font-medium text-foreground block mb-1"
        >{$t("jev.endpointUrlLabel")}</label>
      <div class="flex items-center gap-2">
        <Input
          id="jev-endpoint-url"
          type="text"
          placeholder="https://api.typesafe.ai/v1/systemone"
          bind:value={endpointURL}
          disabled={loading}
          spellcheck="false"
          autocomplete="off"
          class="flex-1" />
        <Popover.Root bind:open={endpointOpen}>
          <Popover.Trigger
            aria-label={$t("jev.endpoint.pick")}
            title={$t("jev.endpoint.pick")}
            class={buttonVariants({ variant: "outline", size: "icon" })}
            disabled={loading}>
            <ChevronDown />
          </Popover.Trigger>
          <Popover.Content side="top" align="end" sideOffset={4} class="w-72 p-0">
            <Command.Root>
              <Command.List>
                <Command.Empty>{$t("jev.endpoint.empty")}</Command.Empty>
                <Command.Group>
                  {#each endpointPresets as p}
                    <Command.Item
                      value={p.value}
                      onSelect={() => {
                        endpointURL = p.value;
                        endpointOpen = false;
                      }}>
                      <span class="flex-1 flex flex-col">
                        <span class="text-foreground">{$t(p.label)}</span>
                        <span class="text-muted-foreground text-xs">{p.value}</span>
                      </span>
                      <Check class={cn("size-4 shrink-0", endpointURL !== p.value && "opacity-0")} />
                    </Command.Item>
                  {/each}
                </Command.Group>
              </Command.List>
            </Command.Root>
          </Popover.Content>
        </Popover.Root>
      </div>
    </div>
    <div>
      <label for="jev-api-key" class="text-sm font-medium text-foreground block mb-1">{$t("jev.apiKeyLabel")}</label>
      <Input
        id="jev-api-key"
        type="text"
        placeholder="sk-..."
        bind:value={apiKey}
        disabled={loading}
        spellcheck="false"
        autocomplete="off" />
    </div>
    <div>
      <label for="jev-model" class="text-sm font-medium text-foreground block mb-1">{$t("jev.modelLabel")}</label>
      <div class="flex items-center gap-2">
        <Input
          id="jev-model"
          type="text"
          placeholder="jev-latest"
          bind:value={model}
          disabled={loading}
          spellcheck="false"
          autocomplete="off"
          class="flex-1" />
        <Popover.Root bind:open={modelOpen}>
          <Popover.Trigger
            aria-label={$t("jev.model.pick")}
            title={$t("jev.model.pick")}
            class={buttonVariants({ variant: "outline", size: "icon" })}
            disabled={loading}>
            <ChevronDown />
          </Popover.Trigger>
          <Popover.Content side="top" align="end" sideOffset={4} class="w-56 p-0">
            <Command.Root>
              <Command.List>
                <Command.Group>
                  {#each modelPresets as m}
                    <Command.Item
                      value={m}
                      onSelect={() => {
                        model = m;
                        modelOpen = false;
                      }}>
                      <Check class={cn("size-4 shrink-0", model !== m && "opacity-0")} />
                      {m}
                    </Command.Item>
                  {/each}
                </Command.Group>
              </Command.List>
            </Command.Root>
          </Popover.Content>
        </Popover.Root>
      </div>
    </div>
    <div>
      <label for="jev-proxy" class="text-sm font-medium text-foreground block mb-1">{$t("jev.proxyLabel")}</label>
      <div class="flex items-center gap-2">
        <Select.Root type="single" value={proxyType} onValueChange={onProxyTypeChange}>
          <Select.Trigger class="w-32 shrink-0" aria-label={$t("jev.proxyLabel")} disabled={loading}>
            {$t(`ai.proxyType.${proxyType}`)}
          </Select.Trigger>
          <Select.Content>
            {#each proxyTypes as type (type)}
              <Select.Item value={type}>{$t(`ai.proxyType.${type}`)}</Select.Item>
            {/each}
          </Select.Content>
        </Select.Root>
        <Input
          id="jev-proxy"
          type="text"
          placeholder={$t("ai.proxyPlaceholder")}
          bind:value={proxyAddress}
          disabled={loading || proxyType === "direct"}
          spellcheck="false"
          autocomplete="off"
          class="flex-1" />
      </div>
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
