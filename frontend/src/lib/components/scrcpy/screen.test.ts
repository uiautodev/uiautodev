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
  const wrapperTag =
    source.match(/<div\s+class="relative flex max-h-full max-w-full min-h-0 min-w-0"[\s\S]*?>/)?.[0] ?? "";
  expect(wrapperTag).toContain("style:aspect-ratio");
  expect(wrapperTag).toContain("isPortrait");
  expect(wrapperTag).toContain("screenWidth");
  expect(wrapperTag).toContain("screenHeight");
});
