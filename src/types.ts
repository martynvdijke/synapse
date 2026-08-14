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

export interface AutheliaInstanceJSON {
  id: number;
  name: string;
  config_path: string;
  db_path: string;
  default_policy: string;
  overrides: string;
  auto_sync: boolean;
  enabled: boolean;
  npm_instance_ids: string;
  created_at: string;
}

export interface MonitorResponse {
  id: number;
  name: string;
  type: string;
  url: string;
  docker_container: string;
  interval?: number;
  retry_interval?: number;
  maxretries?: number;
  status?: number;
  uptime_24h?: number;
  uptime_7d?: number;
  uptime_1y?: number;
  ping?: number;
  last_msg?: string;
  instance_id: number;
  instance_name: string;
}

export interface ProxyLocation {
  path: string;
  forward_host: string;
  forward_port: number;
  forward_scheme: string;
  advanced_config: string;
}

export interface NPMProxyHost {
  id: number;
  domain_names: string[];
  forward_host: string;
  forward_port: number;
  forward_scheme: string;
  enabled: boolean;
  ssl_forced: boolean;
  certificate_id?: number;
  http2_support?: boolean;
  hsts_enabled?: boolean;
  hsts_subdomains?: boolean;
  block_exploits?: boolean;
  caching_enabled?: boolean;
  allow_websocket_upgrade?: boolean;
  access_list_id?: number;
  advanced_config?: string;
  locations?: ProxyLocation[];
  instance_id: number;
  instance_name?: string;
}

export interface CreateProxyHostInput {
  instance_id: number;
  domain_names: string[];
  forward_host: string;
  forward_port: number;
  forward_scheme: string;
  enabled?: boolean;
  ssl_forced?: boolean;
  certificate_id?: number;
  http2_support?: boolean;
  hsts_enabled?: boolean;
  hsts_subdomains?: boolean;
  block_exploits?: boolean;
  caching_enabled?: boolean;
  allow_websocket_upgrade?: boolean;
  access_list_id?: number;
  advanced_config?: string;
  locations?: ProxyLocation[];
  service_name?: string;
}

export interface ServiceLink {
  id: number;
  service_name: string;
  npm_instance_id?: number;
  npm_instance_name?: string;
  npm_host_name?: string;
  npm_details?: string;
  kuma_instance_id?: number;
  kuma_instance_name?: string;
  kuma_monitor_id?: number;
  kuma_monitor_name?: string;
  kuma_details?: string;
  created_at: string;
  updated_at?: string;
}

export interface ServiceLinkInput {
  service_name: string;
  npm_instance_id?: number | null;
  npm_host_name?: string;
  kuma_instance_id?: number | null;
  kuma_monitor_id?: number | null;
  kuma_monitor_name?: string;
}

export interface CreateMonitorInput {
  instance_id: number;
  name: string;
  type: string;
  url?: string;
  docker_container?: string;
  docker_host?: number;
  interval?: number;
  retry_interval?: number;
  maxretries?: number;
  service_name?: string;
}

export interface MonitorEditInput {
  name?: string;
  type?: string;
  url?: string;
  docker_container?: string;
  docker_host?: number;
  interval?: number;
  retry_interval?: number;
  maxretries?: number;
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
  container_state?: string;
  container_status?: string;
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
  eink_enabled?: boolean;
  trmnl_api_token?: string;
  notify_enabled?: boolean;
  notify_interval_minutes?: number;
  gotify_url?: string;
  gotify_token?: string;
  gotify_priority?: number;
  docker_socket?: string;
  docker_events_enabled?: boolean;
  docker_events_retention_days?: number;
  reconcile_enabled?: boolean;
  reconcile_interval_minutes?: number;
  reconcile_dry_run_default?: boolean;
  notify_docker_die?: boolean;
  notify_docker_health?: boolean;
  notify_docker_image?: boolean;
  notify_reconcile?: boolean;
  notify_cooldown_minutes?: number;
  [key: string]: unknown;
}

export interface NotifyMissingResponse {
  docker: string[];
  npm: string[];
  fetched_at: string;
  degraded: boolean;
  reasons?: string[];
}

export interface SyncRun {
  id: number;
  source: string;
  status: string;
  started_at: string;
  added: number;
  updated?: number;
  skipped: number;
  failed: number;
  dry_run?: boolean;
  error_message: string;
}

export interface FeedItem {
  time: string;
  kind: string;
  title: string;
  detail: string;
  status: string;
}

export interface ReconcileResult {
  changes: Array<{
    service: string;
    target: string;
    action: string;
    detail: string;
  }>;
  dry_run: boolean;
  run: SyncRun;
}

export interface AutheliaStatusResponse {
  configured: boolean;
  error?: string;
  message?: string;
  domains?: string[];
  npm_cnames?: string[];
  matched?: string[];
  missing?: string[];
  open_alerts?: number;
  instance_id?: number;
  instance_name?: string;
  instance_count?: number;
  sync_enabled?: boolean;
  default_policy?: string;
}

export interface AutheliaAlert {
  id: number;
  cname: string;
  message: string;
  severity: string;
  status: string;
  authelia_instance_id?: number;
}

export interface TempAccessRule {
  id: number;
  ip: string;
  reason: string;
  expires_at: string;
  status: string;
  authelia_instance_id?: number;
}

export interface AutheliaSyncAction {
  action: string;
  cname: string;
  policy?: string;
  message: string;
}

export interface AutheliaSyncInstanceResult {
  instance_id: number;
  instance_name: string;
  error?: string;
  added?: number;
  skipped?: number;
  alerted?: number;
  actions?: AutheliaSyncAction[];
}

export interface AutheliaSyncResult {
  error?: string;
  added?: number;
  skipped?: number;
  alerted?: number;
  actions?: AutheliaSyncAction[];
  instance_id?: number;
  instance_name?: string;
  dry_run?: boolean;
  message?: string;
  instances?: AutheliaSyncInstanceResult[];
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
