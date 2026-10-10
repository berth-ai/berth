import { createFileRoute } from "@tanstack/react-router";
import { SkillsPage } from "@/pages/skills";
import { parseSkillView, type SkillView } from "@/pages/skills-logic";

export const Route = createFileRoute("/skills/")({
  validateSearch: (search: Record<string, unknown>): { view?: SkillView } =>
    parseSkillView(search.view) === "flat" ? { view: "flat" } : {},
  component: SkillsRoute,
});

function SkillsRoute() {
  // Parsed again, as in the Tools route: an unknown ?view= survives {}.
  const view = parseSkillView(Route.useSearch().view);
  const navigate = Route.useNavigate();
  return (
    <SkillsPage
      view={view}
      onView={(v) => void navigate({ search: v === "flat" ? { view: v } : {}, replace: true })}
    />
  );
}
