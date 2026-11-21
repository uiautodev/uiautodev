<script lang="ts">
  import { Pencil } from "lucide-svelte";
  import { Button } from "$lib/components/ui/button/index.js";
  import Input from "$lib/components/ui/input/input.svelte";
  import { setDeviceMeta } from "$lib/api/api.js";
  import type { DeviceMeta } from "$lib/api/schemas.js";
  import { alertStore } from "$lib/stores/alert.svelte";
  import { cn } from "$lib/utils.js";
  import { t } from "svelte-i18n";

  let {
    deviceId,
    meta = {},
    class: className,
    onsaved,
  }: {
    deviceId: string;
    meta?: DeviceMeta;
    class?: string;
    onsaved?: (deviceId: string, meta: DeviceMeta) => void;
  } = $props();

  const remark = $derived(meta.remark ?? "");

  let editing = $state(false);
  let draft = $state("");
  let saving = $state(false);
  let inputEl = $state<HTMLInputElement | null>(null);

  $effect(() => {
    if (editing && inputEl) {
      inputEl.focus();
      inputEl.select();
    }
  });

  function startEdit() {
    draft = remark;
    editing = true;
  }

  function cancel() {
    editing = false;
  }

  async function save() {
    if (saving || !editing) return;
    const next = draft.trim();
    if (next === remark.trim()) {
      editing = false;
      return;
    }
    saving = true;
    editing = false;
    try {
      const updated: DeviceMeta = { ...meta };
      if (next) {
        updated.remark = next;
      } else {
        delete updated.remark;
      }
      const saved = await setDeviceMeta(deviceId, updated);
      onsaved?.(deviceId, saved ?? updated);
    } catch (e) {
      console.error("failed to save device remark", e);
      alertStore.error(e instanceof Error ? e.message : $t("common.saveFailed"), {
        title: $t("device.remark"),
      });
    } finally {
      saving = false;
    }
  }
</script>

{#if editing}
  <Input
    bind:ref={inputEl}
    bind:value={draft}
    disabled={saving}
    spellcheck="false"
    autocomplete="off"
    placeholder={$t("device.remarkPlaceholder")}
    class={cn("h-7 w-40 px-2 text-sm", className)}
    onkeydown={(e) => {
      if (e.key === "Enter") save();
      else if (e.key === "Escape") cancel();
    }}
    onblur={save} />
{:else}
  <Button
    variant="ghost"
    size="sm"
    class={cn("group h-7 max-w-full justify-start gap-1 px-1.5 font-normal", className)}
    title={$t("device.remarkEdit")}
    onclick={startEdit}>
    {#if remark}
      <span class="min-w-0 truncate">{remark}</span>
    {:else}
      <span class="text-muted-foreground min-w-0 truncate italic">{$t("device.remarkEmpty")}</span>
    {/if}
    <Pencil class="text-muted-foreground size-3 shrink-0 opacity-0 transition-opacity group-hover:opacity-70" />
  </Button>
{/if}
