<script lang="ts">
  import { deviceStore } from "$lib/stores/device.svelte";
  import CopyButton from "$lib/components/ui/copy-button/copy-button.svelte";
  import { t } from "svelte-i18n";

  const node = $derived(deviceStore.nodeSelected);

  // Check if value is a boolean string (true/false or 1/0 for is-prefixed keys)
  function isBoolean(key: string, value: unknown): boolean {
    const str = String(value);
    const isPrefixed = key.startsWith("is");
    return str === "true" || str === "false" || (isPrefixed && (str === "1" || str === "0"));
  }

  // Get sorted properties (booleans at the bottom)
  const sortedProps = $derived(
    node?.props
      ? Object.entries(node.props).sort(([keyA, a], [keyB, b]) => {
          const aIsBool = isBoolean(keyA, a);
          const bIsBool = isBoolean(keyB, b);
          if (aIsBool && !bIsBool) return 1; // a goes after b
          if (!aIsBool && bIsBool) return -1; // a goes before b
          return 0; // keep original order for same type
        })
      : [],
  );

  // Get CSS class for styling different value types
  function getValueClass(key: string, value: unknown): string {
    const str = String(value);
    const isPrefixed = key.startsWith("is");
    if (str === "true" || (isPrefixed && str === "1")) return "text-green-500";
    if (str === "false" || (isPrefixed && str === "0")) return "text-red-500";
    return ""; // default styling
  }
</script>

<div class="p-2 space-y-2 overflow-auto">
  {#if node}
    <div class="grid grid-cols-1 gap-1 text-xs border-b pb-2">
      <div class="flex gap-1">
        <span class="text-muted-foreground shrink-0 w-10">{$t("device.inspect.tag")}:</span>
        <span class="font-mono truncate">{node.tagName}</span>
      </div>
      <div class="flex gap-1">
        <span class="text-muted-foreground shrink-0 w-10">{$t("device.inspect.size")}:</span>
        <span class="font-mono truncate">{node.rect.width}x{node.rect.height}</span>
      </div>
    </div>

    {#if node.props && Object.keys(node.props).length > 0}
      <div class="space-y-0.5">
        {#each sortedProps as [propKey, propValue]}
          <div
            class="group grid grid-cols-[minmax(80px,120px)_1fr_auto] gap-1 items-center hover:bg-muted/50 rounded px-1.5 py-0.5"
            title="{propKey}: {String(propValue)}">
            <span class="text-xs font-medium text-muted-foreground truncate">{propKey}</span>
            <span class="text-xs font-mono truncate {getValueClass(propKey, propValue)}">{String(propValue)}</span>
            <CopyButton
              text={String(propValue)}
              variant="ghost"
              size="icon"
              class="h-5 w-5 opacity-0 group-hover:opacity-100 transition-opacity" />
          </div>
        {/each}
      </div>
    {:else}
      <p class="text-xs text-muted-foreground">{$t("device.inspect.noProperties")}</p>
    {/if}
  {:else}
    <p class="text-xs text-muted-foreground">{$t("device.inspect.selectNode")}</p>
  {/if}
</div>
