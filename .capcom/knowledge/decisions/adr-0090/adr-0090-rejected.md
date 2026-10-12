---
title: ADR 0090 considered options
---

- **Keep painting the item runs inline under the block.** It needs no new surface, but it is the
  interleaving this decision exists to end.
- **One row per item in the conversation, with no stage level.** The reader would reach an item in
  one press, but a forty-item fan-out stage would put forty rows in the conversation. The stage is
  the unit the Recipe names and the reader launched, so it is the unit the conversation shows.
- **A stage view for every stage, one-item stages included.** It is one shape instead of two, but
  it costs a press to reach the only run there is. The one-item stage opens its run, and its crumb
  still names the stage.
- **Show a stage only once it starts.** The block would grow as it ran, and the reader could not see
  what was still to come. Every stage shows from the start and reads `pending` until it starts.
