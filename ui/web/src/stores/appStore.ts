import { createApiClient } from '../api/client';

export interface AppSettings {
  apiBaseUrl: string;
  wsBaseUrl: string;
}

const defaults: AppSettings = {
  apiBaseUrl: import.meta.env.VITE_AGENT_API_BASE_URL || 'http://127.0.0.1:8765',
  wsBaseUrl: import.meta.env.VITE_AGENT_WS_BASE_URL || 'ws://127.0.0.1:8765'
};

let settings: AppSettings = { ...defaults };

export function getAppSettings(): AppSettings {
  return { ...settings };
}

export function updateAppSettings(next: Partial<AppSettings>): AppSettings {
  settings = { ...settings, ...next };
  return getAppSettings();
}

export function getConfiguredApiClient() {
  return createApiClient({ baseUrl: settings.apiBaseUrl });
}

