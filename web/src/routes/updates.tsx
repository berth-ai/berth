import { createFileRoute } from "@tanstack/react-router";
import { UpdatesPage } from "@/pages/updates";

export const Route = createFileRoute("/updates")({ component: UpdatesPage });
