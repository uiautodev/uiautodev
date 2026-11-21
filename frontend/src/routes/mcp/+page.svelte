<script lang="ts">
  import AppHeader from "$lib/components/AppHeader.svelte";
  import MCP from "$lib/components/mcp/MCP.svelte";
</script>

<div class="flex flex-col h-full bg-background">
  <AppHeader active="mcp" />

  <main class="flex-1 overflow-y-auto">
    <div class="max-w-3xl mx-auto">
      <MCP />
    </div>
  </main>
</div>
