import { QueryClient, useQuery } from "@tanstack/react-query";
import { jobsInterval } from "./jobs";
import { get, shouldRetry, verb } from "./request";
import type {
  Candidate,
  ConfigView,
  ForgeStatus,
  InstructionFile,
  Job,
  LoginStatus,
  OrcaDevice,
  OrcaStatus,
  RegistryAgent,
  Session,
  Status,
  ToolsList,
  WorkspaceReport,
  WorkspaceState,
} from "./types";

export const queryClient = new QueryClient({ defaultOptions: { queries: { retry: shouldRetry } } });

export const keys = {
  session: ["session"],
  status: ["status"],
  jobs: ["jobs"],
  job: (id: number) => ["job", id],
  config: ["config"],
  tools: ["tools"],
  forges: ["forges"],
  logins: ["logins"],
  registry: ["registry"],
  agentRegistry: ["agent-registry"],
  versions: (tool: string) => ["versions", tool],
  workspaceReport: (id: string) => ["workspace-report", id],
  instructions: (id: string, file: string) => ["instructions", id, file],
  workspaceStatus: ["workspace-status"],
  candidates: ["workspace-candidates"],
  orcaDevices: ["orca-devices"],
  orcaStatus: ["orca-status"],
} as const;

export function useSession() {
  return useQuery({
    queryKey: keys.session,
    queryFn: () => get<Session>("/api/session"),
    staleTime: Infinity,
  });
}

export function useStatus() {
  return useQuery({
    queryKey: keys.status,
    queryFn: () => get<Status>("/api/status"),
    refetchInterval: 5000,
  });
}

export function useJobs() {
  return useQuery({
    queryKey: keys.jobs,
    queryFn: () => get<{ jobs: Job[] }>("/api/jobs"),
    refetchInterval: (q) => jobsInterval(q.state.data?.jobs),
  });
}

/** useJob polls one job every second until it leaves "running". */
export function useJob(id: number | null) {
  return useQuery({
    queryKey: keys.job(id ?? 0),
    queryFn: () => get<{ job: Job }>(`/api/jobs/${id}`),
    enabled: id !== null,
    refetchInterval: (q) => (q.state.data?.job.state === "running" ? 1000 : false),
  });
}

/** useConfig loads the config once per page visit; the settings form owns it after that. */
export function useConfig() {
  return useQuery({
    queryKey: keys.config,
    queryFn: () => verb<ConfigView>("config.get"),
    staleTime: Infinity,
    refetchOnMount: "always",
  });
}

export function useTools() {
  return useQuery({ queryKey: keys.tools, queryFn: () => verb<ToolsList>("tools.list") });
}

export function useForges() {
  return useQuery({ queryKey: keys.forges, queryFn: () => verb<ForgeStatus>("forge.status") });
}

export function useRegistry(enabled: boolean) {
  return useQuery({
    queryKey: keys.registry,
    queryFn: () => verb<{ tools: string[] }>("tools.registry"),
    staleTime: Infinity,
    enabled,
  });
}

export function useToolVersions(tool: string) {
  return useQuery({
    queryKey: keys.versions(tool),
    queryFn: () => verb<{ versions: string[] }>("tools.versions", { tool }),
    enabled: tool !== "",
    staleTime: 60_000,
  });
}

/** refreshStatus reloads the status now, after an action changed it. */
export function refreshStatus(): Promise<void> {
  return queryClient.invalidateQueries({ queryKey: keys.status });
}

export function useLogins() {
  return useQuery({ queryKey: keys.logins, queryFn: () => verb<LoginStatus>("login.status") });
}

export function refreshLogins() {
  return queryClient.invalidateQueries({ queryKey: keys.logins });
}

export function useAgentRegistry() {
  return useQuery({
    queryKey: keys.agentRegistry,
    queryFn: () => verb<{ agents: RegistryAgent[] }>("agents.registry"),
  });
}

export function refreshAgentRegistry() {
  return queryClient.invalidateQueries({ queryKey: keys.agentRegistry });
}

/** useWorkspaceStatus polls every workspace's folder and session. */
export function useWorkspaceStatus() {
  return useQuery({
    queryKey: keys.workspaceStatus,
    queryFn: () => verb<{ workspaces: Record<string, WorkspaceState> }>("workspaces.status"),
    refetchInterval: 5000,
  });
}

export function useCandidates(enabled: boolean) {
  return useQuery({
    queryKey: keys.candidates,
    queryFn: () => verb<{ candidates: Candidate[] }>("workspaces.candidates"),
    enabled,
  });
}

/** refreshWorkspaces reloads what a workspace change touches. */
export function refreshWorkspaces(): Promise<unknown> {
  return Promise.all(
    [keys.config, keys.workspaceStatus, keys.candidates].map((queryKey) =>
      queryClient.invalidateQueries({ queryKey }),
    ),
  );
}

/** useWorkspaceReport polls what Berth wrote into workspace id. */
export function useWorkspaceReport(id: string) {
  return useQuery({
    queryKey: keys.workspaceReport(id),
    queryFn: () =>
      verb<{ report: WorkspaceReport }>("workspaces.report", { id }).then((r) => r.report),
    refetchInterval: 5000,
  });
}

/** refreshWorkspaceReport reloads a workspace's report now. */
export function refreshWorkspaceReport(id: string): Promise<void> {
  return queryClient.invalidateQueries({ queryKey: keys.workspaceReport(id) });
}

export function readInstructionFile(id: string, file: string) {
  return verb<{ file: InstructionFile }>("workspaces.instructions.read", { id, file }).then(
    (r) => r.file,
  );
}

export function useInstructionFile(id: string, file: string) {
  return useQuery({
    queryKey: keys.instructions(id, file),
    queryFn: () => readInstructionFile(id, file),
  });
}

/** useOrcaDevices polls the Orca apps paired with this Berth, so a phone
 * shows up once it uses its link. */
export function useOrcaDevices(enabled: boolean) {
  return useQuery({
    queryKey: keys.orcaDevices,
    queryFn: () => verb<{ devices: OrcaDevice[] }>("orca.devices").then((r) => r.devices),
    enabled,
    refetchInterval: 5000,
  });
}

/** useOrcaStatus polls which workspaces Orca has: often while a sync runs,
 * rarely otherwise, as each poll asks Orca. */
export function useOrcaStatus(enabled: boolean) {
  return useQuery({
    queryKey: keys.orcaStatus,
    queryFn: () => verb<OrcaStatus>("orca.status"),
    enabled,
    refetchInterval: (q) => (q.state.data?.syncing ? 2000 : 30000),
  });
}

/** refreshOrcaStatus reloads the Orca status now, after Sync now. */
export function refreshOrcaStatus(): Promise<void> {
  return queryClient.invalidateQueries({ queryKey: keys.orcaStatus });
}

/** refreshOrcaDevices reloads the paired devices now, after a pairing. */
export function refreshOrcaDevices(): Promise<void> {
  return queryClient.invalidateQueries({ queryKey: keys.orcaDevices });
}
