import { get } from "svelte/store";
import { addMessages, getLocaleFromNavigator, init, locale } from "svelte-i18n";
import zhCN from "./locales/zh-CN.json";
import en from "./locales/en.json";

const LOCALE_KEY = "uiautodev-locale";
const SUPPORTED = ["zh-CN", "en"] as const;

export const LOCALES = [
  { code: "zh-CN", label: "简体中文" },
  { code: "en", label: "English" },
] as const;

export type LocaleCode = (typeof LOCALES)[number]["code"];

function isSupported(value: unknown): value is LocaleCode {
  return typeof value === "string" && (SUPPORTED as readonly string[]).includes(value);
}

function resolveInitialLocale(): LocaleCode {
  if (typeof window !== "undefined") {
    try {
      const stored = window.localStorage.getItem(LOCALE_KEY);
      if (isSupported(stored)) return stored;
    } catch {
      // ignore
    }
    const navigatorLocale = getLocaleFromNavigator();
    if (typeof navigatorLocale === "string") {
      const lower = navigatorLocale.toLowerCase();
      if (lower.startsWith("zh")) return "zh-CN";
      if (lower.startsWith("en")) return "en";
    }
  }
  return "zh-CN";
}

let initialized = false;

import { isWailsEnv } from "$lib/wails-env";

async function syncLocaleToBackend(value: LocaleCode): Promise<void> {
  if (!isWailsEnv()) return;
  for (let attempt = 0; attempt < 10; attempt++) {
    try {
      const { setLocalePreference } = await import("$lib/api/api.js");
      await setLocalePreference(value);
      return;
    } catch {
      // The local API may still be starting; retry briefly.
      await new Promise((resolve) => setTimeout(resolve, 300));
    }
  }
}

export async function initI18n(): Promise<void> {
  if (initialized) return;
  initialized = true;

  addMessages("zh-CN", zhCN);
  addMessages("en", en);

  const initial = resolveInitialLocale();
  await init({
    fallbackLocale: "zh-CN",
    initialLocale: initial,
  });
  void syncLocaleToBackend(initial);

  locale.subscribe((value) => {
    if (!value) return;
    if (typeof document !== "undefined") {
      document.documentElement.lang = value;
    }
    if (typeof window !== "undefined") {
      try {
        window.localStorage.setItem(LOCALE_KEY, value);
      } catch {
        // ignore
      }
    }
  });
}

export function getLocale(): LocaleCode {
  return get(locale) as LocaleCode;
}

export async function setLocale(value: LocaleCode): Promise<void> {
  await locale.set(value);
  await syncLocaleToBackend(value);
}
