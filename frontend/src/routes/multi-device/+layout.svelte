<script lang="ts">
  import { goto } from "$app/navigation";
  import { Button } from "$lib/components/ui/button";
  import * as NavigationMenu from "$lib/components/ui/navigation-menu/index.js";
  import { ArrowLeft } from "lucide-svelte";
</script>

<div class="flex flex-col bg-background h-full">
  <slot />
</div>
