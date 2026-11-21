<script lang="ts">
  import { deviceStore } from "$lib/stores/device.svelte";
  import type { UINode } from "$lib/api/schemas";

  type Props = {
    node: UINode | null;
    image?: Blob | null;
    width: number;
    height: number;
    [key: string]: any;
  };

  let { node = null, image = null, width, height, ...restProps }: Props = $props();
  let nodeSelected = $derived(deviceStore.nodeSelected);
  let matchedNodes = $derived(deviceStore.matchedNodes);

  let imageBitmap: ImageBitmap | null = $state(null);

  $effect(() => {
    if (image) {
      createImageBitmap(image)
        .then((bitmap) => {
          imageBitmap = bitmap;
        })
        .catch((err) => {
          console.error("Failed to create ImageBitmap:", err);
          imageBitmap = null;
        });
    } else {
      imageBitmap = null;
    }
  });

  // other rest props can be forwarded to the canvas
  let canvasRef: HTMLCanvasElement;

  // Extended node type with cached perimeter and depth
  type CachedUINode = UINode & { perimeter: number; depth: number };

  // Cache for nodes with perimeter and depth
  let cachedTree: CachedUINode | null = null;

  // Calculate and cache perimeter and depth for all nodes
  function calculateNodeMetrics(n: UINode, depth: number): CachedUINode {
    const r = n.rect;
    const perimeter = r ? 2 * (r.width + r.height) : 0;

    const cached = {
      ...n,
      perimeter,
      depth,
      children: n.children?.map((c) => calculateNodeMetrics(c, depth + 1)) ?? [],
    } as CachedUINode;

    return cached;
  }

  // Update cache when node changes
  $effect(() => {
    if (node) {
      cachedTree = calculateNodeMetrics(node, 0);
    } else {
      cachedTree = null;
    }
  });

  // Re-draw when anything changes
  $effect(() => {
    drawOverlay();
  });

  function findNodeAtPosition(clickX: number, clickY: number, targetNode: CachedUINode | null): UINode | null {
    if (!targetNode) return null;

    const canvasRect = canvasRef.getBoundingClientRect();
    const displayWidth = canvasRect.width;
    const displayHeight = canvasRect.height;

    const nodeWidth = targetNode.rect.width;
    const nodeHeight = targetNode.rect.height;

    // Scale from display coordinates to node coordinates
    const scaleX = nodeWidth / displayWidth;
    const scaleY = nodeHeight / displayHeight;

    // Convert click coordinates to node coordinate space
    const nodeX = clickX * scaleX;
    const nodeY = clickY * scaleY;

    // Collect all matching nodes
    const matches: CachedUINode[] = [];

    function collect(n: CachedUINode) {
      const r = n.rect;
      if (!r) return;

      if (nodeX >= r.x && nodeX <= r.x + r.width && nodeY >= r.y && nodeY <= r.y + r.height) {
        matches.push(n);
      }

      if (n.children?.length) {
        for (const child of n.children) {
          collect(child as CachedUINode);
        }
      }
    }

    collect(targetNode);

    if (matches.length === 0) return null;

    // Sort by perimeter (ascending), then by depth (descending)
    matches.sort((a, b) => {
      if (a.perimeter !== b.perimeter) {
        return a.perimeter - b.perimeter; // Smaller perimeter first
      }
      return b.depth - a.depth; // Larger depth first
    });

    return matches[0];
  }

  function handleClick(e: MouseEvent) {
    if (!cachedTree) return;

    const rect = canvasRef.getBoundingClientRect();
    const x = e.clientX - rect.left;
    const y = e.clientY - rect.top;
    const clickedNode = findNodeAtPosition(x, y, cachedTree);
    if (clickedNode) {
      deviceStore.nodeSelected = clickedNode;
    }
  }

  function drawOverlay() {
    const ctx = canvasRef.getContext("2d");
    if (!ctx) return;

    const dpr = window.devicePixelRatio || 1;
    // set internal pixel size for crisp rendering
    canvasRef.width = Math.round(width * dpr);
    canvasRef.height = Math.round(height * dpr);

    // reset transform and scale for DPR
    ctx.setTransform(1, 0, 0, 1, 0, 0);
    ctx.scale(dpr, dpr);

    ctx.clearRect(0, 0, width, height);

    // draw image first (if available)
    if (image && imageBitmap) {
      ctx.drawImage(imageBitmap, 0, 0, width, height);
    }

    if (!node) return;

    ctx.lineWidth = 1;
    ctx.font = "12px sans-serif";
    ctx.textBaseline = "top";

    const nodeWidth = node.rect.width;
    const nodeHeight = node.rect.height;
    const scaleX = width / nodeWidth;
    const scaleY = height / nodeHeight;

    function drawNode(ctx: CanvasRenderingContext2D, n: UINode) {
      const r = n.rect;
      if (!r) return;

      // scale coordinates
      const x = r.x * scaleX;
      const y = r.y * scaleY;
      const w = r.width * scaleX;
      const h = r.height * scaleY;

      // draw fill + stroke (blue for non-selected)
      ctx.fillStyle = "rgba(0,150,255,0.06)";
      ctx.strokeStyle = "rgba(0,150,255,0.9)";
      ctx.lineWidth = 2;
      ctx.strokeRect(x, y, w, h);

      if (n.children && Array.isArray(n.children)) {
        for (const c of n.children) drawNode(ctx, c);
      }
    }

    // Draw all non-selected nodes
    drawNode(ctx, node);

    // Draw matched nodes (yellow highlight)
    if (matchedNodes && matchedNodes.length > 0) {
      for (const matchedNode of matchedNodes) {
        // Skip if this is the selected node (will be drawn separately)
        if (nodeSelected && matchedNode.id === nodeSelected.id) continue;

        const r = matchedNode.rect;
        if (r) {
          const x = r.x * scaleX;
          const y = r.y * scaleY;
          const w = r.width * scaleX;
          const h = r.height * scaleY;

          // Draw matched node with yellow color
          ctx.fillStyle = "rgba(234,179,8,0.15)";
          ctx.strokeStyle = "rgba(234,179,8,0.9)";
          ctx.lineWidth = 3;
          ctx.strokeRect(x, y, w, h);
        }
      }
    }

    // Draw selected node last (on top)
    if (nodeSelected) {
      const r = nodeSelected.rect;
      if (r) {
        const x = r.x * scaleX;
        const y = r.y * scaleY;
        const w = r.width * scaleX;
        const h = r.height * scaleY;

        // Draw selected node with red color
        ctx.fillStyle = "rgba(255,0,0,0.1)";
        ctx.strokeStyle = "rgba(255,0,0,1)";
        ctx.lineWidth = 4;
        ctx.strokeRect(x, y, w, h);
      }
    }
  }
</script>

<canvas bind:this={canvasRef} {width} {height} onclick={handleClick} {...restProps} aria-hidden="true"></canvas>
