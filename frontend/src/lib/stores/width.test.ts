// Bun's test runner types are not part of the SvelteKit tsconfig.
// @ts-nocheck
import { expect, test } from "bun:test";
import { DEFAULT_WIDTH, containerClass, isWidthMode } from "./width";

test("screen width defaults to wide", () => {
  expect(DEFAULT_WIDTH).toBe("wide");
});

test("only narrow and wide are accepted", () => {
  expect(isWidthMode("narrow")).toBe(true);
  expect(isWidthMode("wide")).toBe(true);
  expect(isWidthMode("full")).toBe(false);
  expect(isWidthMode(null)).toBe(false);
});

// The header and the page body must use the exact same container so their edges
// line up on the device list; pages without the toggle fall back to narrow.
test("wide and narrow share one gutter and only differ in width cap", () => {
  expect(containerClass("wide")).toBe("w-full px-4");
  expect(containerClass("narrow")).toBe("mx-auto w-full max-w-4xl px-4");
  expect(containerClass(undefined)).toBe(containerClass("narrow"));
  expect(containerClass("wide").split(" ")).toContain("px-4");
  expect(containerClass("narrow").split(" ")).toContain("px-4");
});
