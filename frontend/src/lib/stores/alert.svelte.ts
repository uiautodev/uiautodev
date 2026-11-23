import type { AlertVariant } from "$lib/components/ui/alert";

export interface AppAlert {
  id: number;
  title?: string;
  message: string;
  variant: AlertVariant;
  dismissible: boolean;
}

export interface AlertOptions {
  title?: string;
  duration?: number;
  dismissible?: boolean;
}

let nextId = 1;

export const alertStore = $state({
  alerts: [] as AppAlert[],

  info(message: string, options: AlertOptions = {}): number {
    return this.push("info", message, options, 3000);
  },

  success(message: string, options: AlertOptions = {}): number {
    return this.push("success", message, options, 3000);
  },

  warning(message: string, options: AlertOptions = {}): number {
    return this.push("warning", message, options, 5000);
  },

  error(message: string, options: AlertOptions = {}): number {
    return this.push("error", message, options, 5000);
  },

  push(variant: AlertVariant, message: string, options: AlertOptions, defaultDuration: number): number {
    const id = nextId++;
    const duration = options.duration ?? defaultDuration;
    const prompt: AppAlert = {
      id,
      title: options.title,
      message,
      variant,
      dismissible: options.dismissible ?? true,
    };
    this.alerts = [...this.alerts, prompt];
    if (duration > 0) {
      setTimeout(() => this.dismiss(id), duration);
    }
    return id;
  },

  dismiss(id: number) {
    this.alerts = this.alerts.filter((a) => a.id !== id);
  },

  dismissAll() {
    this.alerts = [];
  },
});