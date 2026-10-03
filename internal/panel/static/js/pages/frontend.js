// Terminal and IDE: full-height embeds of the proxied frontends (§5).
import { h, clear, badge } from "../ui.js";
import { icon } from "../icons.js";

function frontend(service, label, path) {
  return {
    render(ctx) {
      const stateSlot = h("span");
      const body = h("div", { class: "embed-body" });
      const frame = h("iframe", { class: "embed-frame", title: label, src: path });
      const el = h("div", { class: "embed" },
        h("div", { class: "embed-bar" },
          h("a", { class: "btn quiet compact", href: "#overview" }, icon("back", 13, 1.6), "Panel"),
          h("strong", { text: label }), stateSlot, h("span", { class: "spacer" }),
          h("a", { class: "btn secondary compact", href: path, target: "_blank", rel: "noopener" },
            icon("external", 13, 1.6), "Open in new tab")),
        body);
      let shown = "";
      const off = ctx.onStatus((st) => {
        const svc = (st.services || []).find((s) => s.name === service);
        clear(stateSlot, badge(svc ? svc.state : "disabled", null, true));
        const want = svc ? "frame" : "off";
        if (want === shown) return;
        shown = want;
        clear(body, svc ? frame : h("div", { class: "embed-off" }, h("div", { class: "card" },
          h("div", { class: "card-body" },
            h("h2", { text: "The " + label + " is off" }),
            h("p", { class: "muted", text: "Turn it on in Settings. Only the " + label + " starts; nothing else restarts." }),
            h("a", { class: "btn primary", href: "#settings" }, "Open Settings")))));
      });
      return { el, dispose: off };
    },
  };
}

export const terminal = frontend("ttyd", "Terminal", "/terminal/");
export const ide = frontend("code_server", "IDE", "/ide/");
