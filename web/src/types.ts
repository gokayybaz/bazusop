export type PageID = "overview" | "fleet" | "services" | "metrics" | "logs" | "jobs" | "alerts" | "cloud" | "activity" | "audit" | "settings"

export type InventoryInstance = {
  agent_id: string
  hostname: string
  os_family: "linux" | "windows"
  os_name: string
  os_version: string
  architecture: string
  kernel_version: string
  cpu_cores: number
  memory_bytes: number
  ip_addresses: string[]
  agent_version: string
  first_seen_at: string
  last_seen_at: string
  status: "connected" | "stale"
}

export type TelemetrySample = {
  recorded_at: string
  cpu_percent: number
  memory_percent: number
  disk_percent: number
  network_rx_bytes: number
  network_tx_bytes: number
}

export type TelemetryPayload = {
  latest: TelemetrySample | null
  samples: TelemetrySample[]
}

export type RuntimeConfiguration = {
  storage: "memory" | "postgresql"
  timescale_enabled: boolean
  telemetry_retention_days: number
  log_retention_days: number
  version: string
  commit: string
  build_date: string
}

export type ManagedService = {
  agent_id: string
  name: string
  display_name: string
  state: "running" | "stopped" | "failed" | "unknown"
  startup_type: "automatic" | "manual" | "disabled" | "unknown"
  observed_at: string
}

export type LogEntry = {
  id: string
  agent_id: string
  occurred_at: string
  collector: "journald" | "file" | "windows_event"
  source: string
  severity: "debug" | "info" | "warn" | "error" | "critical"
  message: string
}

export type OperationJob = {
  id: string
  agent_id: string
  action: "service.restart" | "host.reboot"
  target: string
  approved_by: string
  reason: string
  requested_at: string
  status: "queued" | "running" | "succeeded" | "failed"
  last_sequence: number
  signature: string
  signing_public_key: string
}

export type JobEvent = {
  job_id: string
  sequence: number
  type: "approved" | "claimed" | "output" | "succeeded" | "failed"
  message: string
  actor: string
  occurred_at: string
}

export type AlertIncident = {
  id: string
  rule_id: string
  rule_name: string
  agent_id: string
  severity: "warning" | "critical"
  status: "open" | "acknowledged" | "resolved"
  message: string
  latest_value: number
  opened_at: string
  acknowledged_at?: string
  acknowledged_by?: string
  resolved_at?: string
}

export type AlertRule = { id: string; name: string; kind: "metric" | "reachability"; metric: "cpu" | "memory" | "disk" | ""; threshold: number; stale_after_seconds: number; severity: "warning" | "critical"; enabled: boolean; created_at: string }
export type MaintenanceWindow = { id: string; name: string; agent_id: string; starts_at: string; ends_at: string; created_by: string; created_at: string }
export type CloudAccount = { id: string; name: string; provider: "aws" | "azure" | "gcp"; external_id: string; status: "pending" | "connected"; last_sync_at?: string }
export type CloudInstance = { account_id: string; account_name: string; provider: CloudAccount["provider"]; provider_instance_id: string; name: string; region: string; zone?: string; state: string; os_family: "linux" | "windows" | "unknown"; private_ips: string[]; public_ips: string[]; agent_id_hint?: string; agent_id: string; candidate_agent_id?: string; match_status: "verified" | "candidate" | "unmatched"; match_reason: string }
export type ActivityEvent = { organization_id: string; site_id: string; source: "job" | "alert" | "identity" | "site_role" | "service_account"; reference_id: string; agent_id: string; type: string; actor: string; message: string; occurred_at: string }

export type AuditTrailEvent = { event_id: string; occurred_at: string; correlation_id: string; actor_type: "agent" | "human" | "service_account" | "legacy_token" | "anonymous"; actor_id: string; session_or_token_id: string; organization_id: string; site_id: string; action: string; permission: string; resource_type: string; resource_id: string; outcome: "success" | "failure"; error_code: string; source_ip: string; user_agent: string; change_summary: string }

export type UserSiteRole = { site_id: string; role: string }
export type ManagedUser = { id: string; email: string; role: string; disabled_at?: string; totp_confirmed_at?: string; site_roles: UserSiteRole[] }
