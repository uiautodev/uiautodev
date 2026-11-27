// Bun's test runner types are not part of the SvelteKit tsconfig.
// @ts-nocheck
import { expect, test } from "bun:test";

// On WebKit (the macOS desktop webview) the canvas wrapper is a flex item that
// fills the free width instead of shrinking to the height-constrained portrait
// video, so a wide empty strip appears between the screen and the right-hand
// toolbar. Pinning the wrapper's aspect ratio to the video makes it hug the
// canvas in every engine; keep that binding on the wrapper element.
test("canvas wrapper keeps the portrait video aspect ratio", async () => {
  const source = await Bun.file(new URL("./screen.svelte", import.meta.url)).text();
  const wrapperTag = source.match(/<div\s+class="relative flex max-h-full[^"]*"[\s\S]*?>/)?.[0] ?? "";
  expect(wrapperTag).toContain("style:aspect-ratio");
  expect(wrapperTag).toContain("isPortrait");
  expect(wrapperTag).toContain("screenWidth");
  expect(wrapperTag).toContain("screenHeight");
  // Without items-center the flex item stretches to the row height, so a
  // landscape stream is drawn letterboxed inside a portrait-shaped box.
  expect(wrapperTag).toContain("items-center");
});

// WebKit only constrains the canvas when the whole height chain is definite.
// Keep the column between the screen header and the canvas full height and the
// row that holds the canvas flexible, otherwise the desktop app leaves a large
// empty band above and below a portrait screen while Chromium looks fine.
test("screen body keeps a definite height chain", async () => {
  const source = await Bun.file(new URL("./screen.svelte", import.meta.url)).text();
  const mainTag = source.match(/<main class="[^"]*"[\s\S]*?>/)?.[0] ?? "";
  expect(mainTag).toContain("flex-1");
  expect(mainTag).toContain("min-h-0");
  expect(mainTag).toContain("flex-col");

  const bodyTag = source.match(/<div class="flex h-full min-h-0 w-full flex-col"[\s\S]*?>/)?.[0] ?? "";
  expect(bodyTag).not.toBe("");

  const canvasRowTag =
    source.match(/<div class="relative flex min-h-0 w-full flex-1 flex-row[^"]*"[\s\S]*?>/)?.[0] ?? "";
  expect(canvasRowTag).toContain("flex-1");
  expect(canvasRowTag).toContain("min-h-0");
});

// A landscape stream gets a horizontal toolbar stacked above the canvas instead
// of a vertical one stealing width, and the two share the same left edge.
test("landscape stacks a horizontal toolbar above the canvas", async () => {
  const source = await Bun.file(new URL("./screen.svelte", import.meta.url)).text();
  expect(source).toContain("{#if isPortrait}");
  expect(source).toContain('{@render toolToolbar("vertical")}');
  expect(source).toContain('{@render toolToolbar("horizontal")}');
  expect(source).toMatch(/class="flex h-full max-h-full min-h-0 w-full flex-col items-start justify-center"/);
});
