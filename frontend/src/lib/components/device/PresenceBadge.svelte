<script lang="ts">
  import { t } from "svelte-i18n";
  import { Eye } from "lucide-svelte";
  import AvatarStack from "$lib/components/AvatarStack.svelte";
  import type { Occupancy } from "$lib/api/schemas";

  let {
    occupancy,
    mineId = "",
    showAvatars = true,
  }: {
    occupancy?: Occupancy;
    mineId?: string;
    showAvatars?: boolean;
  } = $props();

  const state = $derived(occupancy?.state ?? "free");

  const tone = $derived.by(() => {
    switch (state) {
      case "mine":
      case "shared":
        return "text-emerald-600 dark:text-emerald-400";
      case "viewing":
        return "text-sky-600 dark:text-sky-400";
      case "busy":
        return "text-amber-600 dark:text-amber-400";
      case "preempted":
        return "text-destructive";
      default:
        return "text-muted-foreground";
    }
  });

  const dotClass = $derived.by(() => {
    switch (state) {
      case "mine":
      case "shared":
        return "bg-emerald-500";
      case "viewing":
        return "bg-sky-500";
      case "busy":
        return "bg-amber-500";
      case "preempted":
        return "bg-destructive";
      default:
        return "bg-muted-foreground/40";
    }
  });

  const observers = $derived(occupancy?.observers ?? []);
</script>

<div class="flex items-center gap-2 min-w-0">
  {#if occupancy}
    <span class="inline-flex items-center gap-1.5 whitespace-nowrap text-sm {tone}">
      <span class="size-2 rounded-full {dotClass}"></span>
      {$t(`presence.state.${state}`)}
    </span>
    {#if showAvatars && (occupancy.occupants?.length ?? 0) > 0}
      <AvatarStack occupants={occupancy.occupants ?? []} {mineId} size={20} max={3} />
    {/if}
    {#if observers.length > 0}
      <span class="inline-flex items-center gap-0.5 text-xs text-muted-foreground" title={$t("presence.observers")}>
        <Eye class="size-3.5" />
        {observers.length}
      </span>
    {/if}
  {/if}
</div>
