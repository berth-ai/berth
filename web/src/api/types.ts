export type Mode = "normal" | "setup" | "recovery" | "";

export interface Session {
  user: string | null;
  mode: Mode;
}

export interface ServiceStatus {
  name: string;
  state: string;
  pid: number;
  started_at: string | null;
  restarts: number;
  log: string[] | null;
}

export interface LockSource {
  origin: string;
  repo?: string;
  ref?: string;
  commit?: string;
  fetched_at: string;
}

export interface AgentVersion {
  hash: string;
  source: LockSource;
  packages: Record<string, string> | null;
  installed_at: string;
}

export interface UpdateCheck {
  ref: string;
  commit?: string;
  hash?: string;
  update_available: boolean | null;
  checked_at: string;
  error?: string;
}

export interface AgentsReport {
  current: AgentVersion | null;
  previous: AgentVersion | null;
  versions: AgentVersion[] | null;
  follow_image: boolean;
  build: { version?: string; commit?: string; arch?: string } | null;
  warnings: string[] | null;
}

export interface PackageKey {
  name: string;
  set: boolean;
}

export interface AgentPackage {
  package: string;
  name: string;
  kind: string;
  version: string;
  enabled: boolean;
  bins: string[] | null;
  keys: PackageKey[] | null;
  mcp_id?: string;
}

export type McpAuth = "none" | "bearer" | "oauth";

export interface McpServer {
  url: string;
  auth: McpAuth;
  enabled: boolean;
}

export interface HostInfo {
  bind_address: string;
  panel_port: string;
  puid: string;
  pgid: string;
  workspaces_dir: string;
}

export interface Status {
  mode: Mode;
  config_error?: string;
  revision: number;
  services: ServiceStatus[] | null;
  agents: AgentsReport | null;
  agents_error?: string;
  packages: AgentPackage[] | null;
  check: UpdateCheck | null;
  host: HostInfo | null;
}

export interface Config {
  schema: number;
  revision: number;
  frontends: { terminal: { enabled: boolean }; ide: { enabled: boolean } };
  agents: {
    ref: string;
    disabled: string[] | null;
    keep: number | null;
    boot_timeout: number | null;
    mcp_timeout: number | null;
  };
  runtime: { tz: string };
  // An older broker omits it.
  mcp?: {
    servers: Record<string, McpServer> | null;
    available: Record<string, string[]> | null;
  };
}

export interface ConfigView {
  config: Config;
  mode: Mode;
  secrets?: Record<string, { set: boolean }>;
}

export interface Changes {
  started: string[] | null;
  restarted: string[] | null;
  stopped: string[] | null;
}

export interface ApplyResult {
  config: Config;
  changes: Changes | null;
}

export interface JobError {
  code: string;
  message?: string;
  step?: string;
}

export interface Device {
  code: string;
  url: string;
}

export interface Job {
  id: number;
  verb: string;
  group: string;
  args: string[] | null;
  state: string;
  started_at: string;
  finished_at: string | null;
  exit_code: number | null;
  result: { error?: JobError } | null;
  log?: string;
  log_truncated: boolean;
  device?: Device;
}

export interface Tool {
  name: string;
  version: string;
}

export interface Preset {
  id: string;
  name: string;
  description: string;
  tools: Record<string, string>;
  state: string;
}

export interface ToolsList {
  base: Tool[];
  user: Tool[];
  presets: Preset[];
  warnings: string[] | null;
}

export interface Forge {
  host: string;
  signed_in: boolean;
  user: string;
}

export interface ForgeStatus {
  git: { name: string; email: string };
  forges: Record<string, Forge>;
}
