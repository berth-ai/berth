import { createFileRoute } from "@tanstack/react-router";
import { AgentsPage } from "@/pages/agents";

export const Route = createFileRoute("/agents/")({ component: AgentsPage });
