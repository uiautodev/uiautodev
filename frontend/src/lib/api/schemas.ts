export interface DeviceMeta {
    remark?: string;
    wda_port?: number;
    wda_mjpeg_port?: number;
    wda_bundle_id?: string;
}

export interface APIDevice {
    platform: string;
    id: string;
    props: {
        serial: string;
        model?: string;
        product?: string;
        [key: string]: any;
    };
    disabled: boolean;
    meta?: DeviceMeta;
    slave_id?: string;
    slave_name?: string;
    remote?: boolean;
    slave_online?: boolean;
    chain?: string[];
}

export interface Rect {
    x: number;
    y: number;
    width: number;
    height: number;
}

export interface UINode {
    id: string;
    tagName: string;
    rect: Rect;
    props?: { [key: string]: any };
    children: UINode[];
}

export interface VersionCheckResult {
    current_version: string;
    latest_version: string;
    update_available: boolean;
}

export interface UpdateInfo {
    current: string;
    latest: string;
    available: boolean;
    dev: boolean;
    release_url?: string;
    notes?: string;
    published_at?: string;
    asset?: string;
    asset_size?: number;
    download_url?: string;
    desktop: boolean;
}

export interface UpdateProgress {
    active: boolean;
    percent: number;
    error?: string;
}

export interface VersionInfo {
    version: string;
    git_commit: string;
    build_time: string;
}

export interface PluginManifest {
    id?: string;
    name: string;
    version?: string;
    displayName?: string;
    description?: string;
    homepage?: string;
    height?: number;
    platforms?: string[];
    display?: string;
    allowed_hosts?: string[];
}

export interface Plugin {
    id: string;
    src: string;
    manifest: PluginManifest;
    enabled: boolean;
    builtin?: boolean;
}

export interface OpenAIConfig {
    base_url: string;
    api_key: string;
    /** Whether an API key is already persisted. The key is returned masked. */
    api_key_set?: boolean;
    model: string;
    /** Optional proxy URL (http/https/socks5/socks5h). Empty means direct connection. */
    proxy: string;
}

export interface JevConfig {
    /** Full decision endpoint URL, e.g. https://api.typesafe.ai/v1/systemone. */
    endpoint_url: string;
    api_key: string;
    /** Whether an API key is already persisted. The key is returned masked. */
    api_key_set?: boolean;
    model: string;
    /** Optional proxy URL (http/https/socks5/socks5h). Empty means direct connection. */
    proxy: string;
}

export interface PluginSummary {
    id: string;
    name: string;
    author: string;
    description: string;
    icon_url?: string;
    homepage?: string;
    repository?: string;
    license?: string;
    tags: string[];
    platforms: string[];
    latest_version: string;
    downloads: number;
    updated_at: string;
}

export interface PluginVersion {
    version: string;
    sha256: string;
    size: number;
    min_app_version?: string;
    changelog?: string;
    published_at: string;
    download_url: string;
}

export interface MarketItem extends PluginSummary {
    installed: boolean;
    installed_version?: string;
    update_available: boolean;
}

export interface MarketList {
    source: string;
    total: number;
    page: number;
    page_size: number;
    items: MarketItem[];
}

export interface MarketSource {
    url: string;
    default_url: string;
    is_custom: boolean;
}

export interface DeviceEncoder {
    name: string;
    codec: string;
    type: "hw" | "sw" | "hybrid";
    vendor: boolean;
}

export interface DeviceEncoderList {
    video: DeviceEncoder[];
    audio: DeviceEncoder[];
}

export type ClusterSlaveMode = "push" | "direct";

export interface ClusterSlave {
    id: string;
    name: string;
    mode: ClusterSlaveMode;
    url?: string;
    username?: string;
    insecure_tls?: boolean;
    online: boolean;
    disabled: boolean;
    include_nested: boolean;
    node_id?: string;
    last_seen: string;
    version?: string;
    device_count: number;
    created_at: string;
}

export interface ClusterSlaveDirectConfig {
    url: string;
    username: string;
    password: string;
    insecure_tls?: boolean;
}

export interface ClusterSlaveCreate {
    name: string;
    mode?: ClusterSlaveMode;
    direct?: ClusterSlaveDirectConfig;
    include_nested?: boolean;
}

export interface ClusterSlaveUpdate {
    name?: string;
    disabled?: boolean;
    direct?: Partial<ClusterSlaveDirectConfig>;
    include_nested?: boolean;
}

export interface ClusterSlaveTokenResult {
    slave: ClusterSlave;
    token?: string;
}

export const APP_SCOPE_LIST = ["user", "system", "all"] as const;
export type AppScope = (typeof APP_SCOPE_LIST)[number];

export interface DeviceAppList {
    apps: string[];
}

export interface CurrentApp {
    app_id: string;
    activity?: string;
}

export const CLIPBOARD_KIND_LIST = ["text", "image"] as const;

export type ClipboardKind = (typeof CLIPBOARD_KIND_LIST)[number];

export interface DeviceClipboardImage {
    mime: string;
    /** Standard base64, without a data URL prefix. */
    data: string;
}

export interface DeviceClipboard {
    kind: ClipboardKind;
    text?: string;
    image?: DeviceClipboardImage;
}

export type ServerConfig = {
    disable_device_listing?: boolean;
    version: string;
    git_commit: string;
    build_time: string;
}
