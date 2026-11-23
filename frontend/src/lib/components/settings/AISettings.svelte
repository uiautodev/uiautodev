<script lang="ts">
  import { onMount } from "svelte";
  import { AsyncButton } from "$lib/components/ui/button/index.js";
  import { buttonVariants } from "$lib/components/ui/button/button.svelte";
  import * as Card from "$lib/components/ui/card/index.js";
  import * as Command from "$lib/components/ui/command/index.js";
  import Input from "$lib/components/ui/input/input.svelte";
  import * as Popover from "$lib/components/ui/popover/index.js";
  import * as Select from "$lib/components/ui/select/index.js";
  import Spinner from "$lib/components/ui/spinner/spinner.svelte";
  import { cn } from "$lib/utils.js";
  import { Bot, Check, ChevronDown, RefreshCw, Save, XCircle } from "lucide-svelte";
  import { getOpenAIConfig, getOpenAIModels, setOpenAIConfig } from "$lib/api/api.js";
  import { alertStore } from "$lib/stores/alert.svelte";
  import { t } from "svelte-i18n";
  import type { OpenAIConfig } from "$lib/api/schemas.js";

  let { token, onAuthExpired } = $props<{
    token: string;
    onAuthExpired: () => void;
  }>();

  let baseURL = $state("");
  let apiKey = $state("");
  let apiKeySet = $state(false);
  let model = $state("");
  let proxyType = $state("direct");
  let proxyAddress = $state("");
  let loading = $state(true);
  let saving = $state(false);
  let saved = $state(false);
  let error = $state("");

  let models = $state<string[]>([]);
  let checking = $state(false);
  let modelsFailed = $state(false);
  let modelsOpen = $state(false);

  const baseURLPresets = [
    { label: "ai.baseURL.preset.deepseek", value: "https://api.deepseek.com" },
    { label: "ai.baseURL.preset.openai", value: "https://api.openai.com/v1" },
    { label: "ai.baseURL.preset.glm", value: "https://open.bigmodel.cn/api/paas/v4" },
    { label: "ai.baseURL.preset.moonshot", value: "https://api.moonshot.cn/v1" },
    { label: "ai.baseURL.preset.qwen", value: "https://dashscope.aliyuncs.com/compatible-mode/v1" },
    { label: "ai.baseURL.preset.ollama", value: "http://localhost:11434/v1" },
  ];
  let baseURLOpen = $state(false);

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

  async function load() {
    if (!token) return;
    loading = true;
    error = "";
    try {
      const cfg = await getOpenAIConfig(token);
      baseURL = cfg.base_url ?? "";
      apiKey = cfg.api_key ?? "";
      apiKeySet = cfg.api_key_set ?? Boolean(cfg.api_key);
      model = cfg.model ?? "";
      const proxy = splitProxy(cfg.proxy ?? "");
      proxyType = proxy.type;
      proxyAddress = proxy.address;
    } catch (e) {
      console.error("failed to load openai config", e);
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
      const cfg: OpenAIConfig = { base_url: baseURL, api_key: apiKey.trim(), model, proxy: combineProxy() };
      const savedCfg = await setOpenAIConfig(token, cfg);
      baseURL = savedCfg.base_url ?? "";
      apiKey = savedCfg.api_key ?? "";
      apiKeySet = savedCfg.api_key_set ?? Boolean(savedCfg.api_key);
      model = savedCfg.model ?? "";
      const proxy = splitProxy(savedCfg.proxy ?? "");
      proxyType = proxy.type;
      proxyAddress = proxy.address;
      saved = true;
    } catch (e) {
      console.error("failed to save openai config", e);
      error = e instanceof Error ? e.message : $t("common.saveFailed");
      if (isUnauthorized(e)) onAuthExpired();
    } finally {
      saving = false;
    }
  }

  async function checkModels() {
    if (checking) return;
    checking = true;
    modelsFailed = false;
    try {
      models = await getOpenAIModels(token, baseURL.trim(), apiKey.trim(), combineProxy());
      modelsOpen = true;
    } catch (e) {
      models = [];
      modelsFailed = true;
      modelsOpen = false;
      console.error("failed to fetch openai models", e);
      const message =
        (e as { response?: { data?: { message?: string } } })?.response?.data?.message ??
        (e instanceof Error ? e.message : $t("ai.fetchModelsFailed"));
      alertStore.error(message, { title: $t("ai.fetchModelsFailed") });
    } finally {
      checking = false;
    }
  }

  onMount(load);
</script>

<Card.Root>
  <Card.Header>
    <Card.Title class="flex items-center gap-2">
      <Bot />
      {$t("ai.title")}
    </Card.Title>
    <Card.Description>{$t("ai.desc")}</Card.Description>
  </Card.Header>
  <Card.Content class="space-y-4">
    <div>
      <label for="openai-base-url" class="text-sm font-medium text-foreground block mb-1"
        >{$t("ai.baseUrlLabel")}</label>
      <div class="flex items-center gap-2">
        <Input
          id="openai-base-url"
          type="text"
          placeholder="https://api.openai.com/v1"
          bind:value={baseURL}
          disabled={loading}
          spellcheck="false"
          autocomplete="off"
          class="flex-1" />
        <Popover.Root bind:open={baseURLOpen}>
          <Popover.Trigger
            aria-label={$t("ai.baseURL.pick")}
            title={$t("ai.baseURL.pick")}
            class={buttonVariants({ variant: "outline", size: "icon" })}
            disabled={loading}>
            <ChevronDown />
          </Popover.Trigger>
          <Popover.Content side="top" align="end" sideOffset={4} class="w-72 p-0">
            <Command.Root>
              <Command.List>
                <Command.Empty>{$t("ai.baseURL.empty")}</Command.Empty>
                <Command.Group>
                  {#each baseURLPresets as p}
                    <Command.Item
                      value={p.value}
                      onSelect={() => {
                        baseURL = p.value;
                        baseURLOpen = false;
                      }}>
                      <span class="flex-1 flex flex-col">
                        <span class="text-foreground">{$t(p.label)}</span>
                        <span class="text-muted-foreground text-xs">{p.value}</span>
                      </span>
                      <Check class={cn("size-4 shrink-0", baseURL !== p.value && "opacity-0")} />
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
      <label for="openai-api-key" class="text-sm font-medium text-foreground block mb-1">{$t("ai.apiKeyLabel")}</label>
      <Input
        id="openai-api-key"
        type="text"
        placeholder="sk-..."
        bind:value={apiKey}
        disabled={loading}
        spellcheck="false"
        autocomplete="off" />
    </div>
    <div>
      <label for="openai-model" class="text-sm font-medium text-foreground block mb-1">{$t("ai.modelLabel")}</label>
      <div class="flex items-center gap-2">
        <Input
          id="openai-model"
          type="text"
          placeholder="gpt-4o"
          bind:value={model}
          disabled={loading}
          spellcheck="false"
          autocomplete="off"
          class="flex-1" />
        <Popover.Root
          bind:open={modelsOpen}
          onOpenChange={(open) => {
            if (open) {
              checkModels();
            } else {
              modelsOpen = false;
            }
          }}>
          <Popover.Trigger
            aria-label={$t("ai.detectModels")}
            title={modelsFailed ? $t("ai.detectFailed") : $t("ai.detectModels")}
            class={buttonVariants({ variant: "outline", size: "icon" })}
            disabled={loading || checking || !baseURL.trim() || (!apiKey.trim() && !apiKeySet)}>
            {#if checking}
              <Spinner />
            {:else if modelsFailed}
              <XCircle class="text-destructive" />
            {:else}
              <RefreshCw />
            {/if}
          </Popover.Trigger>
          <Popover.Content side="top" align="end" sideOffset={4} class="w-64 p-0">
            <Command.Root>
              <Command.Input placeholder={$t("ai.searchModels")} />
              <Command.List>
                <Command.Empty>{$t("ai.noModels")}</Command.Empty>
                <Command.Group>
                  {#each models as m}
                    <Command.Item
                      value={m}
                      onSelect={() => {
                        model = m;
                        modelsOpen = false;
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
      <label for="openai-proxy" class="text-sm font-medium text-foreground block mb-1">{$t("ai.proxyLabel")}</label>
      <div class="flex items-center gap-2">
        <Select.Root type="single" value={proxyType} onValueChange={onProxyTypeChange}>
          <Select.Trigger class="w-32 shrink-0" aria-label={$t("ai.proxyLabel")} disabled={loading}>
            {$t(`ai.proxyType.${proxyType}`)}
          </Select.Trigger>
          <Select.Content>
            {#each proxyTypes as type (type)}
              <Select.Item value={type}>{$t(`ai.proxyType.${type}`)}</Select.Item>
            {/each}
          </Select.Content>
        </Select.Root>
        <Input
          id="openai-proxy"
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
