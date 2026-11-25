<script lang="ts">
  import * as Avatar from "$lib/components/ui/avatar/index.js";

  let {
    id = "",
    name = "",
    avatarUrl = "",
    size = 24,
    badge = false,
    class: className = "",
  }: {
    id?: string;
    name?: string;
    avatarUrl?: string;
    size?: number;
    badge?: boolean;
    class?: string;
  } = $props();

  // FNV-1a keeps the identicon deterministic for the same identity id.
  function hashSeed(value: string): number {
    let hash = 0x811c9dc5;
    for (let i = 0; i < value.length; i++) {
      hash ^= value.charCodeAt(i);
      hash = Math.imul(hash, 0x01000193) >>> 0;
    }
    return hash >>> 0;
  }

  const identicon = $derived.by(() => {
    const seed = hashSeed(id || name || "anonymous");
    const hue = seed % 360;
    const fg = `hsl(${hue} 65% 45%)`;
    const bg = `hsl(${hue} 60% 92%)`;
    const cells: Array<{ x: number; y: number }> = [];
    for (let y = 0; y < 5; y++) {
      for (let x = 0; x < 3; x++) {
        const bit = (seed >>> ((y * 3 + x) % 32)) & 1 || (seed >>> ((y * 3 + x + 13) % 32)) & 1;
        if (!bit) continue;
        cells.push({ x, y });
        if (x < 2) cells.push({ x: 4 - x, y });
      }
    }
    return { fg, bg, cells };
  });

  const label = $derived(name || id);
</script>

<Avatar.Root
  class={className}
  style={`width:${size}px;height:${size}px`}
  title={label}
  aria-label={label}
  data-size="sm">
  {#if avatarUrl}
    <Avatar.Image src={avatarUrl} alt={label} referrerpolicy="no-referrer" />
  {/if}
  <Avatar.Fallback class="overflow-hidden">
    <svg viewBox="0 0 5 5" class="size-full" style={`background:${identicon.bg}`} aria-hidden="true">
      {#each identicon.cells as cell}
        <rect x={cell.x} y={cell.y} width="1" height="1" fill={identicon.fg} shape-rendering="crispEdges" />
      {/each}
    </svg>
  </Avatar.Fallback>
  {#if badge}
    <Avatar.Badge class="bg-emerald-500" />
  {/if}
</Avatar.Root>
