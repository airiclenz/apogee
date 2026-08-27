// Package judge is apogee's LLM oracle for the halves of a claim no assertion settles: whether a
// pane READS right, whether a tone carries, whether a sentence a human will act on says what it
// meant to (ADR 0062).
//
// Everything a cell can settle is settled by a cell — internal/tuitest owns that, and a rubric
// that asks "is the word Fix: on row 4" is a rubric written in the wrong package. What is left over is judgement, and before this package the only judge available was a
// human reading a numbered step. A judge that answers in `go test` is one that runs on every
// release instead of on the releases somebody had the afternoon for.
//
// # The gate
//
// Judge calls need a model, so they are opt-in exactly as the live tests are: [Enabled] reports
// whether APOGEE_JUDGE_ENDPOINT — or, failing that, APOGEE_LIVE_ENDPOINT — names a server, and
// [Skip] skips the test with the line that says how to turn it on. `make live-eval` sets the gate;
// a plain `go test` does not, so an unset gate is a skip and never a silent pass.
//
// # The verdict is binding
//
// With the gate set, a `fail` FAILS the Go test and prints the model's reasons ([Require]). An
// advisory verdict is a verdict nobody reads. Two consequences follow, and both are deliberate:
// a weak local judge is a reason to sharpen the rubric, never to soften the verdict; and because
// temperature 0 on a local server is not bit-reproducible (sampler seed, batch composition), a
// judge failure is re-run ONCE by hand before it is believed — two fails in a row are a real fail.
// TestJudgeSelfCheck is the kit's own probe of the configured judge, and `make live-eval` lists
// this package first so a broken judge is reported as such rather than as twenty rubric failures.
//
// # The files
//
//   - doc.go — this narration.
//   - judge.go — [Rubric], [Verdict], the gate ([Enabled], [Skip]), the one provider round-trip
//     ([Ask], [Pairwise]), the strict reply parse, and the binding assertion ([Require]).
//   - artifact.go — [Artifact] and its [Kind]s: the named texts a verdict is rendered on, plus
//     [FrameArtifact], which serialises a tuitest frame with its colours named.
//
// Documented in docs/design/test-drivers.md.
package judge
