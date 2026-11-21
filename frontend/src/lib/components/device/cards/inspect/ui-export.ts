import JSZip from "jszip";
import type { APIDevice, UINode } from "$lib/api/schemas";
import { treeToXML } from "$lib/api/api";
import { saveFile } from "$lib/compat";

const UI_EXPORT_VERSION = 1;

interface ExportMetadata {
  version: number;
  exported_at: string;
  platform?: string;
  device?: { serial?: string; model?: string; product?: string };
  screen?: { width: number; height: number };
}

export function defaultExportFilename(opts: { platform?: string; serial?: string }): string {
  const platform = opts.platform || "unknown";
  const serial = (opts.serial || "device").slice(0, 4);
  return `${platform}-${serial}-dump.zip`;
}

export function metadataOf(node: UINode, device?: APIDevice | null): ExportMetadata {
  return {
    version: UI_EXPORT_VERSION,
    exported_at: new Date().toISOString(),
    platform: device?.platform,
    device: {
      serial: device?.props.serial,
      model: device?.props.model,
      product: device?.props.product,
    },
    screen: node.rect ? { width: node.rect.width, height: node.rect.height } : undefined,
  };
}

export async function exportUItoZip(opts: {
  node: UINode | null;
  image: Blob | null;
  device?: APIDevice | null;
}): Promise<boolean> {
  const { node, image, device } = opts;
  if (!node) {
    throw new Error("没有可导出的UI布局，请先刷新UI");
  }

  const zip = new JSZip();
  zip.file("ui_tree.json", JSON.stringify(node, null, 2));
  zip.file("ui_tree.xml", await treeToXML(node));
  if (image) {
    zip.file("screenshot.png", image);
  }
  zip.file("metadata.json", JSON.stringify(metadataOf(node, device), null, 2));

  const buffer = await zip.generateAsync({ type: "arraybuffer" });
  return saveFile(buffer, defaultExportFilename({ platform: device?.platform, serial: device?.props.serial }));
}

export async function importUIFromZip(file: File): Promise<{ node: UINode; image: Blob | null }> {
  const zip = await JSZip.loadAsync(file);
  const treeEntry = zip.file("ui_tree.json");
  if (!treeEntry) {
    throw new Error("无效的UI导出包：缺少 ui_tree.json");
  }
  const node = JSON.parse(await treeEntry.async("string")) as UINode;
  if (!node || typeof node !== "object" || !Array.isArray(node.children)) {
    throw new Error("无效的UI布局数据");
  }

  const imageEntry = zip.file("screenshot.png");
  const image = imageEntry ? await imageEntry.async("blob") : null;
  return { node, image };
}