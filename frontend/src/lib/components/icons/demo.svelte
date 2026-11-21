<script lang="ts">
  import { Sparkles } from "lucide-svelte";

  let { size = 24, active = false, ...restProps } = $props();
</script>

<Sparkles {size} color={active ? "#3b82f6" : "currentColor"} {...restProps} />
