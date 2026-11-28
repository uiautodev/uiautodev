// Bun's test runner types are not part of the SvelteKit tsconfig.
// @ts-nocheck
import { expect, test } from "bun:test";

// The desktop app is a single-user client: rendering an account menu there
// shows a meaningless anonymous identity and fires a /me request. Guard both
// so the web build keeps working unchanged.
test("UserMenu is hidden in the desktop app and skips the /me request", async () => {
  const source = await Bun.file(new URL("./UserMenu.svelte", import.meta.url)).text();

  expect(source).toContain("const desktop = isWailsEnv();");
  expect(source).toContain("{#if !desktop && me}");
  expect(source).toMatch(/onMount\(\(\) => \{\s*if \(desktop\) return;\s*void identityStore\.ensure\(\);/);
});
