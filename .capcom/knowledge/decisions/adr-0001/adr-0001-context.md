---
title: ADR 0001 context
---

Apogee must be validatable: we need to simulate changes and measure how the agent
behaves against real local models before trusting a Mechanism. The predecessor
`apogee-sim` already owns the instrument for this — RealSandbox, classifier, scorers,
trace archive, and the fork/stepwise counterfactual rig. The open question was *how*
that instrument reaches the agent now that the agent (not a proxy) owns the loop, tool
execution, and conversation state.
