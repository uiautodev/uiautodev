<script lang="ts">
  import type { UINode } from "$lib/api/schemas";
  import { deviceStore } from "$lib/stores/device.svelte";
  import { CircleQuestionMark } from "lucide-svelte";

  interface FlatNode {
    id: string;
    tagName: string;
    rect: { x: number; y: number; width: number; height: number };
    props?: { [key: string]: any };
    depth: number;
    hasChildren: boolean;
    parentId: string | null;
    open: boolean;
  }

  function flattenTree(root: UINode | null, expandPathTo?: string): FlatNode[] {
    if (!root) return [];

    const ancestorIds = new Set<string>();
    if (expandPathTo) {
      const stack: { node: UINode; path: string[] }[] = [{ node: root, path: [] }];
      while (stack.length > 0) {
        const { node, path } = stack.pop()!;
        const currentPath = [...path, node.id];
        if (node.id === expandPathTo) {
          for (const id of currentPath) ancestorIds.add(id);
          break;
        }
        if (node.children) {
          for (let i = node.children.length - 1; i >= 0; i--) {
            stack.push({ node: node.children[i], path: currentPath });
          }
        }
      }
    }

    const result: FlatNode[] = [];
    const workStack: { node: UINode; depth: number; parentId: string | null }[] = [
      { node: root, depth: 0, parentId: null },
    ];

    while (workStack.length > 0) {
      const { node, depth, parentId } = workStack.pop()!;
      const hasChildren = !!node.children?.length;
      result.push({
        id: node.id,
        tagName: node.tagName,
        rect: node.rect,
        props: node.props,
        depth,
        hasChildren,
        parentId,
        open: ancestorIds.has(node.id),
      });

      if (hasChildren) {
        for (let i = node.children.length - 1; i >= 0; i--) {
          workStack.push({ node: node.children[i], depth: depth + 1, parentId: node.id });
        }
      }
    }

    return result;
  }

  function getVisibleNodes(nodes: FlatNode[]): FlatNode[] {
    const openParents = new Set<string>();
    const result: FlatNode[] = [];

    for (const node of nodes) {
      if (node.depth === 0 || openParents.has(node.parentId!)) {
        result.push(node);
        if (node.open) openParents.add(node.id);
      }
    }

    return result;
  }

  let showAllAttrs = $state(false);

  const IMPORTANT_ATTRS = new Set(["text", "resource-id", "content-desc", "value", "name"]);

  let allNodes = $state<FlatNode[]>([]);
  let visibleNodes = $derived.by(() => getVisibleNodes(allNodes));
  let matchedIds = $derived(new Set(deviceStore.matchedNodes.map((n) => n.id)));

  $effect(() => {
    const node = deviceStore.node;
    const selectedId = deviceStore.nodeSelected?.id;

    allNodes = flattenTree(node, selectedId);

    if (!selectedId) return;
    const timeoutId = setTimeout(() => {
      const row = document.querySelector(`[data-id="${selectedId}"]`);
      const content = row?.querySelector(".xml-node-content") ?? row;
      content?.scrollIntoView({ behavior: "smooth", block: "center", inline: "center" });
    }, 100);
    return () => clearTimeout(timeoutId);
  });

  function toggle(node: FlatNode) {
    node.open = !node.open;
  }

  function select(fnode: FlatNode) {
    deviceStore.nodeSelected = {
      id: fnode.id,
      tagName: fnode.tagName,
      rect: fnode.rect,
      props: fnode.props,
      children: [],
    } as UINode;
  }

  function formatAttrs(props: Record<string, any> | undefined): string[] {
    if (!props) return [];
    const entries = Object.entries(props).filter(([, v]) => v != null && v !== "");
    if (showAllAttrs) return entries.map(([k, v]) => `${k}="${v}"`);
    return entries.filter(([k]) => IMPORTANT_ATTRS.has(k)).map(([k, v]) => `${k}="${v}"`);
  }
</script>

<div class="mb-1 flex justify-start items-center gap-1">
  <div class="group relative">
    <span class="text-muted-foreground hover:text-foreground cursor-help text-xs select-none"
      ><CircleQuestionMark size={16} /></span>
    <div
      class="absolute left-0 top-full mt-1 hidden group-hover:block z-20 bg-popover border border-border rounded shadow-md px-3 py-2 text-xs text-popover-foreground whitespace-nowrap">
      <div>双击选中元素</div>
      <div>按住Shift水平滑动</div>
      <div>无子节点tag显示为绿色</div>
    </div>
  </div>
  <button
    onclick={() => (showAllAttrs = !showAllAttrs)}
    class="text-xs text-gray-400 hover:text-gray-600 px-1.5 py-0.5 rounded border border-gray-200 hover:border-gray-400 transition-colors">
    {showAllAttrs ? "核心属性" : "全部属性"}
  </button>
</div>

<div class="text-xs overflow-auto font-mono leading-none select-none" style="font-size: 12px; line-height: 1.5;">
  {#if visibleNodes.length > 0}
    {#each visibleNodes as fnode (fnode.id)}
      {@const attrs = formatAttrs(fnode.props)}
      {@const selected = deviceStore.nodeSelected?.id === fnode.id}
      {@const matched = matchedIds.has(fnode.id) && !selected}
      <div
        data-id={fnode.id}
        class="xml-node flex items-start whitespace-nowrap cursor-pointer border-l-2 border-b border-transparent py-0.5 pr-4 transition-colors"
        class:node-selected={selected}
        class:node-matched={matched}
        class:border-l-transparent={!selected && !matched}
        class:border-l-primary={selected}
        class:border-l-yellow-500={matched}
        style="padding-left: {fnode.depth * 20 + 4}px"
        ondblclick={() => select(fnode)}
        onkeydown={(e) => {
          if (e.key === "Enter" || e.key === " ") select(fnode);
        }}
        role="button"
        tabindex="0">
        <!-- expand/collapse arrow -->
        {#if fnode.hasChildren}
          <button
            class="inline-flex items-center justify-center w-4 -ml-4 text-muted-foreground hover:text-foreground shrink-0"
            onclick={(e) => {
              e.stopPropagation();
              toggle(fnode);
            }}>
            {fnode.open ? "▾" : "▸"}
          </button>
        {:else}
          <span class="inline-block w-4 -ml-4 shrink-0"></span>
        {/if}

        <span class="xml-node-content">
          <!-- <tagName -->
          <span class="text-muted-foreground">&lt;</span><span
            class={fnode.hasChildren ? "text-red-500" : "text-green-500"}>{fnode.tagName}</span>

          <!-- attributes -->
          {#each attrs as attr, i}
            <span class="text-foreground {i > 0 ? 'ml-2' : ''}">{attr.split("=")[0]}</span><span
              class="text-muted-foreground">=</span
            ><span class="text-[#ce9178]">&quot;{attr.split("=").slice(1).join("=").slice(1, -1)}&quot;</span>
          {/each}

          <!-- closing -->
          {#if fnode.hasChildren && !fnode.open}
            <span class="text-muted-foreground">&gt;</span><span class="text-muted-foreground">&lt;/</span><span
              class="text-red-500">{fnode.tagName}</span
            ><span class="text-muted-foreground">&gt;</span>
          {:else if !fnode.hasChildren}
            <span class="text-muted-foreground"> /&gt;</span>
          {:else}
            <span class="text-muted-foreground">&gt;</span>
          {/if}
        </span>
      </div>
    {/each}
  {:else}
    <div class="text-muted-foreground py-4 text-center">没有可用的UI层级数据。</div>
  {/if}
</div>

<style>
  .xml-node:hover {
    background-color: var(--color-muted);
  }
  .xml-node.node-selected {
    background-color: rgba(59, 130, 246, 0.18);
    box-shadow: inset 0 0 0 1px rgba(59, 130, 246, 0.45);
  }
  .xml-node.node-selected:hover {
    background-color: rgba(59, 130, 246, 0.28);
  }
  .xml-node.node-matched {
    background-color: rgba(245, 158, 11, 0.14);
  }
  .xml-node.node-matched:hover {
    background-color: rgba(245, 158, 11, 0.2);
  }
</style>
