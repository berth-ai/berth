import { createFileRoute } from "@tanstack/react-router";
import { OrcaPage } from "@/pages/orca";

export const Route = createFileRoute("/orca")({ component: OrcaPage });
