<script lang="ts">
  let {
    id = "",
    name = "",
    avatarUrl = "",
    size = 24,
    class: className = "",
  }: {
    id?: string;
    name?: string;
    avatarUrl?: string;
    size?: number;
    class?: string;
  } = $props();

  let imageFailed = $state(false);

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

<span
  class="inline-flex shrink-0 items-center justify-center overflow-hidden rounded-full {className}"
  style={`width:${size}px;height:${size}px`}
  title={label}
  aria-label={label}>
  {#if avatarUrl && !imageFailed}
    <img
      src={avatarUrl}
      alt={label}
      class="h-full w-full object-cover"
      referrerpolicy="no-referrer"
      onerror={() => (imageFailed = true)} />
  {:else}
    <svg viewBox="0 0 5 5" class="h-full w-full" style={`background:${identicon.bg}`} role="img" aria-label={label}>
      {#each identicon.cells as cell}
        <rect x={cell.x} y={cell.y} width="1" height="1" fill={identicon.fg} shape-rendering="crispEdges" />
      {/each}
    </svg>
  {/if}
</span>
