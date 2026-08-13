# The battery's prompt assets — editing one is a `BatteryVersion` bump

Every `.txt` file in this directory is prompt text the capability battery sends to a live model,
and every byte of it is part of the fingerprint's derivation by construction: re-word one and the
battery is asking a different question, so what models are observed to do — and therefore every
capability claim, tier and behavioral signature already recorded under the old wording — is no
longer comparable. Editing, adding or removing a file here therefore requires bumping
`BatteryVersion` (`internal/probe/battery.go:18`), whose constant is homed at
`library.ProbeBatteryVersion` (`internal/library/proberecord.go:36`) so the record resolver can
decide comparability without importing the probe. The files carry no comments and no header of
their own — this README is not embedded, while a comment line inside a `.txt` would be sent to the
model as part of the prompt. `TestBatteryPromptsPinTheFingerprintText` in
`internal/probe/battery_test.go` pins each asset's exact text, so an edit that forgets the bump
fails the suite rather than silently re-spelling models.
