// Bun's test runner types are not part of the SvelteKit tsconfig.
// @ts-nocheck
import { expect, test } from "bun:test";
import { DEFAULT_WIDTH, isWidthMode } from "./width";

test("screen width defaults to wide", () => {
  expect(DEFAULT_WIDTH).toBe("wide");
});

test("only narrow and wide are accepted", () => {
  expect(isWidthMode("narrow")).toBe(true);
  expect(isWidthMode("wide")).toBe(true);
  expect(isWidthMode("full")).toBe(false);
  expect(isWidthMode(null)).toBe(false);
});
