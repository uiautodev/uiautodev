<script lang="ts">
  import { api, baseURL, getMCPToken } from "$lib/api/api.js";
  import { onMount } from "svelte";
  import { Button } from "$lib/components/ui/button/index.js";
  import * as InputGroup from "$lib/components/ui/input-group/index.js";
  import CopyButton from "$lib/components/ui/copy-button/copy-button.svelte";
  import { Switch } from "$lib/components/ui/switch/index.js";
  import { Braces, BugPlay, ExternalLink, Lock } from "lucide-svelte";
  import { managementAuth } from "$lib/stores/management-auth.svelte";
  import OpenCodeIcon from "$lib/components/icons/opencode.svelte";
  import ClaudeIcon from "$lib/components/icons/claude.svelte";
  import CursorIcon from "$lib/components/icons/cursor.svelte";
  import PiIcon from "$lib/components/icons/pi.svelte";
  import * as Tabs from "$lib/components/ui/tabs/index.js";
  import { Textarea } from "$lib/components/ui/textarea/index.js";
  import cursorMcpInstallDarkSvg from "../icons/cursor-mcp-install-dark.svg";
  import { t } from "svelte-i18n";

  let mcpName = $state("uiautodev");
  let mcpToken = $state("");
  let tokenLocked = $state(false);
  let inProject = $state(true);
  let piLocal = $state(true);

  const getMcpAddress = () => {
    const base = baseURL || `${window.location.protocol}//${window.location.host}`;
    return `${base}/mcp/sse`;
  };

  // Streamable HTTP endpoint. Preferred transport for modern clients
  // (opencode v2, Pi); the legacy SSE path remains for older clients.
  const getStreamableMcpAddress = () => {
    const base = baseURL || `${window.location.protocol}//${window.location.host}`;
    return `${base}/mcp/http`;
  };

  type ToolInfo = {
    name: string;
    description: string;
  };

  let tools = $state<ToolInfo[]>([]);

  onMount(async () => {
    try {
      mcpToken = await getMCPToken(managementAuth.token);
    } catch (e) {
      if ((e as { response?: { status?: number } })?.response?.status === 401) {
        tokenLocked = true;
      }
    }

    try {
      // Tool metadata is public; the token is only needed for actual calls.
      const res = await api.get<ToolInfo[]>("/mcp/tools");
      tools = res.data;
    } catch (e) {
      console.error("failed to load MCP tools", e);
    }
  });

  const mcpAddress = getMcpAddress();
  const streamableMcpAddress = getStreamableMcpAddress();

  const opencodeCommand = $derived.by(() => {
    const headerFlag = authHeader ? ` --header "Authorization=${authHeader}"` : "";
    return `opencode mcp add ${mcpName} --url ${streamableMcpAddress}${headerFlag}`;
  });

  const authHeader = $derived(
    mcpToken ? `Bearer ${mcpToken}` : tokenLocked ? "Bearer YOUR_MCP_TOKEN" : "", // pragma: allowlist secret
  );

  const claudeCommand = $derived.by(() => {
    const scopeFlag = inProject ? " -s project" : "";
    const headerFlag = authHeader ? ` -H "Authorization: ${authHeader}"` : "";
    return `claude mcp add${scopeFlag} -t http${headerFlag} ${mcpName} ${streamableMcpAddress}`;
  });

  const mcpHeaders = $derived.by(() => (authHeader ? { Authorization: authHeader } : undefined));
  const mcpJson = $derived.by(() => {
    const cfg: { type: string; url: string; headers?: Record<string, string> } = {
      type: "http",
      url: streamableMcpAddress,
    };
    if (mcpHeaders) cfg.headers = mcpHeaders;
    return { mcpServers: { [mcpName]: cfg } };
  });

  const cursorDeeplink = $derived.by(() => {
    const config: { type: string; url: string; headers?: Record<string, string> } = {
      type: "http",
      url: streamableMcpAddress,
    };
    if (mcpHeaders) config.headers = mcpHeaders;
    const configJson = JSON.stringify(config);
    const base64Config = btoa(configJson);
    return `cursor://anysphere.cursor-deeplink/mcp/install?name=${encodeURIComponent(mcpName)}&config=${encodeURIComponent(base64Config)}`;
  });

  const inspectorHttpCommand = $derived.by(() => {
    const tokenParam = mcpToken ? `?token=${mcpToken}` : "";
    return `npx @modelcontextprotocol/inspector --server-url "${streamableMcpAddress}${tokenParam}" --transport http`;
  });

  const inspectorSseCommand = $derived.by(() => {
    const tokenParam = mcpToken ? `?token=${mcpToken}` : "";
    return `npx @modelcontextprotocol/inspector --server-url "${mcpAddress}${tokenParam}" --transport sse`;
  });

  const piCommand = $derived.by(() => {
    const localFlag = piLocal ? "-l " : "";
    const headerFlag = authHeader ? ` --header "Authorization=${authHeader}"` : "";
    return `pi mcp add ${localFlag}${mcpName} --url ${streamableMcpAddress}${headerFlag}`;
  });

  const piJson = $derived.by(() => {
    const cfg: { url: string; headers?: Record<string, string> } = { url: streamableMcpAddress };
    if (mcpHeaders) cfg.headers = mcpHeaders;
    return { mcpServers: { [mcpName]: cfg } };
  });

  const projectLinks: Record<string, { url: string; label: string }> = {
    opencode: { url: "https://opencode.ai", label: "opencode.ai" },
    claude: { url: "https://code.claude.com", label: "code.claude.com" },
    cursor: { url: "https://cursor.com", label: "cursor.com" },
    pi: { url: "https://pi.dev", label: "pi.dev" },
    "mcp-inspector": {
      url: "https://github.com/modelcontextprotocol/inspector",
      label: "github.com/modelcontextprotocol/inspector",
    },
  };
</script>

{#snippet commandField(value: string)}
  <InputGroup.Root>
    <InputGroup.Input {value} readonly />
    <InputGroup.Addon align="inline-end">
      <CopyButton text={value} variant="ghost" size="icon" class="size-6" />
    </InputGroup.Addon>
  </InputGroup.Root>
{/snippet}

{#snippet homeLink(link: { url: string; label: string })}
  <a
    href={link.url}
    target="_blank"
    rel="noreferrer"
    class="inline-flex items-center gap-1 mb-3 text-xs text-muted-foreground hover:text-foreground transition-colors">
    <ExternalLink size={12} />
    {link.label}
  </a>
{/snippet}

<div class="p-4">
  <!-- Header -->
  <div>
    <h3 class="text-xl font-semibold text-foreground">MCP Server</h3>
    <p class="text-sm text-muted-foreground mb-4">Model Context Protocol integration</p>

    {#if tokenLocked}
      <div
        class="mb-4 flex flex-wrap items-center gap-3 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-900 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-200">
        <Lock class="size-4 shrink-0" />
        <span class="min-w-0 flex-1">{$t("mcp.tokenLockedHint")}</span>
        <Button href="/config#mcp" variant="outline" size="sm">{$t("mcp.openSettings")}</Button>
      </div>
    {:else if !mcpToken}
      <p class="text-xs text-amber-600 dark:text-amber-400 mb-4">{$t("mcp.cardNoTokenHint")}</p>
    {/if}

    <Tabs.Root value="config" class="gap-4">
      <Tabs.List>
        <Tabs.Trigger value="config">
          <Braces size={20} class="text-muted-foreground" /> .mcp.json
        </Tabs.Trigger>
        <Tabs.Trigger value="opencode">
          <OpenCodeIcon size={20} class="text-muted-foreground" /> opencode
        </Tabs.Trigger>
        <Tabs.Trigger value="claude">
          <ClaudeIcon size={20} class="text-muted-foreground" /> Claude
        </Tabs.Trigger>
        <Tabs.Trigger value="cursor">
          <CursorIcon size={20} class="text-muted-foreground" /> Cursor
        </Tabs.Trigger>
        <Tabs.Trigger value="pi">
          <PiIcon size={20} class="text-muted-foreground" /> Pi
        </Tabs.Trigger>
        <Tabs.Trigger value="mcp-inspector">
          <BugPlay size={20} class="text-muted-foreground" />
          MCP Inspector
        </Tabs.Trigger>
      </Tabs.List>
      <Tabs.Content value="config">
        <Textarea
          class="font-mono"
          onfocus={(e) => e.currentTarget.select()}
          value={JSON.stringify(mcpJson, null, 2)} />
      </Tabs.Content>
      <Tabs.Content value="opencode">
        <div>{@render homeLink(projectLinks.opencode)}</div>
        {@render commandField(opencodeCommand)}
      </Tabs.Content>
      <Tabs.Content value="claude">
        <div>{@render homeLink(projectLinks.claude)}</div>
        {@render commandField(claudeCommand)}
        <div class="flex items-center gap-2 mt-4 pl-4">
          <Switch id="mcp-scope" checked={inProject} onCheckedChange={(c) => (inProject = c)} />
          <label for="mcp-scope" class="text-muted-foreground text-xs">scope: project</label>
        </div>
      </Tabs.Content>
      <Tabs.Content value="cursor">
        <div>{@render homeLink(projectLinks.cursor)}</div>
        <a href={cursorDeeplink}>
          <img src={cursorMcpInstallDarkSvg} alt="Add MCP server to Cursor" height="32" />
        </a>
      </Tabs.Content>
      <Tabs.Content value="pi">
        <div>{@render homeLink(projectLinks.pi)}</div>
        {@render commandField(piCommand)}
        <div class="flex items-center gap-2 mt-4 pl-4">
          <Switch id="mcp-pi-scope" checked={piLocal} onCheckedChange={(c) => (piLocal = c)} />
          <label for="mcp-pi-scope" class="text-muted-foreground text-xs">scope: project</label>
        </div>
        <p class="text-xs text-muted-foreground mt-4 pl-4">
          {$t("mcp.piHint")}
        </p>
        <Textarea
          class="font-mono mt-2"
          onfocus={(e) => e.currentTarget.select()}
          value={JSON.stringify(piJson, null, 2)} />
      </Tabs.Content>
      <Tabs.Content value="mcp-inspector">
        <div>{@render homeLink(projectLinks["mcp-inspector"])}</div>
        <div class="space-y-3">
          <div>
            <p class="text-muted-foreground text-xs mb-1">Streamable HTTP</p>
            {@render commandField(inspectorHttpCommand)}
          </div>
          <div>
            <p class="text-muted-foreground text-xs mb-1">SSE</p>
            {@render commandField(inspectorSseCommand)}
          </div>
        </div>
      </Tabs.Content>
    </Tabs.Root>
  </div>

  <!-- Tools Section -->
  <div class="mt-6">
    <div class="flex items-center justify-between mb-3">
      <h4 class="text-base font-medium text-foreground">Available Tools</h4>
      <span class="text-xs text-muted-foreground bg-muted px-2 py-1 rounded-full">{tools.length} tools</span>
    </div>

    <div class="p-4 border rounded-lg bg-background border-border">
      <div class="space-y-2">
        <ul>
          {#each tools as tool}
            <li>
              <span>{tool.name}</span> <span class="text-sm text-muted-foreground">- {tool.description}</span>
            </li>
          {/each}
        </ul>
      </div>
    </div>
  </div>
</div>
