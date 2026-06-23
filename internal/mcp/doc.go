// Package mcp is the Model Context Protocol client, built on the official Go SDK,
// over stdio / SSE / streamable-http. MCP tools are non-forkable external effects
// that Apogee cannot confine, so they gate through Approval even in Auto mode
// (ADR 0004, ADR 0008).
//
// Phase-0 scaffold: no implementation yet (re-verify SDK maturity at Phase 3).
package mcp
