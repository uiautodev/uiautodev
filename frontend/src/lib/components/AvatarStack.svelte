<script lang="ts">
  import UserAvatar from "$lib/components/UserAvatar.svelte";
  import type { Occupant } from "$lib/api/schemas";

  let {
    occupants = [],
    mineId = "",
    max = 4,
    size = 24,
  }: {
    occupants?: Occupant[];
    mineId?: string;
    max?: number;
    size?: number;
  } = $props();

  const ordered = $derived.by(() => {
    const list = [...occupants];
    list.sort((a, b) => {
      if (a.id === mineId) return -1;
      if (b.id === mineId) return 1;
      return (b.last_seen ?? "").localeCompare(a.last_seen ?? "");
    });
    return list;
  });

  const visible = $derived(ordered.slice(0, max));
  const hiddenCount = $derived(Math.max(0, ordered.length - max));
</script>

<div class="flex items-center">
  {#each visible as occupant, index (occupant.id)}
    <span class={index === 0 ? "" : "-ml-1.5"} title={occupant.name}>
      <UserAvatar
        id={occupant.id}
        name={occupant.name}
        avatarUrl={occupant.avatar_url}
        {size}
        class="ring-2 ring-background" />
    </span>
  {/each}
  {#if hiddenCount > 0}
    <span
      class="-ml-1.5 inline-flex items-center justify-center rounded-full bg-muted text-[10px] font-medium text-muted-foreground ring-2 ring-background"
      style={`width:${size}px;height:${size}px`}>
      +{hiddenCount}
    </span>
  {/if}
</div>
