export const OFFICIAL_PLUGIN_PREFIX = "uiautodev-";

export function isOfficialPlugin(id: string): boolean {
  return id.startsWith(OFFICIAL_PLUGIN_PREFIX);
}
