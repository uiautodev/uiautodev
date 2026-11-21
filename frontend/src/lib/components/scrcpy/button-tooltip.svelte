<script lang="ts">
  import * as Tooltip from "$lib/components/ui/tooltip/index.js";
  import Button, { type ButtonProps } from "$lib/components/ui/button/button.svelte";
  import type { Snippet } from "svelte";

  type Props = ButtonProps & { tooltip: string; children?: Snippet; side?: "top" | "right" | "bottom" | "left" };

  let { tooltip, children, side = "right", ...buttonProps }: Props = $props();
</script>

<Tooltip.Root>
  <Tooltip.Trigger>
    {#snippet child({ props })}
      <Button {...props} {...buttonProps}>
        {@render children?.()}
      </Button>
    {/snippet}
  </Tooltip.Trigger>
  <Tooltip.Content {side}>
    <p>{tooltip}</p>
  </Tooltip.Content>
</Tooltip.Root>
