---
title: ConPTY restricted-token launch for Windows Consoles
status: done
type: task
priority: 3
labels: [windows]
plan: 2026-10-04 - 00 - windows-console-confinement-plan
aliases: [apogee-6ef3]
created: 2026-10-03T18:11:26Z
closed: 2026-10-04T21:18:04Z
reason: "Resolved by plan 'windows-console-confinement-plan' items 2-3 (archived at d5012578): a Windows Console launches under the restricted token and opens Confined."
---

Follow-up to ADR 0059 (Bounds: Windows, amended 2026-10-03) and plan 'docs/plans/2026-10-03 - 00 - table-stakes-and-image-input-plan.md' items 15–16. The ConPTY backend creates the Console process itself and does not take the restricted token, so on Windows a confined console_open fails closed (ErrConfinementUnavailable, demoted to Approval) and each console_send under a confinement box to an unconfined Console is demoted to Approval too. Launch the pseudoconsole child under the restricted token (CreateProcessAsUser with the pseudoconsole attribute, or an equivalent), so a Console on Windows can be Confined and run in Auto without a prompt. The Job Object container already applies. Needs a Windows host to verify.
