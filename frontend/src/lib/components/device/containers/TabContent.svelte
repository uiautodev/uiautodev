<script lang="ts">
  import * as Tabs from "$lib/components/ui/tabs/index.js";
  let { value, children, class: className = "", ...restProps } = $props();
</script>

<Tabs.Content {value} class={`overflow-auto min-h-0 ${className}`} {...restProps}>
  {@render children?.()}
</Tabs.Content>
