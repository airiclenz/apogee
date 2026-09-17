package domain

// The Context cost REPORT (ADR 0079) — what apogee itself puts in front of the model at Turn 1
// before the user's first message: the standing system content, piece by piece, and the tool
// menu. It is a read-only view the engine composes from the very renders it seeds a request
// from and a Driver surfaces (the probe, the headless frames, the bench); nothing here reaches
// the model. The types live in domain because both sides of that seam speak them (ADR 0010).

// ContextCostRow is one piece of the Turn-1 context: its name, the bytes apogee sends for it
// and the token estimate those bytes come to. Names are the standing system message's row
// names in wire order — prompt, orientation, delegate report, task list, context files — then
// the tool surface: `tool menu` (the native tools array, measured PromptChars-style as every
// ToolDef's name, description and schema) or, on a non-native profile, `tool instructions`
// (the rendered text menu + emission instructions the wire seam folds into the system channel
// in the array's place). A piece that renders nothing has no row.
type ContextCostRow struct {
	Name   string // the piece, by its standing-block or tool-surface name
	Bytes  int    // the bytes apogee sends for it
	Tokens int    // the estimate over Bytes through the session's chars→token ratio
}

// ContextCost is the whole Turn-1 report: Rows in wire order, the total Bytes across them, and
// Tokens — the estimate over that total through the same ratio (one estimate over the sum, not
// the sum of the per-row estimates, which each round up). Calibrated says whether that ratio
// has been folded toward a server's own token count (an idle Agent that has not sent a request
// reports the default ratio, so a Driver labels its numbers as an estimate). Every field is
// filled by the producer; the value carries no arithmetic of its own.
type ContextCost struct {
	Rows       []ContextCostRow
	Bytes      int
	Tokens     int
	Calibrated bool
}
