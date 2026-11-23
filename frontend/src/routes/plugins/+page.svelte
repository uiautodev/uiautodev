<script lang="ts">
  import { goto } from "$app/navigation";
  import { onMount } from "svelte";

  onMount(() => {
    goto("/config" + window.location.hash, { replaceState: true });
  });
</script>
