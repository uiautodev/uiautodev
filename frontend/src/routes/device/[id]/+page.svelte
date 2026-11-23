<script lang="ts">
  import { page } from "$app/state";
  import { goto } from "$app/navigation";
  import { Button } from "$lib/components/ui/button/index.js";
  import { onMount } from "svelte";
  import * as NavigationMenu from "$lib/components/ui/navigation-menu/index.js";
  import * as Resizable from "$lib/components/ui/resizable/index.js";
  import * as Tooltip from "$lib/components/ui/tooltip/index.js";
  import { ArrowLeft, Columns2, Rows2 } from "lucide-svelte";
  import CardsContainer from "$lib/components/device/containers/CardsContainer.svelte";
  import { CopyButton } from "$lib/components/ui/copy-button";
  import { deviceStore } from "$lib/stores/device.svelte.js";
  import { layoutStore, toggleLayout } from "$lib/stores/layout.svelte";
  import { t } from "svelte-i18n";
  import LanguageSwitcher from "$lib/components/ui/language-switcher/language-switcher.svelte";
  import ThemeToggle from "$lib/components/ui/theme-toggle/theme-toggle.svelte";
  import UpdateButton from "$lib/components/ui/update-button/update-button.svelte";
  import DeviceRemark from "$lib/components/device/DeviceRemark.svelte";
  import DeviceIosSettings from "$lib/components/device/DeviceIosSettings.svelte";
  import type { DeviceMeta } from "$lib/api/schemas.js";
  import ScreenAndroid from "$lib/components/scrcpy/screen-android.svelte";
  import ScreenIos from "$lib/components/scrcpy/screen-ios.svelte";
  import ScreenHarmony from "$lib/components/scrcpy/screen-harmony.svelte";
  import ScreenDemo from "$lib/components/scrcpy/screen-demo.svelte";
  import * as Alert from "$lib/components/ui/alert/index.js";
  import Spinner from "$lib/components/ui/spinner/spinner.svelte";
  import { ScreenMode, setScreenMode } from "$lib/stores/screen-mode.svelte";
  import DeviceGuard from "$lib/components/device/DeviceGuard.svelte";
  import PresenceBadge from "$lib/components/device/PresenceBadge.svelte";
  import { identityStore } from "$lib/stores/identity.svelte";
  import UserMenu from "$lib/components/UserMenu.svelte";
  import type { DeviceAccessMode, Occupancy } from "$lib/api/schemas.js";

  const deviceId = page.params.id as string;
  const title = page.url.searchParams.get("title") ?? deviceId;
  const initialMode: DeviceAccessMode = page.url.searchParams.get("mode") === "view" ? "view" : "control";

  // 从 store 派生 device
  const device = $derived(deviceStore.deviceInfo);
  const isVertical = $derived(layoutStore.value === "vertical");
  const isHorizontal = $derived(!isVertical);
  let presence = $state<Occupancy | null>(null);
  const readOnly = $derived(presence?.state === "viewing");
  const mineId = $derived(identityStore.me?.id ?? "");

  function goBack() {
    goto("/");
  }

  function handleMetaSaved(id: string, meta: DeviceMeta) {
    if (deviceStore.deviceInfo?.id === id) {
      deviceStore.deviceInfo = { ...deviceStore.deviceInfo, meta };
    }
  }

  onMount(() => {
    deviceStore.ensureDeviceInfo();
    globalThis.document.title = title;
    return () => {
      deviceStore.clear();
      deviceStore.deviceInfo = null;
      setScreenMode(deviceId, ScreenMode.Control);
    };
  });
</script>

<div class="flex flex-col justify-center h-full">
  <header class="shrink-0 flex items-center">
    <NavigationMenu.Root viewport={false}>
      <NavigationMenu.List>
        <NavigationMenu.Item>
          <NavigationMenu.Link>
            <Button onclick={goBack} variant="secondary" size="icon" aria-label={$t("device.back")}>
              <ArrowLeft />
            </Button>
          </NavigationMenu.Link>
        </NavigationMenu.Item>
        <NavigationMenu.Item>
          <div class="flex min-w-0 items-center gap-2">
            {#if device?.platform === "android"}
              <span class="whitespace-nowrap"
                >{$t("device.platform.android")}:{device?.props.model}
                {$t("home.table.product")}:{device?.props.product}</span>
            {:else if device?.platform === "ios"}
              <span class="whitespace-nowrap">{$t("device.platform.ios")}</span>
            {:else if device?.platform === "harmony"}
              <span class="whitespace-nowrap"
                >{$t("device.platform.harmony")}:{device?.props.model}
                {$t("home.table.product")}:{device?.props.product}</span>
            {:else if device?.platform === "demo"}
              <span class="whitespace-nowrap"
                >{$t("device.platform.demo")}:{device?.props.model}
                {$t("home.table.product")}:{device?.props.product}</span>
            {/if}
            <span class="flex items-center whitespace-nowrap">
              {device?.props.serial}
              <CopyButton text={device?.props.serial ?? ""} />
            </span>
            <DeviceRemark {deviceId} meta={device?.meta} onsaved={handleMetaSaved} />
            {#if presence}
              <PresenceBadge occupancy={presence} {mineId} />
            {/if}
            {#if device?.platform === "ios"}
              <DeviceIosSettings {deviceId} meta={device?.meta} onsaved={handleMetaSaved} />
            {/if}
          </div>
        </NavigationMenu.Item>
      </NavigationMenu.List>
    </NavigationMenu.Root>
    <div class="ml-auto pr-2 flex items-center gap-1">
      <UserMenu compact />
      <UpdateButton />
      <LanguageSwitcher />
      <Tooltip.Root>
        <Tooltip.Trigger>
          {#snippet child({ props })}
            <Button
              {...props}
              onclick={toggleLayout}
              variant="ghost"
              size="icon"
              aria-label={$t("device.layout.toggle")}>
              {#if isHorizontal}
                <Rows2 />
              {:else}
                <Columns2 />
              {/if}
            </Button>
          {/snippet}
        </Tooltip.Trigger>
        <Tooltip.Content side="bottom">
          <p>
            {isHorizontal ? $t("device.layout.toVertical") : $t("device.layout.toHorizontal")}
          </p>
        </Tooltip.Content>
      </Tooltip.Root>
      <ThemeToggle />
    </div>
  </header>
  <DeviceGuard {deviceId} {initialMode} onstate={(next) => (presence = next)}>
    <main class="flex-1 flex overflow-hidden border w-full h-full">
      {#if deviceStore.loading}
        <div class="flex flex-col items-center justify-center w-full gap-4">
          <Spinner class="size-8" />
          <span class="text-muted-foreground text-sm">连接中...</span>
        </div>
      {:else if deviceStore.error}
        <div class="p-4 w-full">
          <Alert.Root variant="destructive">
            <Alert.Title>获取设备信息失败</Alert.Title>
            <Alert.Description>{deviceStore.error}</Alert.Description>
          </Alert.Root>
        </div>
      {:else}
        <Resizable.PaneGroup direction={layoutStore.value}>
          <Resizable.Pane defaultSize={isHorizontal ? 30 : 40}>
            {#if device?.platform === "android"}
              <ScreenAndroid
                {deviceId}
                showRightBar={isHorizontal}
                navInHeader={isVertical}
                control={!readOnly}
                class="h-full flex-1" />
            {/if}
            {#if device?.platform === "ios"}
              <ScreenIos
                {deviceId}
                showRightBar={isHorizontal}
                navInHeader={isVertical}
                control={!readOnly}
                class="h-full flex-1" />
            {/if}
            {#if device?.platform === "harmony"}
              <ScreenHarmony
                {deviceId}
                showRightBar={isHorizontal}
                navInHeader={isVertical}
                control={!readOnly}
                class="h-full flex-1" />
            {/if}
            {#if device?.platform === "demo"}
              <ScreenDemo
                {deviceId}
                showRightBar={isHorizontal}
                navInHeader={isVertical}
                control={!readOnly}
                class="h-full flex-1" />
            {/if}
          </Resizable.Pane>
          <Resizable.Handle withHandle />
          <Resizable.Pane>
            <div class="relative flex-1 px-2 pt-2 h-full">
              {#if device?.platform}
                <CardsContainer {deviceId} platform={device.platform} remote={device.remote ?? false} class="h-full" />
              {/if}
              {#if readOnly}
                <div class="absolute inset-0 flex items-center justify-center bg-background/60">
                  <span
                    class="rounded-md bg-amber-100 px-3 py-1.5 text-sm text-amber-900 dark:bg-amber-950 dark:text-amber-200">
                    {$t("presence.readOnly")}
                  </span>
                </div>
              {/if}
            </div>
          </Resizable.Pane>
        </Resizable.PaneGroup>
      {/if}
    </main>
  </DeviceGuard>
</div>
