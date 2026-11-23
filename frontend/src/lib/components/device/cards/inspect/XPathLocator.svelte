<script lang="ts">
  import { deviceStore } from "$lib/stores/device.svelte";
  import * as InputGroup from "$lib/components/ui/input-group/index.js";
  import * as Popover from "$lib/components/ui/popover/index.js";
  import * as Command from "$lib/components/ui/command/index.js";
  import { ChevronDown, Search } from "lucide-svelte";
  import { cn } from "$lib/utils.js";
  import { Button, buttonVariants } from "$lib/components/ui/button";
  import CopyButton from "$lib/components/ui/copy-button/copy-button.svelte";
  import { isValidXPathSyntax } from "$lib/utils/xpath";
  import { generateXPathCandidates, type XPathCandidate } from "$lib/utils/xpathgen";
  import type { UINode } from "$lib/api/schemas";
  import { t } from "svelte-i18n";

  let xpathInput = $state("");
  let matchedCount = $derived(deviceStore.matchedNodes.length);
  let isQuerying = $state(false);
  let isXPathValid = $state(false);
  let hasQueried = $state(false);

  let xpathCandidates = $derived(
    generateXPathCandidates(deviceStore.node, deviceStore.nodeSelected).sort((a, b) => a.matchCount - b.matchCount),
  );
  let candidatesOpen = $state(false);
  let lastAutoId = $state<string | null>(null);
  let circleRow: HTMLDivElement | undefined = $state();

  function getNodeDisplayText(node: UINode): string {
    const tag = node.tagName || "unknown";
    const props = node.props || {};
    const label = props["text"] || props["resource-id"] || props["content-desc"] || "";
    return label ? `<${tag}> ${label}` : `<${tag}>`;
  }

  function selectMatchedNode(node: UINode) {
    deviceStore.nodeSelected = node;
  }

  // Validate XPath on input change
  $effect(() => {
    if (!xpathInput.trim()) {
      isXPathValid = false;
      hasQueried = false;
      deviceStore.clearXPathResults();
    } else if (isValidXPathSyntax(xpathInput)) {
      isXPathValid = true;
    } else {
      isXPathValid = false;
    }
  });

  async function runXPathQuery(xpath: string) {
    if (!xpath.trim()) return;
    isQuerying = true;
    try {
      await deviceStore.queryXPath(xpath);
      isXPathValid = true;
      hasQueried = true;
    } catch (e) {
      isXPathValid = false;
      hasQueried = false;
      deviceStore.clearXPathResults();
    } finally {
      isQuerying = false;
    }
  }

  function candidateDisplay(cand: XPathCandidate): string {
    return cand.isFirst ? cand.xpath : `(${cand.xpath})[${cand.rank}]`;
  }

  function handleXPathQuery() {
    void runXPathQuery(xpathInput);
  }

  // 选中元素后，自动取第一个候选填入输入框并查询验证
  $effect(() => {
    const node = deviceStore.nodeSelected;
    const candidates = xpathCandidates;
    if (!node) {
      lastAutoId = null;
      return;
    }
    if (lastAutoId === node.id) return;
    lastAutoId = node.id;
    // 点击匹配节点圆圈时，仅切换选中/高亮，不再触发后台 xpath 查询
    if (deviceStore.matchedNodes.some((m) => m.id === node.id)) return;
    if (candidates.length > 0) {
      const cand = candidates[0];
      xpathInput = candidateDisplay(cand);
      void runXPathQuery(cand.xpath);
    } else {
      xpathInput = "";
      hasQueried = false;
      deviceStore.clearXPathResults();
    }
  });

  function handleCandidateChange(cand: XPathCandidate) {
    xpathInput = candidateDisplay(cand);
    void runXPathQuery(cand.xpath);
    candidatesOpen = false;
  }

  // 选中/生成 xpath 时，若高亮数字不在可见范围，横向滚动过去
  $effect(() => {
    const selectedId = deviceStore.nodeSelected?.id;
    if (!selectedId || deviceStore.matchedNodes.length === 0 || !circleRow) return;
    const el = circleRow.querySelector(`[data-node-id="${CSS.escape(selectedId)}"]`);
    el?.scrollIntoView({ behavior: "smooth", inline: "nearest", block: "nearest" });
  });
</script>

<!-- XPath Query Section -->
<div class="w-full mb-2">
  <p class="mb-1 text-xs text-muted-foreground">{$t("device.inspect.xpathHint")}</p>
  <InputGroup.Root>
    <InputGroup.Input
      placeholder="XPath: //*[@text='Settings']"
      bind:value={xpathInput}
      onkeydown={(e) => e.key === "Enter" && handleXPathQuery()} />
    <InputGroup.Addon align="inline-end">
      <InputGroup.Button
        size="icon-xs"
        onclick={handleXPathQuery}
        disabled={isQuerying || !xpathInput.trim() || !isXPathValid}
        aria-label="查询">
        <Search size={16} />
      </InputGroup.Button>
      <CopyButton text={xpathInput} variant="ghost" size="icon" class="size-6" />
      <Popover.Root bind:open={candidatesOpen}>
        <Popover.Trigger
          aria-label="定位元素"
          title="定位元素"
          disabled={xpathCandidates.length === 0}
          class={cn(buttonVariants({ variant: "outline", size: "icon" }), "size-6")}>
          <ChevronDown class="size-4" />
        </Popover.Trigger>
        <Popover.Content side="bottom" align="end" sideOffset={4} class="w-[36rem] max-w-[90vw] p-0">
          <Command.Root>
            <Command.List>
              <Command.Empty>无可用的定位候选</Command.Empty>
              <Command.Group>
                {#each xpathCandidates as cand (cand.xpath)}
                  <Command.Item value={cand.xpath} onSelect={() => handleCandidateChange(cand)}>
                    <span class="flex min-w-0 flex-1 items-center gap-2">
                      <span class="min-w-0 flex-1 text-muted-foreground font-mono break-all">{cand.xpath}</span>
                      {#if cand.matchCount > 1}
                        <span
                          class="shrink-0 rounded px-1.5 text-xs font-medium tabular-nums
                              {cand.isFirst
                            ? 'bg-green-500/15 text-green-700 dark:text-green-400'
                            : 'bg-amber-500/15 text-amber-700 dark:text-amber-400'}">
                          {cand.matchCount}
                        </span>
                      {/if}
                    </span>
                  </Command.Item>
                {/each}
              </Command.Group>
            </Command.List>
          </Command.Root>
        </Popover.Content>
      </Popover.Root>
    </InputGroup.Addon>
  </InputGroup.Root>
</div>

<!-- Matched Nodes List -->
{#if hasQueried}
  <div class="mb-2">
    {#if matchedCount > 0}
      <div class="flex gap-1.5 overflow-x-auto pb-1" bind:this={circleRow}>
        {#each deviceStore.matchedNodes as node, index (node.id)}
          {@const isSelected = node.id === deviceStore.nodeSelected?.id}
          <Button
            onclick={() => selectMatchedNode(node)}
            variant={isSelected ? "secondary" : "ghost"}
            size="sm"
            data-node-id={node.id}
            class="size-7 shrink-0 rounded-full p-0 text-xs {isSelected
              ? 'bg-green-500/15 text-green-700 dark:text-green-400'
              : ''}"
            aria-label={getNodeDisplayText(node)}
            title={getNodeDisplayText(node)}>
            {index + 1}
          </Button>
        {/each}
      </div>
    {:else}
      <span class="text-xs text-muted-foreground">未匹配到节点</span>
    {/if}
  </div>
{/if}
