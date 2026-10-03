// Recovery mode (§10): the stored config cannot be read. Only the panel runs.
import { verb, errorText } from "../api.js";
import { h, card, alert, button, confirmDialog, toast, describeChanges } from "../ui.js";

export function render(ctx) {
  const st = ctx.status() || {};
  const act = (title, verbName, body, confirmLabel, danger) => () => confirmDialog({
    title, body, confirm: confirmLabel, danger,
    action: async () => {
      const r = await verb(verbName, {});
      if (!r.ok) {
        toast(errorText(r), "bad");
        return false;
      }
      toast(describeChanges(r.body.changes));
      ctx.refresh();
      return true;
    },
  });
  const el = h("div", { class: "page narrow" },
    h("header", { class: "page-head" }, h("div", null, h("h1", { text: "Berth needs attention" }),
      h("p", { class: "muted", text: "The settings file cannot be read, so only the panel is running." }))),
    card({
      title: "Settings could not be read",
      body: [
        alert("bad", st.config_error || "The configuration is unusable."),
        h("p", { text: "Revert to the previous revision if you changed something recently. Otherwise reset to the defaults. The unreadable file is kept as config.corrupt.json for inspection." }),
      ],
      footer: [
        button("Revert to previous config", { kind: "primary", onclick: act("Revert to the previous config?",
          "config.revert", "Berth applies the revision before the current one and starts the services it enables.", "Revert") }),
        button("Reset to defaults", { onclick: act("Reset every setting to its default?", "config.reset",
          "Frontends, agent settings and the time zone return to their defaults. Your login, API keys, tools and files are kept.",
          "Reset to defaults", true) }),
      ],
    }));
  return { el };
}
