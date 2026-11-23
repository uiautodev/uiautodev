<script lang="ts">
  import { onDestroy, onMount, type Snippet } from "svelte";
  import { goto } from "$app/navigation";
  import { t } from "svelte-i18n";
  import { Button } from "$lib/components/ui/button/index.js";
  import * as Card from "$lib/components/ui/card/index.js";
  import Spinner from "$lib/components/ui/spinner/spinner.svelte";
  import AvatarStack from "$lib/components/AvatarStack.svelte";
  import { deleteDevicePresenceKeepalive, putDevicePresence } from "$lib/api/api";
  import { alertStore } from "$lib/stores/alert.svelte";
  import { identityStore } from "$lib/stores/identity.svelte";
  import type { DeviceAccessMode, Occupancy } from "$lib/api/schemas";
  import { Eye, Lock, ShieldAlert } from "lucide-svelte";

  let {
    deviceId,
    initialMode = "control",
    onstate,
    children,
  }: {
    deviceId: string;
    initialMode?: DeviceAccessMode;
    onstate?: (occupancy: Occupancy | null) => void;
    children: Snippet;
  } = $props();

  const heartbeatIntervalMs = 30_000;

  let occupancy = $state<Occupancy | null>(null);
  let mode = $state<DeviceAccessMode>("control");
  let ready = $state(false);
  let blocked = $state<Occupancy | null>(null);
  let error = $state("");
  let pending = $state(false);

  let heartbeat: ReturnType<typeof setInterval> | undefined;

  const mineId = $derived(identityStore.me?.id ?? "");

  function emit(next: Occupancy | null) {
    occupancy = next;
    onstate?.(next);
  }

  function stopHeartbeat() {
    if (heartbeat) {
      clearInterval(heartbeat);
      heartbeat = undefined;
    }
  }

  function readPresenceError(e: unknown): { status?: number; occupancy?: Occupancy } {
    const response = (e as { response?: { status?: number; data?: { data?: Occupancy } } }).response;
    const payload = response?.data?.data;
    const occupancy = payload && typeof payload === "object" && "state" in payload ? payload : undefined;
    return { status: response?.status, occupancy };
  }

  function handleError(e: unknown) {
    const { status, occupancy: next } = readPresenceError(e);
    if (status === 423 && next) {
      blocked = next;
      ready = false;
      emit(next);
      return;
    }
    if (status === 403 && next) {
      blocked = null;
      mode = "view";
      ready = true;
      emit(next);
      return;
    }
    if (status === 409) {
      alertStore.warning(
        $t("presence.preemptedToast", { values: { name: next?.notice?.by?.name ?? $t("presence.someone") } }),
      );
      goto("/");
      return;
    }
    error = e instanceof Error ? e.message : String(e);
    if (next) emit(next);
  }

  async function enter(nextMode: DeviceAccessMode, takeover = false) {
    if (pending) return;
    pending = true;
    error = "";
    try {
      const next = await putDevicePresence(deviceId, nextMode, takeover);
      mode = nextMode;
      blocked = null;
      ready = true;
      emit(next);
      startHeartbeat();
    } catch (e) {
      handleError(e);
    } finally {
      pending = false;
    }
  }

  async function heartbeatOnce() {
    if (!ready) return;
    try {
      emit(await putDevicePresence(deviceId, mode));
    } catch (e) {
      handleError(e);
    }
  }

  function startHeartbeat() {
    stopHeartbeat();
    heartbeat = setInterval(() => {
      if (typeof document !== "undefined" && document.hidden) return;
      void heartbeatOnce();
    }, heartbeatIntervalMs);
  }

  function onVisibilityChange() {
    if (!document.hidden && ready) void heartbeatOnce();
  }

  function onPageHide() {
    deleteDevicePresenceKeepalive(deviceId);
  }

  onMount(() => {
    void identityStore.ensure();
    void enter(initialMode);
    document.addEventListener("visibilitychange", onVisibilityChange);
    window.addEventListener("pagehide", onPageHide);
  });

  onDestroy(() => {
    stopHeartbeat();
    document.removeEventListener("visibilitychange", onVisibilityChange);
    window.removeEventListener("pagehide", onPageHide);
    deleteDevicePresenceKeepalive(deviceId);
  });
</script>

<div class="flex min-h-0 flex-1 flex-col">
  {#if mode === "view" && ready}
    <div
      class="flex shrink-0 flex-wrap items-center gap-2 border-b border-amber-200 bg-amber-50 px-4 py-2 text-sm text-amber-900 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-200">
      <Eye class="size-4" />
      <span class="font-medium">{$t("presence.readOnly")}</span>
      <span class="text-amber-800/80 dark:text-amber-200/70">{$t("presence.readOnlyDesc")}</span>
      <div class="ml-auto flex items-center gap-2">
        {#if occupancy?.occupants?.length}
          <AvatarStack occupants={occupancy.occupants} {mineId} size={20} max={3} />
        {/if}
        {#if occupancy?.can_takeover}
          <Button size="sm" variant="outline" onclick={() => enter("control", true)} disabled={pending}>
            <Lock class="size-4 mr-1" />
            {$t("presence.requestControl")}
          </Button>
        {/if}
      </div>
    </div>
  {/if}

  <div class="flex min-h-0 flex-1">
    {#if !ready && blocked}
      <div class="flex w-full items-center justify-center p-6">
        <Card.Root class="w-full max-w-md">
          <Card.Header>
            <Card.Title class="flex items-center gap-2">
              <ShieldAlert class="text-amber-500" />
              {$t("presence.busyTitle")}
            </Card.Title>
            <Card.Description>
              {$t("presence.inUseBy", { values: { name: blocked.occupants?.[0]?.name ?? $t("presence.someone") } })}
            </Card.Description>
          </Card.Header>
          <Card.Content class="space-y-4">
            {#if blocked.occupants?.length}
              <div class="flex items-center gap-3">
                <AvatarStack occupants={blocked.occupants} {mineId} size={32} max={4} />
                <span class="text-sm text-muted-foreground">{$t("presence.busyDesc")}</span>
              </div>
            {:else}
              <p class="text-sm text-muted-foreground">{$t("presence.busyDesc")}</p>
            {/if}
            <p class="text-xs text-muted-foreground">{$t("presence.freeHint")}</p>
          </Card.Content>
          <Card.Footer class="flex flex-wrap justify-end gap-2">
            <Button variant="outline" onclick={() => goto("/")}>{$t("presence.cancel")}</Button>
            {#if blocked.can_observe}
              <Button variant="secondary" onclick={() => enter("view")} disabled={pending}>
                <Eye class="size-4 mr-1" />
                {$t("presence.observeOnly")}
              </Button>
            {/if}
            {#if blocked.can_takeover}
              <Button onclick={() => enter("control", true)} disabled={pending}>
                <Lock class="size-4 mr-1" />
                {$t("presence.takeover")}
              </Button>
            {/if}
          </Card.Footer>
        </Card.Root>
      </div>
    {:else if error}
      <div class="flex w-full items-center justify-center p-6">
        <Card.Root class="w-full max-w-md">
          <Card.Header>
            <Card.Title>{$t("presence.acquireError")}</Card.Title>
            <Card.Description>{error}</Card.Description>
          </Card.Header>
          <Card.Footer class="justify-end gap-2">
            <Button variant="outline" onclick={() => goto("/")}>{$t("presence.cancel")}</Button>
            <Button onclick={() => enter("control")} disabled={pending}>{$t("common.refresh")}</Button>
          </Card.Footer>
        </Card.Root>
      </div>
    {:else if !ready}
      <div class="flex w-full flex-col items-center justify-center gap-3">
        <Spinner class="size-8" />
        <span class="text-sm text-muted-foreground">{$t("presence.acquiring")}</span>
      </div>
    {:else}
      {@render children()}
    {/if}
  </div>
</div>
