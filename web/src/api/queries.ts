import { QueryClient, useQuery } from "@tanstack/react-query";
import { jobsInterval } from "./jobs";
import { get, shouldRetry, verb } from "./request";
import type {
  Candidate,
  ConfigView,
  ForgeStatus,
  Job,
  LoginStatus,
  RegistryAgent,
  Session,
  Status,
  ToolsList,
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
  workspaceStatus: ["workspace-status"],
  candidates: ["workspace-candidates"],
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
