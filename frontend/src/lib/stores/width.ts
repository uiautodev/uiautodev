export const WIDTH_MODES = ["wide", "narrow"] as const;

export type WidthMode = (typeof WIDTH_MODES)[number];

export const DEFAULT_WIDTH: WidthMode = "wide";

export function isWidthMode(value: unknown): value is WidthMode {
  return typeof value === "string" && (WIDTH_MODES as readonly string[]).includes(value);
}

// containerClass is shared by the app header and the page body so their left and
// right edges always line up. Pages without the wide/narrow toggle pass no mode
// and keep the narrow (centred) layout.
export function containerClass(mode: WidthMode | undefined): string {
  return mode === "wide" ? "w-full px-4" : "mx-auto w-full max-w-4xl px-4";
}
