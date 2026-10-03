import { createFileRoute } from "@tanstack/react-router";
import { McpPage } from "@/pages/mcp";

export const Route = createFileRoute("/mcp")({ component: McpPage });
