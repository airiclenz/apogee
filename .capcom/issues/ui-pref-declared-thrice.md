---
title: "A UI pref is declared once, in domain (architecture review 2026-11-10 #8)"
status: parked
type: task
priority: 3
created: 2026-11-10T19:36:32Z
---

Found by docs/reviews/architecture-review-2026-11-10.html, verified 2026-11-10 at a5c4f2db. Verified PARTLY: the sites are as described, but drift between the three default statements is already caught by tests (TestRegistryDefaultsFollowTheStarterTemplate; config_test.go `wantUIDefault`, hand-spelled on purpose), and the starter template's default is hand-written documentation that stays. The gain is fewer sites per new key and losing the hidden Validate invariant, not removing drift.

Problem: adding a ui.* key touches ~11 sites in ~7 files (8528ae81, ui.pointer-shapes): UIPrefs field, UIKey const, `uiKeys` order, `DefaultUIPrefs`, the land arm, the uiConfig yaml field, the registry row and uiBool pair, defaults/config.yaml, domain/doc.go's key count, the settingsApplyLocal arm. The registry's enum validators (internal/config/registry.go ~1847-1871) build `UIPrefs{Field: v}.Validate()`, quietly relying on every other field's zero value passing Validate. The side-panel width's "0 = unset" is special-cased across several files (settingsapply.go, cmd/apogee/settingsedit.go, sidepanel.go, keyfield.go, config.go, registry.go).

Solution: one per-key table in domain (path, parse/format, field, default, structured) from which land, UIKeys, DefaultUIPrefs and a per-key Validate(key, text) derive; the registry's ui.* rows become that table plus Desc/Kind. A SidePanelWidth value type (stated | unset) owns resolve and clamp. Constraints: keyfield.go's rule that the file pass lands an unvalidated typed copy, and tui may not import config. Overlaps #3 (ui-pref-apply-restated).

## Acceptance

- Decided: pursued as a plan or closed as not worth it.
