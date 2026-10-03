import { h, pageHeader } from "../ui.js";

export function render() {
  return { el: h("div", { class: "page" }, pageHeader("Coming in this plan", "")) };
}
