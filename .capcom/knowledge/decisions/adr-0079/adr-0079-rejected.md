---
title: ADR 0079 considered and rejected
---

- **A tokenizer.** A real tokenizer per model would replace the `~`; it would also pin apogee to
  a vocabulary table per profile. The measured `prompt_tokens` is the truth and costs one request;
  the estimate is a labelled proxy. Out of scope.
- **Dry-running the Reactions cascade for the idle estimate.** The cascade reads a live Moment; a
  dry-run would need a fake one and would report a number no request ever carried. Measured-only.
- **Pinning tokens in the golden.** The estimator is a divisor; changing it would churn every
  golden without any injected byte having changed. Bytes are what apogee controls.
- **Putting the estimate on `run_started`.** The frame is emitted before the Agent exists; the
  report cannot be taken there. `run_finished` carries it beside `context_files`, which is already
  a post-construction fact.
- **A `--bypass` flag on probe.** Bypass is a session and bench switch; probe reads the
  configured session and, under `--live`, adds the Bypass column itself. One less flag to document.
- **A config budget ceiling or startup warning on the number.** The number must exist and be
  benched before a threshold means anything. A later plan.
- **TUI surfaces (`/usage`, the status line).** Same reason — the bench uses the number first.
