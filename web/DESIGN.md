# Berth panel: design

This file defines how the Berth panel looks and behaves. It binds everyone who
changes `web/`, human or agent. It covers only the UI; what each page does is
in the code under `web/src` and `internal/panel`.

## 1. Visual theme and atmosphere

A calm, dense, trustworthy operator console for one technical person running
their own agent workstation. Information first, decoration last. The panel is
opened briefly and often: every page should answer "is everything fine, and
what can I do here?" at a glance.

- **Direction: Harbor** (cool slate, teal, compact). Cool slate neutrals with a
  single harbour-water teal for actions, links, focus and the current nav
  item. It reads like a well-kept infrastructure dashboard: flat white cards
  on a pale slate page, hairline borders, compact 32 px controls, 13 px body
  text. Hierarchy comes from weight (500/600) and the three text tones, not
  from size jumps or colour. The teal is the only nautical nod in core UI.
- Mood references: well-kept infrastructure dashboards and system consoles,
  as inspiration only. Berth never copies another product's identity, logo,
  typeface or signature colour.
- A berth is where a ship docks. A subtle nod is welcome in the logo and empty
  states, never in core UI or at the cost of clarity. The logo mark is a
  dock outline: a box with no top edge and rounded bottom corners, stroked in
  `--accent` (2.5 px border, 18 × 12 px in the nav, 20 × 13 px on the setup
  and sign-in screens), followed by the word "Berth" at weight 600.

## 2. Colour palette and roles

Defined as CSS custom properties on `:root`, with a dark set applied by
`prefers-color-scheme: dark` and overridden by `data-theme="light|dark"` on
`<html>` when the operator picks a theme manually. Set `color-scheme` to match
in each block so form controls and scrollbars follow.

Contrast is on `--bg` unless stated. Ratios on `--surface` differ by at most
1.3 and cross no threshold; `--line-control` is 3.49 / 3.42 on `--surface`
and 3.08 / 3.09 on `--surface-sunken`.

| Token              | Role                                                                 | Light                | Dark             | Contrast on `--bg` (light / dark)                  |
| ------------------ | -------------------------------------------------------------------- | -------------------- | ---------------- | -------------------------------------------------- |
| `--bg`             | Page background                                                      | `#F5F7F9`            | `#0E1318`        | —                                                  |
| `--surface`        | Cards, nav, dialogs, inputs, toasts                                  | `#FFFFFF`            | `#151C23`        | —                                                  |
| `--surface-sunken` | Hover rows, inline code, segmented-control track, skeletons          | `#EEF1F4`            | `#1C252E`        | —                                                  |
| `--fg`             | Primary text                                                         | `#14191F`            | `#E3E9EE`        | 16.45 / 15.25                                      |
| `--fg-secondary`   | Body copy under titles, table cells, banner text                     | `#3E4852`            | `#B6C1CB`        | 8.68 / 10.20                                       |
| `--fg-muted`       | Captions, hints, column headers, inactive nav                        | `#5A6672`            | `#93A1AD`        | 5.46 / 7.06                                        |
| `--line`           | Card borders, dividers, row rules (decorative)                       | `#DCE1E6`            | `#26313B`        | 1.23 / 1.41                                        |
| `--line-strong`    | Secondary-button outline, save bar and toast border                  | `#C3CCD4`            | `#36434F`        | 1.51 / 1.84                                        |
| `--line-control`   | Input, select and checkbox borders; toggle track when off (derived)  | `#7D8B98`            | `#62717E`        | 3.25 / 3.72                                        |
| `--accent`         | Primary button fill, toggle on, selected outline                     | `#0B7285`            | `#4FC1D1`        | 5.20 / 8.78                                        |
| `--accent-hover`   | Hover on accent fill and links                                       | `#095E6E`            | `#72D0DD`        | 6.89 / 10.47; `--accent-fg` on it 7.40 / 10.27     |
| `--accent-soft`    | Current nav item background                                          | `#E3F1F3`            | `#143039`        | —                                                  |
| `--accent-text`    | Links, current nav item text, quiet buttons                          | `#0A6172`            | `#8FDCE6`        | 6.59 / 12.04 (6.12 / 8.96 on `--accent-soft`)      |
| `--accent-fg`      | Text on `--accent`                                                   | `#FFFFFF`            | `#081719`        | 5.59 / 8.62 on `--accent`                          |
| `--focus`          | Focus ring (= `--accent`)                                            | `#0B7285`            | `#4FC1D1`        | 5.20 / 8.78                                        |
| `--ok`             | Running, succeeded, installed, signed in, set, on                    | `#17703F`            | `#4CC38A`        | 5.71 / 8.43                                        |
| `--ok-soft`        | Badge background for `--ok`                                          | `#E5F3EB`            | `#12291F`        | 5.36 / 6.96 (`--ok` on it)                         |
| `--warn`           | Restarting, starting, update available, locked sign-in, disconnected | `#975600`            | `#F0A64A`        | 5.36 / 9.12                                        |
| `--warn-soft`      | Badge and banner background for `--warn`                             | `#FCF0DC`            | `#2E2210`        | 5.11 / 7.59 (`--warn` on it); `--fg` 15.68 / 12.69 |
| `--bad`            | Failed, errors, destructive confirm                                  | `#C4314B`            | `#FF7B8A`        | 5.03 / 7.51                                        |
| `--bad-soft`       | Badge, error-alert and danger-hover background                       | `#FBE9EC`            | `#33161C`        | 4.62 / 6.64 (`--bad` on it)                        |
| `--bad-fg`         | Text on `--bad` (danger confirm button)                              | `#FFFFFF`            | `#2A0A10`        | 5.40 / 7.35 on `--bad`                             |
| `--info`           | Job in progress, waiting for sign-in approval, restart notes         | `#2F5FB3`            | `#7AA7FF`        | 5.74 / 7.82                                        |
| `--info-soft`      | Running-job indicator and info-note background                       | `#E7EEF9`            | `#17233A`        | 5.28 / 6.58 (`--info` on it)                       |
| `--idle`           | Stopped, disabled, off, not set, signed out, not installed           | `#5A6672`            | `#9AA7B3`        | 5.46 / 7.60                                        |
| `--idle-soft`      | Badge background for `--idle`                                        | `#EDF0F3`            | `#1F2830`        | 5.13 / 6.09 (`--idle` on it)                       |
| `--log-bg`         | Log viewer, terminal placeholder, error detail (dark in both themes) | `#10161C`            | `#0A0F14`        | —                                                  |
| `--log-fg`         | Log text                                                             | `#D3DCE4`            | `#D3DCE4`        | 13.11 / 13.86 on `--log-bg`                        |
| `--log-dim`        | Log timestamps, prompts                                              | `#8494A3`            | `#8494A3`        | 5.85 / 6.18 on `--log-bg`                          |
| `--log-err`        | Error lines in logs                                                  | `#FF8A97`            | `#FF8A97`        | 8.09 / 8.55 on `--log-bg`                          |
| `--thumb`          | Toggle knob (derived from a hard-coded `#fff`)                       | `#FFFFFF`            | `#FFFFFF`        | —                                                  |
| `--scrim`          | Dialog backdrop                                                      | `rgba(14,19,24,.45)` | `rgba(0,0,0,.6)` | —                                                  |

Rules:

- Status is never colour alone: every status shows an icon and a word.
- One accent. Status colours are for status only, never decoration.
- Destructive actions use `--bad` only on the confirming button, not on the
  button that opens the confirmation.
- AA contrast in both themes: 4.5:1 for text, 3:1 for large text, icons and
  UI boundaries.
- `--line` and `--line-strong` are below 3:1 by design and may only separate
  things already identified by other means (text, fill, position). A boundary
  that is the only cue for a control (inputs, selects, toggle-off track) uses
  `--line-control`.
- Errored inputs switch their border to `--bad`; selected cards and options
  switch theirs to `--accent`.
- Disabled controls drop to `opacity: .5` and `cursor: not-allowed`; they
  keep their colours.

## 3. Typography

- No external or embedded web fonts. System stacks only:
  - Sans: `system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", sans-serif`
  - Mono: `ui-monospace, "SF Mono", "Cascadia Mono", "JetBrains Mono", Menlo, Consolas, monospace`
- Mono only for versions, lock hashes, paths, commands, env var names, device
  codes and logs. Never for labels, headings, navigation or buttons.
- Type scale. Base is `--text-sm` at line height 1.45 on `<body>`.

  | Token        | Size  | Line height | Weight                                       | Use                                                               |
  | ------------ | ----- | ----------- | -------------------------------------------- | ----------------------------------------------------------------- |
  | `--text-2xs` | 11 px | 1.3         | 500–600                                      | Nav count badge, version in nav, embed-bar badge only             |
  | `--text-xs`  | 12 px | 1.4         | 400; 500–600 in badges                       | Hints, captions, column headers, status badges, mono values, logs |
  | `--text-sm`  | 13 px | 1.45        | 400; 500 labels and buttons; 600 card titles | Body, controls                                                    |
  | `--text-md`  | 14 px | 1.4         | 600                                          | Card and section headings (`h2` inside cards)                     |
  | `--text-lg`  | 16 px | 1.35        | 600                                          | Wizard step and dialog titles                                     |
  | `--text-xl`  | 20 px | 1.3         | 600, letter-spacing −0.01em                  | Page titles (`h1`)                                                |
  | `--text-2xl` | 24 px | 1.25        | 600                                          | Sign-in and setup-complete screens only                           |

- Three weights only: 400 body, 500 labels, buttons and nav items, 600
  headings, titles, badges and the current nav item.
- Numbers that change in place (uptime, counts, durations) use
  `font-variant-numeric: tabular-nums`.

## 4. Components

One shared set, used everywhere. A new page reuses these before adding
anything.

| Component             | Rules                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| --------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Button                | Primary (accent fill), secondary (outline), quiet (text), danger (confirm only). One primary per view. Busy state keeps the width and shows a spinner and verb ("Updating…")                                                                                                                                                                                                                                                                                                                                                                                          |
| Toggle                | For on/off settings that apply on save. Label on the left, state readable without colour                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| Status badge          | Icon + word: Running, Starting, Restarting, Failed, Stopped, Disabled. Optional detail ("3 restarts") in muted text                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| Card                  | Service cards and page sections share one shell: title row (name, badge, actions), body, optional footer                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| Table                 | Base tools, user tools, presets, version history, agents, MCP servers, skills, jobs. Sticky header, row hover, numeric columns right-aligned, wraps into stacked rows below 600 px. Package, history and skill rows expand in place (a full-width row below) to manage keys, show versions or manage a skill                                                                                                                                                                                                                                                          |
| Form field            | Label above, hint below, error below the hint with `aria-describedby`; errors name the fix, not just the fault                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| Secret field          | Write-only. Unset: password input + Save. Set: "Set ✓" with Replace and Remove; the value is never shown again                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| Dialog                | In-page modal with focus trap, Escape to close, explicit verb on the confirm button ("Roll back", not "OK"). Never browser `confirm()`                                                                                                                                                                                                                                                                                                                                                                                                                                |
| Toast                 | Short confirmations, bottom corner, auto-dismiss 5 s, errors persist until dismissed                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| Stepper               | Setup wizard only: numbered steps, current, done, skippable                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| Log viewer            | Mono, sunken surface, auto-scroll with a Follow toggle, Copy, preserves ANSI-free plain text                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| Sticky save bar       | Appears on unsaved changes: summary of what restarts, Discard, Save                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| Banner                | Page-wide: disconnected, recovery mode, update available. Icon + text + one action                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| Empty state           | One sentence and one action ("No tools added yet · Add tool")                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| Running-job indicator | Visible from every page while a job runs; links to its log                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| Agent picker          | Who gets an MCP server or skill: a compact secondary button labelled with the agents ("All agents", names, or "No installed agent") opens a popover of checkboxes, "All agents" first; ticking every agent means all agents, and the last ticked agent cannot be unticked. An agent reached anyway (a skill through another agent's directory) shows ticked and locked with a muted note; agents that cannot take the item show unticked and locked with a reason. Saves on each change. The add/edit dialog embeds the same checklist. Skills use the Attach picker. |
| Attach picker         | Who gets one skill of a source, in a dialog. "All agents, including ones added later" comes first; unticking it lists today's agents, and ticking every agent keeps that list. An agent another source already gives the skill to shows unticked and locked with "Gets <skill> from <source>", and All agents locks too. An agent reached through another agent's folder shows ticked and locked with "Through <agent>"; agents that cannot take skills are listed locked. The confirm button reads Attach, Save, or Detach once no agent is ticked                   |
| Launch menu           | Starts an agent or a shell in a workspace's tmux session: a compact secondary button "Launch" with a play icon and a chevron opens a menu of the enabled agents by name, a separator, then Shell. Picking one opens the workspace's Terminal tab. Disabled while the workspace's folder is missing                                                                                                                                                                                                                                                                    |
| Segmented control     | Theme picker (Light, Dark, System) and tabs inside a page (Tools: Built-in, Your tools, Presets, each with a count; Skills: By source, All skills; Workspace: Overview, Terminal): equal-width options on a `--surface-sunken` track; the selected tab lives in the URL                                                                                                                                                                                                                                                                                               |

Harbor styling, per component:

- **Common.** Controls are `--control-h` (32 px) tall, `--radius-m`, `font:
inherit`, weight 500. Compact controls inside cards and table rows are
  `--control-h-s` (28 px) at `--text-xs`. Hover changes background only, never
  size or border width. Every interactive element gets
  `:focus-visible { outline: 2px solid var(--focus); outline-offset: 2px }`.
- **Button.** Padding 0 16 px (primary), 0 12 px (secondary and compact).
  Primary: `--accent` fill and border, `--accent-fg` text, hover
  `--accent-hover`. Secondary: `--surface` fill, 1 px `--line-strong` border,
  `--fg` text, hover `--surface-sunken`. Quiet: transparent, no border,
  `--accent-text`, hover `--accent-hover`. Danger (confirm only): `--bad` fill
  and border, `--bad-fg` text. A secondary button that opens a destructive
  confirmation stays secondary. Busy: 12 px spinner (2 px `currentColor` ring
  with a transparent right edge, 0.8 s linear rotation) before the verb.
  Disabled: opacity .5.
- **Toggle.** Track 36 × 20 px, radius 10 px, 2 px inset; knob 16 px circle in
  `--thumb` with `--shadow-1`, slides 16 px with `transition: transform .15s`.
  Track is `--accent` when on and `--line-control` when off. `role="switch"`
  with `aria-checked`; the word (On/Off) sits to the left of the track in
  `--fg-secondary`, weight 500.
- **Status badge.** Inline-flex, gap 5 px, padding 2 × 8 px, `--radius-s`,
  `--text-xs` weight 600, colour `--{status}` on `--{status}-soft`, 12 px icon.
  Badges in the slim embed bar use `--text-2xs` and padding 1 × 7 px. The
  nav count badge (failures) is text only in `--bad` with an 11 px icon, no
  background.
- **Status icons.** 16 × 16 viewBox, stroke 2, round caps and joins, drawn in
  `currentColor`. Circle `M8 1.5a6.5 6.5 0 1 0 0 13a6.5 6.5 0 1 0 0-13z` plus:

  | Status                                            | Token    | Inner path                                                   |
  | ------------------------------------------------- | -------- | ------------------------------------------------------------ |
  | Running, Succeeded, Installed, Signed in, Set, On | `--ok`   | check `M5 8.2l2 2 4-4.4`                                     |
  | Restarting, Starting (derived)                    | `--warn` | no circle; arrow `M13.5 8a5.5 5.5 0 1 1-1.6-3.9M13.5 2v3h-3` |
  | Update available                                  | `--warn` | up arrow `M8 11V5.5M5.8 7.5L8 5.3l2.2 2.2`                   |
  | In progress                                       | `--info` | arrow as Restarting, no circle                               |
  | Waiting                                           | `--info` | clock `M8 4.5V8l2.2 1.4`                                     |
  | Failed                                            | `--bad`  | cross `M5.5 5.5l5 5M10.5 5.5l-5 5`                           |
  | Stopped                                           | `--idle` | square `M6 6h4v4H6z`                                         |
  | Disabled, Off, Not set, Signed out, Not installed | `--idle` | dash `M5 8h6`                                                |

- **Card.** `--surface`, 1 px `--line`, `--radius-l`, no shadow. Padding
  `--space-4` (16 px); title row and footer separated by 1 px `--line` when
  the card has sections, with 12 × 16 px padding. Title `--text-md` 600 or
  `--text-sm` 600 in dense grids. A card in a failed state may switch its
  border to `--bad`; its badge still carries the meaning.
- **Table.** `role="table"` grid or `<table>` inside a card with
  `overflow: hidden` and an `overflow-x: auto` wrapper, edge to edge: the
  card body adds no padding around a table. Header row: `--text-xs`
  weight 500 `--fg-muted`, padding 8 × 12 px (16 px on outer cells), 1 px
  `--line` bottom. Body rows: same padding, 1 px `--line` rules, hover
  `--surface-sunken`. The primary cell is a quiet link (`--accent-text` 600)
  with mono detail below in `--fg-muted`.
  The Skills table has two views, by source (the default) and all skills
  by name (`?view=flat`). By source, skills sharing a repository and ref
  sit under a header row on `--surface-sunken`: a collapse chevron, the
  repository in mono 600 over the count, the ref and short commit (or a
  `--warn` "On n commits" with Realign when they differ), who uses them
  ("Varies by skill" when they differ), one switch for the group ("n of m"
  when mixed) and Manage. Its rows indent 40 px and show only their commit.
  A single skill, and every skill in the flat view, has no header row and
  shows its repository and ref. "Available to" is a plain list of names,
  `--warn` when no agent gets the skill; a switched-off skill's name is at
  70 % opacity. Manage expands a full-width row: for a skill, its location
  (`~/.local/share/berth/skills/<id>`), the Available to checklist, then
  Update, Change ref and Remove skill; for a group, Available to for all
  of them with "n of m" on partial agents, then Update all (Realign),
  Change ref, Add more from this source and Remove all. Change ref is a
  small dialog; from a grouped skill it offers "Apply to all n skills",
  ticked in the flat view. In the flat view, Update on a grouped skill asks
  whether to update its whole group.
- **Agent icons.** Before the name in the Agents table only, centred on the
  name and package lines: a 28 px frame (`--surface`, 1 px `--line`,
  `--radius-m`) holding a 20 px mark. A lock
  `icon` slug draws a bundled LobeHub Icons mark in its own colours, the one
  exception to the single-accent rule: Claude Code's terracotta, the Gemini
  CLI and Codex tiles with their gradients. OpenCode's mark has no colour
  version and draws in `currentColor`. If the installed set has no `icon`
  metadata, the four built-in agents are recognized by package name.
  Unknown agents get a 28 px monogram tile in place of the frame: the
  name's first letter, `--text-xs` weight 600, on a soft tint (`--accent-soft`, `--info-soft`, `--ok-soft`, `--warn-soft`,
  `--idle-soft`) picked by a stable hash of the package id. Always
  `aria-hidden`; the name sits beside it. MCP servers, "Available to"
  checklists and sign-in dialog titles show names without brand icons.
- **Sign-in column** (Agents table, between Keys and Enabled). One line per
  method: the method label when an agent has several, a small Signed in
  (with the account) / Signed out badge, then compact buttons: Sign in, or
  a ghost Sign in again and an outline Sign out (which confirms). A failed
  probe shows a warn "Unknown" badge with its message below in `--text-xs`
  `--fg-muted`; a switched-off agent shows "—".
- **Sign-in dialog.** Title: "Sign in to {agent} · {method}".
  Starting: spinner and "Starting the sign-in…". Device: the device code
  box. Paste: an "Open {host}" link button, a Code field (no autocomplete,
  no spellcheck) and Submit code; "Checking the code…" after a submit; a
  rejected code either asks again ("…open the sign-in page again; it has
  changed") or ends with an error and Start again. Closing it while it runs
  cancels the sign-in. A sign-in running in another tab reopens here.
- **Form field.** Column, gap 6 px. Label weight 500. Input/select:
  `--control-h`, padding 0 10 px, `--surface`, 1 px `--line-control`,
  `--radius-m`. Hint `--text-xs` `--fg-muted`. Error `--text-xs` `--bad` with
  a 12 px alert icon (circle + `M8 4.8v3.8M8 10.8v.4`), and `aria-invalid`.
- **Secret field.** Set state: `--ok` "Set" badge, then compact secondary
  Replace and Remove buttons (28 px). Remove opens a dialog; only that
  dialog's confirm is danger.
- **Dialog.** Fixed scrim (`--scrim`) with 16 px padding, centred panel:
  `--surface`, 1 px `--line`, `--radius-l`, `--shadow-2`, max width 520–580 px
  by content, max height `calc(100vh - 32px)` with an internal scroll.
  Header, body and footer padded 16–20 px, separated by 1 px `--line`; footer
  buttons right-aligned, confirm last. Clicking the scrim closes it unless a
  job is starting. Focus moves to the first field or the confirm button.
- **Toast.** Fixed, bottom right, 16 px from the edges, stacked with 8 px gap,
  width `min(360px, 100vw - 32px)`. `--surface`, 1 px `--line-strong`,
  `--radius-l`, `--shadow-2`, padding 10 × 12 px. 16 px status icon in the
  status colour, message weight 500, 22 px quiet dismiss button. Container is
  `aria-live="polite"`; each toast `role="status"`.
- **Stepper.** Ordered list. 24 px circles, 1.5 px border: done and current
  use `--accent` (done filled, with a check; current outlined, `aria-current`);
  upcoming use `--line-control` with `--fg-muted` number. Label `--text-xs`,
  600 on the current step. Connectors are 1.5 px lines, `--accent` once
  passed. "Step n of N" sits right of the wizard title in `--fg-muted`.
- **Log viewer.** `--log-bg` in both themes, `--log-fg` text, mono
  `--text-xs` at line height 1.6, padding 10 × 12 px, `white-space: pre`,
  `overflow-x: auto`, `role="log"`. Timestamps in `--log-dim`, error lines in
  `--log-err`. The job log page frames it in a card with a toolbar (Follow
  toggle, Copy) on `--surface`.
- **Sticky save bar.** `position: sticky; bottom: 16px`, `role="region"`
  labelled "Unsaved changes". `--surface`, 1 px `--line-strong`, `--radius-l`,
  `--shadow-2`, padding 10 × 12 × 10 × 16 px. "Unsaved changes" weight 600,
  restart summary in `--fg-secondary`, then Discard (secondary) and Save
  (primary). Wraps on narrow screens.
- **Banner.** Full width above page content, padding 10 px ×
  `clamp(16px, 3vw, 32px)`, `--{status}-soft` background, 1 px `--line`
  bottom. 16 px icon stroked in the status colour, a title word in the status
  colour weight 600, message in `--fg-secondary`, one compact secondary
  action. `role="alert"` for disconnected and recovery; `role="status"`
  otherwise.
- **Inline alert** (form-level error, locked sign-in, restart note).
  Padding 8–10 × 12 px, `--radius-m`, `--{status}-soft`, 14–16 px icon.
- **Empty state.** Centred in its card, `--fg-secondary` sentence, one
  secondary or primary button. Loading uses `--surface-sunken` skeleton blocks
  at the final size with `aria-busy="true"`; no shimmer.
- **Running-job indicator.** Bottom of the nav (top of the mobile menu): a
  button with `--info-soft` fill, 1 px `--line`, `--radius-m`, padding 10 px.
  Spinner and label in `--info` 600, a 4 px progress bar (`--line` track,
  `--info` fill, `transition: width .3s`), then "n of m steps · View log" in
  `--text-xs` `--fg-secondary`.
- **Segmented control.** Track `--surface-sunken`, 2 px padding, `--radius-m`,
  2 px gap. Options 26 px tall (32 px in the mobile menu), `--radius-s`,
  `--text-xs` 500, `--fg-muted`; the pressed option is `--surface` with
  `--fg` and `--shadow-1`, `aria-pressed="true"`.
- **Icons.** Inline SVG, 16 × 16 viewBox, `stroke="currentColor"`,
  `fill="none"`, round caps. Nav and toolbar icons render at 16 px, stroke 1.5.
  Inline action icons render at 13–14 px, stroke 1.6. Status icons render at
  11–12 px, stroke 2. Decorative icons carry `aria-hidden="true"`. Agent
  brand marks are filled 24 × 24 artwork; see Agent icons.

## 5. Layout

- Left navigation (Overview, Workspaces, Terminal, IDE, Agents, MCP servers,
  Skills, Tools, Accounts, Settings, Activity), collapsing into a menu button
  below 900 px.
- Terminal and IDE pages are full-height embeds under a slim top bar.
- A workspace's Terminal tab embeds the same terminal on the workspace's
  session, in a `--log-bg` frame 70 % of the viewport tall (at least 320 px),
  with the session's windows above it.
- Content column max width for settings-style pages; overview and tables may
  use the full width.
- Tokens:

  | Token           | Value   | Use                                                                                                      |
  | --------------- | ------- | -------------------------------------------------------------------------------------------------------- |
  | `--space-1`     | 4 px    | Icon-to-text in badges, label-to-value                                                                   |
  | `--space-2`     | 8 px    | Button groups, list gaps, toast stack                                                                    |
  | `--space-3`     | 12 px   | Card grid gap, row padding                                                                               |
  | `--space-4`     | 16 px   | Card padding, nav padding, mobile page gutter                                                            |
  | `--space-5`     | 24 px   | Section gaps inside a page (derived from 20 px in the prototype)                                         |
  | `--space-6`     | 32 px   | Desktop page gutter                                                                                      |
  | `--space-7`     | 40 px   | Minimum touch target (derived)                                                                           |
  | `--space-8`     | 56 px   | Top offset on the setup and sign-in screens (derived)                                                    |
  | `--radius-s`    | 4 px    | Badges, segmented options, small icon buttons                                                            |
  | `--radius-m`    | 6 px    | Buttons, inputs, nav items, inline alerts, code                                                          |
  | `--radius-l`    | 10 px   | Cards, dialogs, toasts, save bar                                                                         |
  | `--control-h`   | 32 px   | Buttons, inputs, selects                                                                                 |
  | `--control-h-s` | 28 px   | Compact buttons in cards, rows and banners                                                               |
  | `--nav-w`       | 224 px  | Desktop navigation                                                                                       |
  | `--nav-item-h`  | 34 px   | Nav items (40 px in the mobile menu)                                                                     |
  | `--topbar-h`    | 52 px   | Mobile header (`--surface`, 1 px `--line` bottom, sticky)                                                |
  | `--embed-bar-h` | 44 px   | Terminal and IDE top bar                                                                                 |
  | `--content-max` | 1120 px | Page column, centred                                                                                     |
  | `--form-max`    | 680 px  | Settings-style forms and long text inside a page                                                         |
  | `--card-min`    | 260 px  | `grid-template-columns: repeat(auto-fit, minmax(var(--card-min), 1fr))`; 200–240 px for small stat cards |

- Page padding is `clamp(16px, 3vw, 32px)`; page sections stack with a 20 px
  gap. Page header: `h1` with a one-line `--fg-muted` description below it,
  actions on the right, wrapping under the title when narrow.
- Navigation: `--surface`, 1 px `--line` right border, sticky full height,
  padding 16 × 10 px. Order: logo row (with the version in mono at the right),
  nav items, then pinned to the bottom the running-job indicator, theme
  segmented control and the user row with Sign out. Nav item: 16 px icon, gap
  10 px, `--radius-m`, `--fg-muted` weight 500. Current: `--accent-soft`
  background, `--accent-text` 600, `aria-current="page"`. Hover:
  `--surface-sunken`, `--fg`.
- Below 900 px the nav becomes a sticky top bar with a 36 px menu button
  (`aria-expanded`, `aria-controls`); the menu opens beneath it as a full-width
  list.
- Setup and sign-in: centred single column, card max width 560 px (sign-in
  380–460 px), logo above, top padding `clamp(20px, 6vh, 56px)`.

## 6. Depth and elevation

- Elevation is functional: dialogs and menus float; cards sit on the page.
  Do not stack shadows to decorate.
- Harbor is flat: cards, nav and tables use borders, never shadows.

  | Token        | Light                                                          | Dark                                                   | Use                                     |
  | ------------ | -------------------------------------------------------------- | ------------------------------------------------------ | --------------------------------------- |
  | `--shadow-1` | `0 1px 2px rgba(0,0,0,.12)`                                    | `0 1px 2px rgba(0,0,0,.3)` (derived)                   | Toggle knob, pressed segmented option   |
  | `--shadow-2` | `0 10px 30px rgba(16,24,32,.16), 0 1px 3px rgba(16,24,32,.08)` | `0 10px 30px rgba(0,0,0,.5), 0 1px 3px rgba(0,0,0,.4)` | Dialogs, menus, toasts, sticky save bar |

- Stacking order: sticky mobile header 20, save bar 10, dialog 60, toasts 70.

## 7. Do's and don'ts

Do:

- Show what a change will restart before it is saved.
- Say what happened and what to do next in every error.
- Keep every action reachable by keyboard, with a visible focus ring.
- Respect `prefers-reduced-motion`: no motion beyond opacity fades.

Don't:

- A cream or off-white background, serif display headings, italic accent
  words, numbered "01 / 02 / 03" section labels, pill-shaped buttons
  everywhere, glassmorphism, purple-to-blue gradients, oversized hero sections.
- Load anything from outside the binary: fonts, icons, scripts, images.
  Icons are inline SVG.
- Display a secret after it has been saved.
- Use colour alone to carry meaning.
- Use teal for anything that is not an action, link, selection or focus; in
  particular not for status, charts or decorative borders.
- Use `--line` or `--line-strong` as the only boundary of an input or toggle.
- Put a shadow on a card, the nav or a table, or use a coloured left-border
  stripe on cards or alerts.
- Use the soft status backgrounds for anything but badges, banners and inline
  alerts.
- Hard-code `#fff`, terminal colours or any hex in markup or JS; the
  prototype did, the port must use tokens.
- Animate the spinner or progress bar when `prefers-reduced-motion: reduce`;
  show the static state instead.

## 8. Responsive behaviour

- Works from 400 px phone width to wide desktop; nothing scrolls horizontally
  except tables, logs and code, each in its own scroll container.
- Below 900 px: navigation collapses. Below 600 px: tables stack, card grids
  become one column, the sticky save bar spans the full width.
- Touch targets at least 40 × 40 px. Under `(pointer: coarse)` controls grow
  to 40 px; on fine pointers the 32 px and 28 px controls keep a 40 px hit
  area where they stand alone (toggle, icon buttons), extended with padding or
  a transparent `::before`.

## 9. Agent prompt guide

When changing the panel UI:

1. Reuse a component from §4 before creating one; add any new component to §4
   in the same change.
2. Use tokens from §2, §3, §5 and §6; never hard-code a colour, size or shadow.
3. Check both themes, 400 px width, keyboard-only use and AA contrast.
4. Keep the stack: `web/` (React, shadcn `base-nova`, free ReUI items, Tailwind). Tokens live in `web/src/styles/tokens.css` (hex only there); `theme.css` maps Tailwind and shadcn onto them, with shadcn's `primary` as `--accent` and its `accent` as `--surface-sunken`. Build components from `@/components` (`PanelCard`, `StatusBadge`, `InlineAlert`, `FormField`, `BusyButton`, `useConfirm`, `SecretField`, `LogViewer`, `SaveBar`) before adding new ones. No `<style>` injection, no external requests.

Direction prompt:

> Berth uses the Harbor direction: a compact, flat operator console in cool
> slate with one teal accent. Pages sit on `--bg`; content lives in white
> (`--surface`) cards with a 1 px `--line` border and 10 px radius, never a
> shadow. Body text is 13 px system sans at 1.45; page titles 20 px/600, card
> titles 14 px/600, captions 12 px in `--fg-muted`. Controls are 32 px tall
> with 6 px radius; compact ones in rows are 28 px. Teal (`--accent`) marks
> the one primary action per view, links, selections, the current nav item
> (on `--accent-soft`) and focus; nothing else. Status uses a soft-tinted
> badge with a 12 px stroked icon and a word, in green/amber/red/blue/slate.
> Logs and terminal output are always dark (`--log-bg`) in both themes. Floating
> layers (dialogs, toasts, save bar) are the only things with shadows. Prefer
> density over whitespace, weight over size, words over colour.
