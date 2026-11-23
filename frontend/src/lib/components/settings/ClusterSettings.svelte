<script lang="ts">
  import { onMount } from "svelte";
  import { Button } from "$lib/components/ui/button/index.js";
  import * as AlertDialog from "$lib/components/ui/alert-dialog/index.js";
  import * as Card from "$lib/components/ui/card/index.js";
  import Input from "$lib/components/ui/input/input.svelte";
  import * as Select from "$lib/components/ui/select/index.js";
  import { Switch } from "$lib/components/ui/switch";
  import { CopyButton } from "$lib/components/ui/copy-button";
  import { Check, Network, Pencil, Plus, RefreshCw, Trash2 } from "lucide-svelte";
  import {
    baseURL,
    createClusterSlave,
    deleteClusterSlave,
    listClusterSlaves,
    rotateClusterSlaveToken,
    updateClusterSlave,
  } from "$lib/api/api.js";
  import type { ClusterSlave, ClusterSlaveMode, ClusterSlaveTokenResult } from "$lib/api/schemas.js";
  import { isWailsEnv } from "$lib/compat.js";
  import { t } from "svelte-i18n";

  let { token, onAuthExpired } = $props<{
    token: string;
    onAuthExpired: () => void;
  }>();

  let slaves = $state<ClusterSlave[]>([]);
  let loading = $state(true);
  let refreshing = $state(false);
  let refreshDone = $state(false);
  let refreshDoneTimer: ReturnType<typeof setTimeout> | undefined;
  let error = $state("");
  let name = $state("");
  // The desktop app listens on localhost only, so remote slaves cannot dial it;
  // only direct mode (master dials the slave) is usable there.
  const desktop = isWailsEnv();
  let mode = $state<ClusterSlaveMode>(desktop ? "direct" : "push");
  let url = $state("");
  let username = $state("");
  let password = $state("");
  let insecureTls = $state(false);
  let creating = $state(false);
  let created = $state<ClusterSlaveTokenResult | null>(null);
  let directSetup = $state<{ url: string; username: string; password: string } | null>(null);
  let tokenDialogOpen = $state(false);
  let busyId = $state("");

  let editOpen = $state(false);
  let editSlave = $state<ClusterSlave | null>(null);
  let editUrl = $state("");
  let editUsername = $state("");
  let editPassword = $state("");
  let editInsecureTls = $state(false);
  let savingEdit = $state(false);

  // The desktop app is served from the Wails asset origin, so the master URL
  // slaves should dial is the API origin (baseURL), not window.location.origin.
  const masterOrigin = baseURL || (typeof window !== "undefined" ? window.location.origin : "");

  const canAdd = $derived(!loading && !!name.trim() && (mode === "push" || !!url.trim()));

  function isUnauthorized(e: unknown): boolean {
    return (e as { response?: { status?: number } })?.response?.status === 401;
  }

  function errorMessage(e: unknown, fallback: string): string {
    if (isUnauthorized(e)) onAuthExpired();
    return e instanceof Error ? e.message : fallback;
  }

  async function load(silent = false): Promise<boolean> {
    if (!silent && slaves.length === 0) loading = true;
    error = "";
    try {
      slaves = await listClusterSlaves(token);
      return true;
    } catch (e) {
      error = errorMessage(e, $t("cluster.loadFailed"));
      return false;
    } finally {
      loading = false;
    }
  }

  async function refresh() {
    if (refreshing) return;
    refreshing = true;
    const ok = await load(true);
    refreshing = false;
    if (!ok) return;
    refreshDone = true;
    if (refreshDoneTimer) clearTimeout(refreshDoneTimer);
    refreshDoneTimer = setTimeout(() => (refreshDone = false), 1500);
  }

  async function add() {
    const trimmed = name.trim();
    if (!trimmed || !canAdd) return;
    if (slaves.some((slave) => slave.name.toLowerCase() === trimmed.toLowerCase())) {
      error = $t("cluster.duplicateName");
      return;
    }
    creating = true;
    error = "";
    try {
      const result =
        mode === "direct"
          ? await createClusterSlave(token, {
              name: trimmed,
              mode: "direct",
              direct: {
                url: url.trim(),
                username: username.trim(),
                password,
                insecure_tls: insecureTls,
              },
            })
          : await createClusterSlave(token, { name: trimmed, mode: "push" });
      created = result;
      directSetup = mode === "direct" ? { url: url.trim(), username: username.trim(), password } : null;
      tokenDialogOpen = true;
      name = "";
      url = "";
      username = "";
      password = "";
      insecureTls = false;
      await load();
    } catch (e) {
      error = errorMessage(e, $t("cluster.addFailed"));
    } finally {
      creating = false;
    }
  }

  async function rotate(slave: ClusterSlave) {
    if (!confirm($t("cluster.rotateConfirm", { values: { name: slave.name } }))) return;
    busyId = slave.id;
    error = "";
    try {
      created = await rotateClusterSlaveToken(token, slave.id);
      directSetup = null;
      tokenDialogOpen = true;
      await load();
    } catch (e) {
      error = errorMessage(e, $t("cluster.rotateFailed"));
    } finally {
      busyId = "";
    }
  }

  async function remove(slave: ClusterSlave) {
    if (!confirm($t("cluster.deleteConfirm", { values: { name: slave.name } }))) return;
    busyId = slave.id;
    error = "";
    try {
      await deleteClusterSlave(token, slave.id);
      await load();
    } catch (e) {
      error = errorMessage(e, $t("cluster.deleteFailed"));
    } finally {
      busyId = "";
    }
  }

  async function setEnabled(slave: ClusterSlave, enabled: boolean) {
    if (slave.disabled === !enabled) return;
    busyId = slave.id;
    error = "";
    try {
      await updateClusterSlave(token, slave.id, { disabled: !enabled });
      await load(true);
    } catch (e) {
      error = errorMessage(e, $t("cluster.updateFailed"));
    } finally {
      busyId = "";
    }
  }

  async function setIncludeNested(slave: ClusterSlave, includeNested: boolean) {
    if (!!slave.include_nested === includeNested) return;
    busyId = slave.id;
    error = "";
    try {
      await updateClusterSlave(token, slave.id, { include_nested: includeNested });
      await load(true);
    } catch (e) {
      error = errorMessage(e, $t("cluster.updateFailed"));
    } finally {
      busyId = "";
    }
  }

  function openEdit(slave: ClusterSlave) {
    editSlave = slave;
    editUrl = slave.url ?? "";
    editUsername = slave.username ?? "";
    editPassword = "";
    editInsecureTls = slave.insecure_tls === true;
    editOpen = true;
  }

  async function saveEdit() {
    if (!editSlave) return;
    if (!editUrl.trim()) {
      error = $t("cluster.directFieldsRequired");
      return;
    }
    savingEdit = true;
    error = "";
    try {
      await updateClusterSlave(token, editSlave.id, {
        direct: {
          url: editUrl.trim(),
          username: editUsername.trim(),
          password: editPassword,
          insecure_tls: editInsecureTls,
        },
      });
      editOpen = false;
      await load(true);
    } catch (e) {
      error = errorMessage(e, $t("cluster.updateFailed"));
    } finally {
      savingEdit = false;
    }
  }

  function commandFor(result: ClusterSlaveTokenResult): string {
    if (result.slave.mode === "direct") {
      const usernameValue = directSetup?.username || result.slave.username || "";
      const passwordValue = directSetup?.password || "";
      if (!usernameValue) {
        return `uiautodev-server -addr 0.0.0.0:33299`;
      }
      return `uiautodev-server -addr 0.0.0.0:33299 -auth ${usernameValue}:${passwordValue}`;
    }
    return `npx uiautodev run -master-url ${masterOrigin} -token ${result.token ?? ""}`;
  }

  function formatTime(value: string): string {
    if (!value) return "—";
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return "—";
    return date.toLocaleString();
  }

  onMount(() => {
    load();
    const timer = setInterval(() => load(true), 5000);
    return () => {
      clearInterval(timer);
      if (refreshDoneTimer) clearTimeout(refreshDoneTimer);
    };
  });
</script>

<Card.Root>
  <Card.Header>
    <Card.Title class="flex items-center gap-2">
      <Network />
      {$t("cluster.title")}
    </Card.Title>
    <Card.Description>{$t("cluster.desc")}</Card.Description>
  </Card.Header>
  <Card.Content class="space-y-4">
    <div class="flex flex-col gap-3">
      <div class="flex flex-col gap-2 sm:flex-row sm:items-end">
        <div class="flex-1">
          <label for="cluster-name" class="text-sm font-medium text-foreground block mb-1"
            >{$t("cluster.nameLabel")}</label>
          <Input id="cluster-name" bind:value={name} placeholder={$t("cluster.namePlaceholder")} disabled={creating} />
        </div>
        <div class="w-full sm:w-48">
          <label for="cluster-mode" class="text-sm font-medium text-foreground block mb-1"
            >{$t("cluster.modeLabel")}</label>
          {#if desktop}
            <div
              id="cluster-mode"
              class="rounded-md border border-border px-3 py-2 text-sm text-muted-foreground"
              title={$t("cluster.modeDesktopHint")}>
              {$t("cluster.modeDirect")}
            </div>
          {:else}
            <Select.Root type="single" value={mode} onValueChange={(value) => (mode = value as ClusterSlaveMode)}>
              <Select.Trigger id="cluster-mode" class="w-full" disabled={creating}>
                {$t(mode === "direct" ? "cluster.modeDirect" : "cluster.modePush")}
              </Select.Trigger>
              <Select.Content>
                <Select.Item value="push">{$t("cluster.modePush")}</Select.Item>
                <Select.Item value="direct">{$t("cluster.modeDirect")}</Select.Item>
              </Select.Content>
            </Select.Root>
          {/if}
        </div>
      </div>

      {#if mode === "direct"}
        <div class="grid gap-2">
          <div>
            <label for="cluster-url" class="text-sm font-medium text-foreground block mb-1"
              >{$t("cluster.urlLabel")}</label>
            <Input
              id="cluster-url"
              bind:value={url}
              placeholder={$t("cluster.urlPlaceholder")}
              disabled={creating}
              spellcheck="false" />
          </div>
          <div class="grid gap-2 sm:grid-cols-2">
            <div>
              <label for="cluster-username" class="text-sm font-medium text-foreground block mb-1"
                >{$t("cluster.usernameLabel")}</label>
              <Input id="cluster-username" bind:value={username} disabled={creating} autocomplete="username" />
            </div>
            <div>
              <label for="cluster-password" class="text-sm font-medium text-foreground block mb-1"
                >{$t("cluster.passwordLabel")}</label>
              <Input
                id="cluster-password"
                type="password"
                bind:value={password}
                disabled={creating}
                autocomplete="new-password" />
            </div>
          </div>
          <p class="text-xs text-muted-foreground">{$t("cluster.credentialsOptionalHint")}</p>
          <label class="flex items-center gap-2 text-sm text-foreground">
            <input type="checkbox" bind:checked={insecureTls} class="size-4 accent-primary" />
            {$t("cluster.insecureTLS")}
          </label>
          <p class="text-xs text-muted-foreground">{$t("cluster.httpWarning")}</p>
        </div>
      {/if}

      <div class="flex flex-wrap items-center gap-2">
        <Button onclick={add} disabled={creating || !canAdd}>
          <Plus />
          {$t(mode === "direct" ? "cluster.addDirect" : "cluster.add")}
        </Button>
      </div>
    </div>

    {#if error}
      <p class="text-sm text-destructive">{error}</p>
    {/if}

    <div class="rounded-md border border-border">
      <div class="flex items-center justify-between border-b border-border px-3 py-2">
        <span class="text-sm font-medium">{$t("cluster.slaveCount", { values: { count: slaves.length } })}</span>
        <Button variant="ghost" size="sm" onclick={refresh} disabled={refreshing || loading}>
          {#if refreshDone}
            <Check class="size-4" />
          {:else}
            <RefreshCw class="size-4 {refreshing ? 'animate-spin' : ''}" />
          {/if}
        </Button>
      </div>
      {#if loading}
        <p class="px-3 py-6 text-center text-sm text-muted-foreground">{$t("cluster.loading")}</p>
      {:else if slaves.length === 0}
        <p class="px-3 py-6 text-center text-sm text-muted-foreground">{$t("cluster.empty")}</p>
      {:else}
        <ul class="divide-y divide-border">
          {#each slaves as slave (slave.id)}
            <li class="flex flex-col gap-2 px-3 py-3 sm:flex-row sm:items-center">
              <div class="min-w-0 flex-1">
                <div class="flex items-center gap-2">
                  <span
                    class="size-2 rounded-full {slave.disabled
                      ? 'bg-amber-500'
                      : slave.online
                        ? 'bg-green-500'
                        : 'bg-slate-400'}"
                    aria-hidden="true"></span>
                  <span class="truncate text-sm font-medium text-foreground">{slave.name}</span>
                  <span class="text-xs text-muted-foreground">
                    {slave.disabled
                      ? $t("cluster.disabled")
                      : slave.online
                        ? $t("cluster.online")
                        : $t("cluster.offline")}
                  </span>
                </div>
                <div class="mt-1 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                  <span class="font-mono">{slave.id}</span>
                  <span>{$t(slave.mode === "direct" ? "cluster.modeDirect" : "cluster.modePush")}</span>
                  {#if slave.url}<span class="max-w-full truncate">{slave.url}</span>{/if}
                  {#if slave.version}<span>v{slave.version}</span>{/if}
                  <span>{$t("cluster.devices", { values: { count: slave.device_count } })}</span>
                  <span>{$t("cluster.lastSeen")}: {formatTime(slave.last_seen)}</span>
                </div>
              </div>
              <div class="flex shrink-0 items-center gap-2">
                <label class="flex items-center gap-1.5 text-xs text-muted-foreground">
                  <Switch
                    checked={!!slave.include_nested}
                    disabled={busyId === slave.id}
                    title={$t("cluster.includeNestedHint")}
                    aria-label={$t("cluster.includeNested")}
                    onCheckedChange={(includeNested) => setIncludeNested(slave, includeNested)} />
                  {$t("cluster.includeNested")}
                </label>
                <Switch
                  checked={!slave.disabled}
                  disabled={busyId === slave.id}
                  title={$t(slave.disabled ? "cluster.enable" : "cluster.disable")}
                  aria-label={$t(slave.disabled ? "cluster.enable" : "cluster.disable")}
                  onCheckedChange={(enabled) => setEnabled(slave, enabled)} />
                {#if slave.mode === "direct"}
                  <Button variant="outline" size="sm" onclick={() => openEdit(slave)} disabled={busyId === slave.id}>
                    <Pencil />
                    {$t("cluster.edit")}
                  </Button>
                {:else}
                  <Button variant="outline" size="sm" onclick={() => rotate(slave)} disabled={busyId === slave.id}>
                    <RefreshCw />
                    {$t("cluster.rotate")}
                  </Button>
                {/if}
                <Button
                  variant="outline"
                  size="icon"
                  onclick={() => remove(slave)}
                  disabled={busyId === slave.id}
                  title={$t("cluster.delete")}
                  aria-label={$t("cluster.delete")}>
                  <Trash2 />
                </Button>
              </div>
            </li>
          {/each}
        </ul>
      {/if}
    </div>

    <AlertDialog.Root
      bind:open={tokenDialogOpen}
      onOpenChange={(open) => {
        if (!open) {
          created = null;
          directSetup = null;
        }
      }}>
      <AlertDialog.Content>
        <AlertDialog.Header>
          <AlertDialog.Title>
            {$t(created?.slave.mode === "direct" ? "cluster.directSetupTitle" : "cluster.tokenTitle")}
          </AlertDialog.Title>
          <AlertDialog.Description>
            {$t(created?.slave.mode === "direct" ? "cluster.directSetupHint" : "cluster.tokenHint")}
          </AlertDialog.Description>
        </AlertDialog.Header>
        {#if created}
          {#if created.slave.mode === "direct"}
            <div class="flex flex-col gap-2">
              <div class="flex items-center gap-2">
                <code class="min-w-0 flex-1 break-all rounded bg-muted px-2 py-1 font-mono text-xs"
                  >{commandFor(created)}</code>
                <CopyButton text={commandFor(created)} />
              </div>
              <p class="text-xs text-muted-foreground">
                {$t("cluster.directSetupUrlHint", { values: { url: created.slave.url ?? "" } })}
              </p>
            </div>
          {:else}
            <div class="flex flex-col gap-2">
              <div class="flex items-center gap-2">
                <code class="min-w-0 flex-1 break-all rounded bg-muted px-2 py-1 font-mono text-xs"
                  >{created.token ?? ""}</code>
                <CopyButton text={created.token ?? ""} />
              </div>
              <div class="flex items-center gap-2">
                <code class="min-w-0 flex-1 break-all rounded bg-muted px-2 py-1 font-mono text-xs"
                  >{commandFor(created)}</code>
                <CopyButton text={commandFor(created)} />
              </div>
            </div>
          {/if}
        {/if}
        <AlertDialog.Footer>
          <AlertDialog.Cancel>{$t("cluster.close")}</AlertDialog.Cancel>
        </AlertDialog.Footer>
      </AlertDialog.Content>
    </AlertDialog.Root>

    <AlertDialog.Root
      bind:open={editOpen}
      onOpenChange={(open) => {
        if (!open) editPassword = "";
      }}>
      <AlertDialog.Content>
        <AlertDialog.Header>
          <AlertDialog.Title>{$t("cluster.editDirectTitle")}</AlertDialog.Title>
          <AlertDialog.Description>{$t("cluster.editDirectDesc")}</AlertDialog.Description>
        </AlertDialog.Header>
        <div class="flex flex-col gap-3">
          <div>
            <label for="cluster-edit-url" class="text-sm font-medium text-foreground block mb-1"
              >{$t("cluster.urlLabel")}</label>
            <Input id="cluster-edit-url" bind:value={editUrl} spellcheck="false" />
          </div>
          <div class="grid gap-3 sm:grid-cols-2">
            <div>
              <label for="cluster-edit-username" class="text-sm font-medium text-foreground block mb-1"
                >{$t("cluster.usernameLabel")}</label>
              <Input id="cluster-edit-username" bind:value={editUsername} autocomplete="username" />
            </div>
            <div>
              <label for="cluster-edit-password" class="text-sm font-medium text-foreground block mb-1"
                >{$t("cluster.passwordLabel")}</label>
              <Input
                id="cluster-edit-password"
                type="password"
                bind:value={editPassword}
                placeholder={$t("cluster.passwordKeepHint")}
                autocomplete="new-password" />
            </div>
          </div>
          <label class="flex items-center gap-2 text-sm text-foreground">
            <input type="checkbox" bind:checked={editInsecureTls} class="size-4 accent-primary" />
            {$t("cluster.insecureTLS")}
          </label>
        </div>
        <AlertDialog.Footer>
          <AlertDialog.Cancel>{$t("cluster.cancel")}</AlertDialog.Cancel>
          <Button onclick={saveEdit} disabled={savingEdit || !editUrl.trim()}>
            {$t("cluster.save")}
          </Button>
        </AlertDialog.Footer>
      </AlertDialog.Content>
    </AlertDialog.Root>
  </Card.Content>
</Card.Root>
