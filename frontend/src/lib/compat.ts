import { getServerConfig as getServerConfigFromAPI, setAPIToken } from "./api/api";
import type { ServerConfig } from "./api/schemas";
import { isWailsEnv } from "./wails-env";

export { isWailsEnv };

export async function openExternalURL(url: string) {
  if (isWailsEnv()) {
    const { Browser } = await import("@wailsio/runtime");
    await Browser.OpenURL(url);
  } else {
    window.open(url, "_blank");
  }
}

async function wailsApp() {
  const { App } = await import("../../bindings/github.com/uiautodev/uiautodev");
  return App;
}

export async function getServerConfig(): Promise<ServerConfig> {
  let config: ServerConfig;
  if (isWailsEnv()) {
    const app = await wailsApp();
    config = (await app.GetServerConfig()) as ServerConfig;
  } else {
    config = await getServerConfigFromAPI();
  }
  if (isWailsEnv()) {
    try {
      const app = await wailsApp();
      setAPIToken(await app.GetAPIToken());
    } catch (error) {
      console.error("Failed to load desktop API token:", error);
    }
  }
  return config;
}

// Map a file extension to the accepted file extensions for a save dialog.
function fileFilterFor(filename: string): string[] {
  const ext = filename.split(".").pop()?.toLowerCase();
  switch (ext) {
    case "mp4":
      return ["mp4"];
    case "zip":
      return ["zip"];
    default:
      return [];
  }
}

function mimeTypeFor(filename: string): string {
  const ext = filename.split(".").pop()?.toLowerCase();
  switch (ext) {
    case "mp4":
      return "video/mp4";
    case "zip":
      return "application/zip";
    default:
      return "application/octet-stream";
  }
}

// Browser fallback: anchor + object URL download (no location choice possible).
function downloadBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}

// Save via the Wails desktop bridge (native save dialog).
async function saveFileWails(buffer: ArrayBuffer, defaultFilename: string): Promise<boolean> {
  const { App } = await import("../../bindings/github.com/uiautodev/uiautodev");
  try {
    const exts = fileFilterFor(defaultFilename);
    const filePath = await App.SaveFileDialog(defaultFilename, exts);
    if (!filePath) {
      // User cancelled
      return false;
    }
    // Go []byte bindings are transported as base64 strings.
    await App.SaveFileWithData(filePath, arrayBufferToBase64(buffer));
    return true;
  } catch (error) {
    console.error("Failed to save file:", error);
    throw error;
  }
}

function arrayBufferToBase64(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer);
  const chunkSize = 0x8000;
  let binary = "";
  for (let i = 0; i < bytes.length; i += chunkSize) {
    binary += String.fromCharCode(...bytes.subarray(i, i + chunkSize));
  }
  return btoa(binary);
}

// Browser fallback: save via File System Access API, or a plain download otherwise.
async function saveFileBrowser(buffer: ArrayBuffer, defaultFilename: string): Promise<boolean> {
  const mimeType = mimeTypeFor(defaultFilename);
  const blob = new Blob([buffer], { type: mimeType });
  // Prefer the File System Access API so the user can choose the save location.
  // Only Chromium-supporting browsers expose it; fall back to a plain download otherwise.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const pickFile = (window as any).showSaveFilePicker as (options: any) => Promise<any>;
  if (typeof pickFile === "function") {
    try {
      const handle = await pickFile({
        suggestedName: defaultFilename,
        types: [
          {
            description: fileFilterFor(defaultFilename).join(", ") || "Files",
            accept: { [mimeType]: fileFilterFor(defaultFilename).map((e) => "." + e) },
          },
        ],
      });
      const writable = await handle.createWritable();
      await writable.write(blob);
      await writable.close();
      return true;
    } catch (error) {
      // User cancelled the dialog: not an error.
      if (error instanceof DOMException && error.name === "AbortError") {
        return false;
      }
      console.error("Failed to save file with picker, falling back to download:", error);
      downloadBlob(blob, defaultFilename);
      return true;
    }
  }
  downloadBlob(blob, defaultFilename);
  return true;
}

export async function saveFile(buffer: ArrayBuffer, defaultFilename: string): Promise<boolean> {
  if (isWailsEnv()) {
    return saveFileWails(buffer, defaultFilename);
  }
  return saveFileBrowser(buffer, defaultFilename);
}
