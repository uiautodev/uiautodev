<script lang="ts">
  import * as Alert from "./index.js";
  import { CircleCheck, CircleX, Info, TriangleAlert, X } from "lucide-svelte";
  import { alertStore } from "$lib/stores/alert.svelte";
  import { cn } from "$lib/utils.js";

  const alertIcon = {
    default: Info,
    destructive: CircleX,
    info: Info,
    success: CircleCheck,
    warning: TriangleAlert,
    error: CircleX,
  } as const;
</script>

<div
  class="pointer-events-none fixed top-4 left-1/2 z-50 flex w-full max-w-md -translate-x-1/2 flex-col gap-2 px-4">
  {#each alertStore.alerts as item (item.id)}
    <Alert.Root
      variant={item.variant}
      class={cn(
        "pointer-events-auto animate-in fade-in slide-in-from-top-2 shadow-md duration-200",
        item.dismissible && "pr-8",
      )}>
      <svelte:component this={alertIcon[item.variant ?? "info"]} />
      {#if item.title}
        <Alert.Title>{item.title}</Alert.Title>
      {/if}
      <Alert.Description>{item.message}</Alert.Description>
      {#if item.dismissible}
        <button
          class="absolute top-3 right-3 rounded text-current/70 transition-colors hover:text-current"
          aria-label="关闭"
          onclick={() => alertStore.dismiss(item.id)}>
          <X size={14} />
        </button>
      {/if}
    </Alert.Root>
  {/each}
</div>