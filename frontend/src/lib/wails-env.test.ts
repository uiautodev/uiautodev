// Bun's test runner types are not part of the SvelteKit tsconfig.
// @ts-nocheck
import { afterEach, expect, test } from "bun:test";
import { isWailsEnv } from "./wails-env";

const globals = globalThis as { window?: unknown };

afterEach(() => {
  delete globals.window;
});

// The Wails runtime injects window._wails.environment before the app scripts
// run; a plain browser must never be treated as the desktop client.
test("desktop detection follows the injected Wails environment", () => {
  expect(isWailsEnv()).toBe(false);

  globals.window = {};
  expect(isWailsEnv()).toBe(false);

  globals.window = { _wails: {} };
  expect(isWailsEnv()).toBe(false);

  globals.window = { _wails: { environment: { OS: "darwin" } } };
  expect(isWailsEnv()).toBe(true);
});
