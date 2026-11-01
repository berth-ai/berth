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
  skills_dir?: string;
  skills_via?: string[];
  skills_project?: string;
  mcp_project?: string;
  mcp_trust?: boolean;
  /** instructions is the agent's instruction file at a workspace's root. */
  instructions?: string;
  icon?: string;
  // An older broker omits these.
  origin?: "locked" | "added";
  previous?: string;
  error?: string;
}

export type McpAuth = "none" | "bearer" | "oauth";

export interface McpServer {
  url: string;
  auth: McpAuth;
  enabled: boolean;
}

/** Found is one SKILL.md of a source's last scan; an error row cannot be attached. */
export interface Found {
  path: string;
  description?: string;
  error?: string;
}

/** SkillMode is how a workspace gets a skill: linked and hidden from git, or copied in. */
export type SkillMode = "link" | "vendor";

/** Attachment gives a found skill to agents: null (or, from the devserver,
 * absent) is every agent including ones added later, [] is none; workspaces
 * get it linked or vendored. */
export interface Attachment {
  enabled: boolean;
  agents?: string[] | null;
  workspaces?: Record<string, SkillMode> | null;
}

/** Source is a repository folder at a ref, pinned to a commit. */
export interface Source {
  repo: string;
  ref: string;
  path: string;
  commit: string;
  found: Record<string, Found> | null;
  attached: Record<string, Attachment> | null;
}

/** DiffRow is one skill a preview reports; a moved row's from is its old path. */
export interface DiffRow {
  name: string;
  path: string;
  from?: string;
  description?: string;
  error?: string;
  attached: boolean;
}

/** SourceDiff is skills.sources.preview's answer for an existing source. */
export interface SourceDiff {
  id: string;
  ref: string;
  from: string;
  commit: string;
  changed: boolean;
  added: DiffRow[];
  removed: DiffRow[];
  moved: DiffRow[];
  described: DiffRow[];
}

/** NewSourceScan is skills.sources.preview's answer for a repository not added yet. */
export interface NewSourceScan {
  commit: string;
  found: Record<string, Found>;
  id: string;
  exists: string;
}

/** SourceResult is what skills.sources.add and skills.sources.update answer. */
export interface SourceResult {
  id?: string;
  commit?: string;
  changed?: boolean;
  revision?: number;
  attached?: string[];
  skipped?: string[];
  missing?: string[];
}

/** WorkspaceSkill is one skill Berth placed in a workspace (wsstate.Skill). */
export interface WorkspaceSkill {
  name: string;
  mode: SkillMode;
  links?: string[] | null;
  dir?: string;
  edited?: boolean;
}

/** WorkspaceMcpFile is Berth's part of one project MCP file. */
export interface WorkspaceMcpFile {
  style: string;
  key?: string;
  ids: string[] | null;
  created?: boolean;
  tracked?: boolean;
}

/** WorkspaceReport is what Berth recorded writing into a workspace
 * (workspaces.report): entries keyed "<source>/<name>", files by path. */
export interface WorkspaceReport {
  skills: {
    path?: string;
    entries?: Record<string, WorkspaceSkill> | null;
    skipped?: Record<string, string> | null;
    error?: string;
  };
  mcp: {
    path?: string;
    files?: Record<string, WorkspaceMcpFile> | null;
    trust?: boolean;
    skipped?: Record<string, string> | null;
    error?: string;
  };
  instructions?: {
    path?: string;
    links?: string[] | null;
    skipped?: Record<string, string> | null;
    error?: string;
  };
}

/** InstructionFile is one instruction file as workspaces.instructions.read reports it. */
export interface InstructionFile {
  file: string;
  state: "file" | "missing" | "symlink" | "other";
  target?: string;
  /** owned: Berth made the link, because the workspace shares AGENTS.md. */
  owned?: boolean;
  content: string;
  sha256: string;
  size: number;
  editable: boolean;
  reason?: string;
  tracked: boolean;
}

export interface WorkspaceEnvVar {
  value: string;
  secret: boolean;
}

export interface Workspace {
  path: string;
  repo: string;
  env?: Record<string, WorkspaceEnvVar> | null;
  instructions?: { shared: boolean };
}

/** WorkspaceState is one workspace as workspaces.status reports it. */
export interface WorkspaceState {
  exists: boolean;
  writable: boolean;
  git: boolean;
  /** branch is empty on a detached HEAD. */
  branch: string;
  head: string;
  /** dirty counts changed paths; -1 means not known. */
  dirty: number;
  session: boolean;
  windows: string[] | null;
  unknown: boolean;
}

/** Candidate is a folder in /workspaces that can be adopted. */
export interface Candidate {
  path: string;
  git: boolean;
}

export interface HostInfo {
  bind_address: string;
  panel_port: string;
  // An older broker omits it.
  orca_port?: string;
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
  frontends: {
    terminal: { enabled: boolean };
    ide: { enabled: boolean };
    // An older broker omits it.
    orca?: { enabled: boolean; pairing_address: string };
  };
  agents: {
    ref: string;
    disabled: string[] | null;
    // An older broker omits it.
    added?: string[] | null;
    keep: number | null;
    boot_timeout: number | null;
    mcp_timeout: number | null;
  };
  runtime: { tz: string };
  // An older broker omits it.
  mcp?: {
    servers: Record<string, McpServer> | null;
    available: Record<string, string[]> | null;
    workspaces?: Record<string, string[]> | null;
  };
  // An older broker omits it.
  skills?: { sources?: Record<string, Source> | null } | null;
  // An older broker omits it.
  workspaces?: Record<string, Workspace> | null;
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
  left?: string[];
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
  result: {
    error?: JobError & { field?: string };
    changed?: boolean;
    commit?: string;
    id?: string;
    path?: string;
    writable?: boolean;
    git?: boolean;
    deleted?: boolean;
  } | null;
  log?: string;
  log_truncated: boolean;
  device?: Device;
  login?: LoginState;
}

export interface LoginState {
  agent: string;
  method: string;
  flow: string;
  url: string;
  code: string;
  waiting: boolean;
  inputs: number;
  signed_in: boolean;
  account: string;
  failed: string;
}

export interface LoginMethodStatus {
  id: string;
  label: string;
  flow: string;
  signed_in: boolean;
  account: string;
  error: string;
}

export interface LoginStatus {
  agents: Record<string, { name: string; methods: LoginMethodStatus[] | null }> | null;
}

/** UsageTokens counts tokens by kind; cache reads are input served from
 * the prompt cache, cache writes input stored in it. */
export interface UsageTokens {
  input: number;
  output: number;
  cache_read: number;
  cache_write: number;
}

export interface UsagePeriod extends UsageTokens {
  total: number;
  messages: number;
  sessions: number;
}

export interface UsageModel extends UsageTokens {
  model: string;
  total: number;
  messages: number;
}

/** AgentUsage is one agent in agents.usage, read from its own session files. */
export interface AgentUsage {
  name: string;
  /** tracked is false when Berth cannot read this agent's usage. */
  tracked: boolean;
  today: UsagePeriod;
  week: UsagePeriod;
  /** days runs oldest first, one entry per calendar day. */
  days: { date: string; total: number }[] | null;
  models: UsageModel[] | null;
  last_active: string | null;
  error: string;
}

export interface UsageReport {
  schema: number;
  since: string;
  agents: Record<string, AgentUsage> | null;
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
  /** tea's servers; gh and glab sign in to one host and omit it. */
  logins?: ForgeLogin[];
}

/** ForgeLogin is one Gitea or Forgejo server tea is signed in to. */
export interface ForgeLogin {
  name: string;
  url: string;
  user: string;
  default: boolean;
}

export interface ForgeStatus {
  git: { name: string; email: string };
  forges: Record<string, Forge>;
}

/** RegistryAgent is one agent the image's registry lets a user add. */
export interface RegistryAgent {
  id: string;
  package: string;
  name: string;
  description: string;
  version: string;
  icon?: string;
  keys: string[] | null;
  login: string[] | null;
  added: boolean;
  installed_version?: string;
  error?: string;
}

/** OrcaPair is orca.pair's answer: a link for one Orca app and its QR
 * modules, row by row, "1" for dark. */
export interface OrcaPair {
  url: string;
  scope: "desktop" | "mobile";
  address: string;
  warning?: string;
  qr: string[];
}

/** OrcaProject is one workspace as Orca sees it, from orca.status. error
 * is why the last sync could not add a folder Orca does not have. */
export interface OrcaProject {
  path: string;
  in_orca: boolean;
  error?: string;
}

/** OrcaStatus is orca.status: each workspace by id, when the last sync
 * ended (null before the first) and whether one runs. */
export interface OrcaStatus {
  workspaces: Record<string, OrcaProject>;
  synced_at: string | null;
  syncing: boolean;
}

/** OrcaDevice is one Orca app paired with this Berth, from orca.devices.
 * last_seen_at is null until the app first connects. */
export interface OrcaDevice {
  id: string;
  name: string;
  scope: "desktop" | "mobile";
  paired_at: string | null;
  last_seen_at: string | null;
}
