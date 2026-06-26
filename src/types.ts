// Shared type definitions for Synapse dashboard API responses

export interface KumaInstanceJSON {
  id: number;
  name: string;
  url: string;
  username: string;
  password: string;
  enabled: boolean;
  created_at: string;
}

export interface NPMInstanceJSON {
  id: number;
  name: string;
  url: string;
  username: string;
  password: string;
  enabled: boolean;
  created_at: string;
}

export interface MonitorResponse {
  id: number;
  name: string;
  type: string;
  url: string;
  docker_container: string;
  instance_id: number;
  instance_name: string;
}

export interface MonitorStats {
  status: number;
  uptime_24h: number;
  uptime_7d: number;
  uptime_1y: number;
  avg_ping: number;
  last_msg: string;
  cert_info: string;
}

export interface ServiceInfo {
  name: string;
  container_name: string;
  image: string;
  type: string;
  url: string;
  in_kuma: boolean;
  ports?: unknown;
  environment?: unknown;
  volumes?: unknown;
  depends_on?: string[];
  labels?: unknown;
  restart?: string;
  command?: string;
  entrypoint?: string;
  user?: string;
  working_dir?: string;
  healthcheck?: {
    test: string | string[];
    interval?: string;
    timeout?: string;
    retries?: number;
    start_period?: string;
  };
}

export interface ProxyResponse {
  cname: string;
  container: string;
  source_instance_name?: string;
  in_kuma: boolean;
}

export interface StatusResponse {
  docker_count: number;
  npm_count: number;
  npm_error: string;
  monitor_count: number;
  running: boolean;
  docker_error: string;
  kuma_error: string;
  last_docker: string;
  last_npm: string;
  connection_health?: {
    docker?: { ok: boolean };
    npm?: {
      ok: boolean;
      instances?: Array<{ id: number; name: string; ok: boolean; last_error: string }>;
    };
    kuma?: {
      ok: boolean;
      instances: Array<{ id: number; name: string; ok: boolean; last_error: string }>;
    };
  };
}

export interface SettingsResponse {
  compose_path: string;
  authelia_config_path: string;
  authelia_db_path: string;
  authelia_sync_enabled: boolean;
  authelia_default_policy: string;
  authelia_sync_overrides: string;
  [key: string]: unknown;
}

export interface SyncRun {
  id: number;
  source: string;
  status: string;
  started_at: string;
  added: number;
  skipped: number;
  failed: number;
  error_message: string;
}

export interface AutheliaStatusResponse {
  configured: boolean;
  error?: string;
  domains?: string[];
  npm_cnames?: string[];
  matched?: string[];
  open_alerts?: number;
}

export interface AutheliaAlert {
  id: number;
  cname: string;
  message: string;
  severity: string;
  status: string;
}

export interface TempAccessRule {
  id: number;
  ip: string;
  reason: string;
  expires_at: string;
  status: string;
}

export interface AutheliaSyncResult {
  error?: string;
  added?: number;
  skipped?: number;
  alerted?: number;
  actions?: Array<{
    action: string;
    cname: string;
    policy?: string;
    message: string;
  }>;
}

export interface ProgressEvent {
  total: number;
  current: number;
  message: string;
  added: number;
  skipped: number;
  failed: number;
  status: string;
}

export interface LogEntry {
  timestamp: string;
  level: string;
  source: string;
  message: string;
  duration: number;
  error: string;
  error_kind: string;
  metadata: Record<string, unknown>;
}

export interface LogFilters {
  level: string;
  source: string;
  search: string;
  error_kind: string;
}
