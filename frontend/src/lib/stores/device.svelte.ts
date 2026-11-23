import { page } from "$app/state";
import type { UINode, APIDevice } from "$lib/api/schemas";
import { getDeviceInfo, getUiTree, queryXPath, getScreenshot, type DumpBackend } from "$lib/api/api";
import { useLocalStorage } from "$lib/utils/localstorage.svelte";

// Android UI hierarchy dump method, persisted across sessions. Defaults to yadb.
export const dumpBackendStore = useLocalStorage<DumpBackend>("device-dump-backend", "yadb");

export const deviceStore = $state({
  node: null as UINode | null,
  nodeSelected: null as UINode | null,
  dumpImage: null as Blob | null,
  // 设备信息缓存
  deviceInfo: null as APIDevice | null,
  // XPath 查询匹配的节点
  matchedNodes: [] as UINode[],
  // UI 正在刷新的状态
  dumping: false,
  // 截图冻结状态
  freezing: false,
  // 加载和错误状态
  loading: false,
  error: null as string | null,

  async ensureDeviceInfo(): Promise<void> {
    const deviceId = page.params.id as string;

    // 如果已有缓存且 ID 匹配，直接返回
    if (this.deviceInfo?.id === deviceId) {
      return;
    }
    this.loading = true;
    this.error = null;
    try {
      this.deviceInfo = await getDeviceInfo(deviceId);
    } catch (e) {
      this.error = e instanceof Error ? e.message : String(e);
      this.deviceInfo = null;
    } finally {
      this.loading = false;
    }
  },

  clear() {
    this.node = null;
    this.nodeSelected = null;
    this.dumpImage = null;
    this.matchedNodes = [];
    this.loading = false;
    this.error = null;
  },
  async dumpHierarchy() {
    this.dumping = true;
    try {
      const node = await getUiTree(page.params.id as string, dumpBackendStore.current);
      this.node = node;
    } finally {
      this.dumping = false;
    }
  },
  async freezeScreen(snapshot?: () => Promise<Blob | undefined>) {
    this.freezing = true;
    this.dumpImage = null;
    try {
      const blob = (await snapshot?.()) ?? (await getScreenshot(page.params.id as string));
      if (blob) {
        this.dumpImage = blob;
      }
    } finally {
      this.freezing = false;
    }
  },
  async queryXPath(xpath: string) {
    if (!this.node) {
      await this.dumpHierarchy();
    }
    this.matchedNodes = await queryXPath(this.node!, xpath);
  },
  clearXPathResults() {
    this.matchedNodes = [];
  },
});
