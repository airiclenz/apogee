# Probe tool-call-format suggestion — plan

**Goal:** `apogee probe model` suggests a `tool-call-format:` only when the battery saw that format
carry the model's call, and otherwise says no listed format fits and quotes the call the model
wrote. The probe never suggests `custom-regex` or a `tool-call-example:`; the manual says so.

**Date:** 2026-10-08
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 8fdb9c7b

**Regression check (2026-10-08, 8fdb9c7b):**
- 1: guard folded (writer's decisions: trial request routed by system message to its own script field; fifth Report case for a native probe that never completed, pinned in TestModelReportIncompleteBattery); names the superseded `SuggestProfile` comment (internal/probe/modelfingerprint.go:131-135).
- 2: guard folded (Acceptance split to one package per line, `-run 'Doc|Manual'` on probe and `-run 'Doc|Manual|Readme'` on tools; drift grep widened to cmd/apogee, internal/probe, CONTEXT.md).

**Sources:**
- bd `apogee-probe-suggests-unparseable-format`, bd `apogee-probe-suggests-tool-call-example`
- `docs/skill-runs/probe-model/2026-10-08/results.md` (model 3: pythonic `probe_echo(text="apogee")`)
- ADR 0021 (probe is evidence; config is the user's), `internal/floor/salvage.go` (salvage shapes)
- `docs/manual/probe.md`, `docs/manual/configuration.md` (`tool-call-example:`)

**Ratified design calls** (owner, 2026-10-08):
- **Fenced check:** salvage guard recovers the call → suggest `native` (the guard runs it only under a native parser, `agent.salvageToolCall`); else one extra trial request taught the markdown-fenced format → suggest `markdown-fenced` only if its reply parses to `probe_echo`; else suggest `native` and say no listed format fits, quoting the call.
  This supersedes the `SuggestProfile` comment at internal/probe/modelfingerprint.go ("the format small models reach for unprompted").
- **Example key:** the probe never suggests `custom-regex` or `tool-call-example:`; the quoted call shape is the user's material for both, and the manual says so. Non-JSON shapes are never converted. bd `apogee-probe-suggests-tool-call-example` closes on this call.
- **Fingerprint:** the trial is not a Capability — it never enters `Features`, `Complete`, the fingerprint or the record, and `BatteryVersion` does not change.

**Standing requirements:**
- skills: coding-standards
- This box: tests only as `GOMEMLIMIT=2GiB go test -race -count=1 -run <Test> ./internal/probe/` (one package, never a pattern; never two runs at once).

**Out of scope:**
- Converting pythonic kwargs to JSON; synthesising a `tool-call-pattern:`.
- Reading the user's `model-profiles:` from the probe.
- Storing reply text in the probe record.

## 1. Suggest a tool-call format only on evidence; quote the call otherwise

**What:**
**Goal:** `SuggestProfile` returns `markdown-fenced` only when a markdown-fenced trial reply parsed to
`probe_echo`, and `native` in every other case; `Model.Report()` carries a `tool-call format` line
whenever the native probe observed no native call (including a native probe that never completed),
stating which case applied and — when no listed format fits or the trial never completed — quoting
the native probe's reply. Fixes bd
`apogee-probe-suggests-unparseable-format`: today markdown-fenced is suggested for a model whose
written call (`probe_echo(text="apogee")`) it cannot parse.

**Approach (assumed at the header base):**
- `Battery` gains the native probe's visible reply (`ToolReply string`) and a `FencedTrial`
  observation (`Ran`, `Parsed bool`, `Failure string`). `RunBattery` runs the trial only when the
  native probe completed, observed no native call, and `salvageableCallName` did not fire.
- Trial request: system = `batterySystemPrompt` + blank line + `processing.InstructionsFor` for a
  `domain.ModelProfile{ToolCallFormat: domain.FormatMarkdownFenced}` over a one-tool menu built from
  `echoTool`; user = the native probe's ask; no wire `Tools`. Parse with the parser
  `processing.ParserFor` builds for that same profile (what is taught and what is parsed stay one
  seam). `Parsed` iff the call's tool is `echoTool.Name`. Existing prompts are untouched
  (`TestBatteryPromptsPinTheFingerprintText`).
- `SuggestProfile`: `markdown-fenced` iff `FencedTrial.Parsed`; else `native`. It needs to know
  whether salvage fired — carry that on `Battery` (e.g. `Salvaged bool`), set where the native
  probe already calls `salvageableCallName`.
- `Report()` renders, just above the suggested-profile heading, `field("tool-call format", …)`:
  salvaged → `native — the model wrote its call as JSON in the reply, and the salvage guard runs
  that under native`; parsed → `markdown-fenced — taught that format, the model wrote a probe_echo
  call it parses`; trial failed → `native — the markdown-fenced trial never completed (<Failure>),
  so no text format was confirmed; the model wrote: <quote>`; no fit → `native — no listed format
  parses this model's call; the model wrote: <quote> — a custom-regex profile (tool-call-pattern:,
  tool-call-example:) written from that shape is yours to add`. No line when a native call was seen.
- `<quote>`: `ToolReply` whitespace-collapsed to one line, cut at 120 runes on a rune boundary with
  `…`; empty reads `nothing (the reply was empty)`. Escape-stripping stays at the cmd render seam.

**Regression guard.** When the native probe's Finding.Failure != "", Report renders `native — the native probe never completed, so no tool-call format was tested` (no quote, no trial request sent); pin it in TestModelReportIncompleteBattery.
the scripted batteryServer routes the markdown-fenced trial request by its system message (the taught markdown-fenced instructions) to a new script field, so every new case scripts the trial reply explicitly; the existing fake upstreams in internal/probe/battery_test.go and cmd/apogee/probemodel_test.go stay valid unchanged, because their default branch never parses to probe_echo
Supersedes the `SuggestProfile` comment at internal/probe/modelfingerprint.go:131-135 ("the format small models reach for unprompted") per the header's ratified Fenced check call — rewrite it with the rule.

**Files:** internal/probe/battery.go; internal/probe/modelfingerprint.go; internal/probe/model.go; internal/probe/battery_test.go; internal/probe/model_test.go
**Read first:** internal/probe/battery.go — RunBattery, probeNativeToolCall, salvageableCallName, Battery, echoTool; internal/probe/modelfingerprint.go — SuggestProfile; internal/probe/model.go — Report, findingLines;
internal/processing/instructions.go — InstructionsFor; internal/processing/parserfor.go — ParserFor; internal/probe/battery_test.go — batteryServer, script.toolReply, runBatteryRecording, TestBatteryNoNativeTools;
internal/probe/model_test.go — gatherModel, TestModelReportIncompleteBattery; cmd/apogee/probemodel_test.go — modelUpstreamPlaceholderToolCalls

**Tests:** extend the scripted `batteryServer`/`script` with a new field that answers the trial request, routed by its system message (the taught markdown-fenced instructions); every new case scripts the trial reply explicitly, and the existing fake upstreams stay unchanged. Cases: native
pass → no trial request sent, no line; salvageable JSON → no trial, `native`, salvage line; pythonic
reply + trial reply in the fenced format → `markdown-fenced`, parsed line; pythonic reply + trial
reply pythonic → `native`, no-fit line quoting `probe_echo(text="apogee")`; trial transport error →
`native`, never-completed line; native probe never completed (`script{fail: true}`) → no trial
request, `native — the native probe never completed, so no tool-call format was tested`, pinned in
`TestModelReportIncompleteBattery`; trial never alters `Features()` / `Fingerprint`. Update
`TestBatteryNoNativeTools` (it pins `markdown-fenced` on a reply with no evidence).

**Acceptance:**
- `go build ./... && go vet ./internal/probe/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestBattery|TestModelReport|TestFingerprint' ./internal/probe/`

**Closes:** apogee-probe-suggests-unparseable-format

**Commit:** `fix(probe): suggest a tool-call format only when the battery saw it carry the call`

## 2. State the suggestion rule in the manual

**What:**
**Goal:** the user manual states how `apogee probe model` picks the suggested `tool-call-format:`
(native call, salvage, the markdown-fenced trial and its one extra request, the no-fit quote), and
that it never suggests `custom-regex` or `tool-call-example:` — the quoted call is the material for
writing both. Depends on item 1.

**Approach (assumed at the header base):** amend the `apogee probe model` paragraph of
`docs/manual/probe.md` (it names a "three-part capability battery"; the trial is an extra request,
not a fourth capability) and the sentence after the `model-profiles:` example in
`docs/manual/configuration.md` ("`apogee probe model` prints the entry its findings suggest").
Rule for every other site: any doc or package-comment sentence that says what format the probe
suggests or how many requests the battery spends — find with
`grep -rn "markdown-fenced\|SuggestProfile\|probe model" docs/manual internal/probe/doc.go README.md`.
One `CHANGELOG.md` `[Unreleased]` Fixed entry for both beads (via the item sidecar).

**Regression guard.** Acceptance runs one package per line (plan Standing requirements). The rule's grep is
`grep -rn "markdown-fenced\|SuggestProfile\|probe model\|differently.shaped" docs/manual internal/probe cmd/apogee README.md CONTEXT.md`
— it catches cmd/apogee/probemodel_test.go:770 ("the battery asks five differently-shaped questions"), false once the trial adds a sixth shape.

**Files:** docs/manual/probe.md; docs/manual/configuration.md; internal/probe/doc.go; cmd/apogee/probemodel_test.go
**Read first:** docs/manual/probe.md — `apogee probe model` paragraph ("three-part capability battery"); docs/manual/configuration.md — sentence after the model-profiles: example; internal/probe/doc.go — model-half file map;
cmd/apogee/probemodel_test.go — modelUpstreamPlaceholderToolCalls comment; internal/probe/docmap_test.go — TestDocMapNamesEveryFile; internal/tools/manual_drift_test.go — TestManualListsEveryKnownToolName

**Tests:** none beyond the existing doc-drift tests.

**Acceptance:**
- `GOMEMLIMIT=2GiB go test -count=1 -run 'Doc|Manual' ./internal/probe/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'Doc|Manual|Readme' ./internal/tools/`
- `grep -n "custom-regex" docs/manual/probe.md` names the never-suggested rule.

**Closes:** apogee-probe-suggests-tool-call-example

**Commit:** `docs(probe): state how probe model picks the suggested tool-call format`
