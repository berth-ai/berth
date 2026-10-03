import { createFileRoute } from "@tanstack/react-router";
import { SkillsPage } from "@/pages/skills";

export const Route = createFileRoute("/skills")({ component: SkillsPage });
