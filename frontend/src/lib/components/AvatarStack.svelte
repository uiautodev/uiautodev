<script lang="ts">
  import * as Avatar from "$lib/components/ui/avatar/index.js";
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

<Avatar.Group class="items-center -space-x-1.5">
  {#each visible as occupant (occupant.id)}
    <UserAvatar id={occupant.id} name={occupant.name} avatarUrl={occupant.avatar_url} {size} badge />
  {/each}
  {#if hiddenCount > 0}
    <Avatar.GroupCount class="text-[10px] font-medium" style={`width:${size}px;height:${size}px`}>
      +{hiddenCount}
    </Avatar.GroupCount>
  {/if}
</Avatar.Group>
