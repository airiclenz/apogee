// Package tools is the ~30-tool suite plus the registry and executor that sit
// behind the public Tool interface — an open extension point (ADR 0002). Tools
// are stateless across Turns: their only durable side effect is filesystem
// writes, and nothing live is held across the quiescent boundary (ADR 0008).
//
// Phase-0 scaffold: no implementation yet.
package tools
