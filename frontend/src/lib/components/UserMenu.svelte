<script lang="ts">
  import { onMount } from "svelte";
  import { Button } from "$lib/components/ui/button/index.js";
  import * as Popover from "$lib/components/ui/popover/index.js";
  import { LogOut } from "lucide-svelte";
  import { t } from "svelte-i18n";
  import UserAvatar from "$lib/components/UserAvatar.svelte";
  import { identityStore } from "$lib/stores/identity.svelte";

  let { compact = false }: { compact?: boolean } = $props();

  let menuOpen = $state(false);

  const me = $derived(identityStore.me);
  const displayName = $derived.by(() => {
    if (!me) return "";
    if (me.anonymous) return `${me.name} · ${me.id.slice(-4)}`;
    return me.name;
  });
  const sourceLabel = $derived.by(() => {
    if (!me) return "";
    return $t(`me.authSource.${me.auth_source}`);
  });

  function signOut() {
    if (!me?.signout_url) return;
    menuOpen = false;
    window.location.href = me.signout_url;
  }

  onMount(() => {
    void identityStore.ensure();
  });
</script>

{#if me}
  <Popover.Root bind:open={menuOpen}>
    <Popover.Trigger>
      {#snippet child({ props })}
        <button
          {...props}
          type="button"
          class="flex items-center gap-2 rounded-md px-1.5 py-1 hover:bg-muted transition-colors"
          title={displayName}>
          <UserAvatar id={me.id} name={me.name} avatarUrl={me.avatar_url} size={26} />
          {#if !compact}
            <span class="hidden sm:inline max-w-32 truncate text-sm text-foreground">{displayName}</span>
          {/if}
        </button>
      {/snippet}
    </Popover.Trigger>
    <Popover.Content align="end" class="w-64 p-3">
      <div class="flex items-center gap-2">
        <UserAvatar id={me.id} name={me.name} avatarUrl={me.avatar_url} size={36} />
        <div class="min-w-0">
          <p class="truncate text-sm font-medium text-foreground">{displayName}</p>
          <p class="truncate text-xs text-muted-foreground">{me.email || sourceLabel}</p>
        </div>
      </div>
      {#if me.signout_url && !me.anonymous}
        <div class="mt-3 border-t border-border pt-2">
          <Button variant="ghost" size="sm" class="w-full justify-start" onclick={signOut}>
            <LogOut class="size-4 mr-1" />
            {$t("me.signOut")}
          </Button>
        </div>
      {/if}
    </Popover.Content>
  </Popover.Root>
{/if}
