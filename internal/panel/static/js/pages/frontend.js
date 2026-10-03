import { h, pageHeader } from "../ui.js";

const stub = { render: () => ({ el: h("div", { class: "page" }, pageHeader("Coming in this plan", "")) }) };
export const terminal = stub;
export const ide = stub;
