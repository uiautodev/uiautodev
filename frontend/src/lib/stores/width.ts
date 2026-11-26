export const WIDTH_MODES = ["narrow", "wide"] as const;

export type WidthMode = (typeof WIDTH_MODES)[number];

export const DEFAULT_WIDTH: WidthMode = "wide";

export function isWidthMode(value: unknown): value is WidthMode {
  return typeof value === "string" && (WIDTH_MODES as readonly string[]).includes(value);
}
