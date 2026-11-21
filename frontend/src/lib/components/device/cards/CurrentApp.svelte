<script lang="ts">
  import { api } from "$lib/api/api";
  import * as Card from "$lib/components/ui/card/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import Input from "../../ui/input/input.svelte";

  const { deviceId, ...restProps } = $props();

  let loading = $derived(false);
  let pkg = $derived("");
  let activity = $derived("");
  let raw = $derived("");

  function parseOutput(output: string) {
    // Try common dumpsys outputs
    const patterns = [
      /mCurrentFocus=.*?([a-zA-Z0-9_\.]+)\/([a-zA-Z0-9_\.\$]+)/,
      /mFocusedApp=.*?([a-zA-Z0-9_\.]+)\/([a-zA-Z0-9_\.\$]+)/,
      /mResumedActivity.*?([a-zA-Z0-9_\.]+)\/([a-zA-Z0-9_\.\$]+)/,
      /ResumedActivity:.*?([a-zA-Z0-9_\.]+)\/([a-zA-Z0-9_\.\$]+)/,
    ];
    for (const re of patterns) {
      const m = re.exec(output);
      if (m && m.length >= 3) {
        return { package: m[1], activity: m[2] };
      }
    }
    // fallback: find first package/activity like pattern
    const fallback = /([a-zA-Z0-9_\.]+)\/([a-zA-Z0-9_\.\$]+)/.exec(output);
    if (fallback && fallback.length >= 3) {
      return { package: fallback[1], activity: fallback[2] };
    }
    return { package: "", activity: "" };
  }

  export async function refresh() {
    if (!deviceId) return;
    loading = true;
    pkg = "";
    activity = "";
    raw = "";
    try {
      type ShellResponse = {
        output: string;
      };
      const res = await api.post(
        `/api/v1/devices/${deviceId}/shell`,
        {
          command:
            "dumpsys window windows | grep -E 'mCurrentFocus|mFocusedApp' || dumpsys activity activities | grep -E 'mResumedActivity|ResumedActivity'",
          timeout: 3000,
        },
      );
      const response: ShellResponse = res.data;
      console.log("shell response:", response);
      raw = response.output;
      const parsed = parseOutput(raw);
      pkg = parsed.package;
      activity = parsed.activity;
    } catch (err: any) {
      raw = err?.message || String(err);
    } finally {
      loading = false;
    }
  }

  async function stopApp() {
    if (!deviceId || !pkg) return;
    loading = true;
    try {
      await api.post(`/api/v1/devices/${deviceId}/shell`, {
        command: `am force-stop ${pkg}`,
        timeout: 5000,
      });
    } catch (err: any) {
      console.error("Failed to stop app:", err);
    } finally {
      loading = false;
    }
  }

  async function startApp() {
    if (!deviceId || !pkg || !activity) return;
    loading = true;
    try {
      await api.post(`/api/v1/devices/${deviceId}/shell`, {
        command: `am start -n ${pkg}/${activity}`,
        timeout: 5000,
      });
    } catch (err: any) {
      console.error("Failed to start app:", err);
    } finally {
      loading = false;
    }
  }
</script>

<Card.Root {...restProps}>
  <Card.Header>
    <Card.Title>获取当前App</Card.Title>
  </Card.Header>
  <Card.Content class="flex flex-col gap-2">
    <div class="space-y-2">
      <Input bind:value={pkg} placeholder="Package" />
      <Input bind:value={activity} placeholder="Activity" />
    </div>
    <div class="flex gap-4">
      <Button onclick={refresh} disabled={loading}>
        {#if loading}刷新中...{:else}刷新{/if}
      </Button>
      <Button onclick={stopApp} disabled={!pkg || loading} variant="destructive">
        停止
      </Button>
      <Button onclick={startApp} disabled={!pkg || !activity || loading}>
        启动
      </Button>
    </div>
  </Card.Content>
</Card.Root>
