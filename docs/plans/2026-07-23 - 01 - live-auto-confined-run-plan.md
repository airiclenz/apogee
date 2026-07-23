# Runbook — Live Auto-confined deliverable run (Linux / landlock)

Date: 2026-07-23. This is the owner-run manual verification the CHANGELOG tracks under
**"Known post-release verification (owner-run / CI)" → "Live Auto-confined deliverable
run"**. It is a real coding conversation in `--mode auto` during which you observe three
behaviors:

- **(i)** a shell write **outside** the workspace is OS-denied by landlock, while an
  in-workspace write succeeds — both run **without** an approval prompt (that is Auto);
- **(ii)** an **MCP tool** still raises **Approval** (Auto fences fs-write but gates the
  unfenceable surface);
- **(iii)** a **sub-agent** is delegated and its nested work renders in the transcript.

Only after observing all three, flip the CHANGELOG bullet's **Linux arm** to ✅.

## One-time setup (before launching)

1. **MCP server.** Auto needs at least one MCP tool to gate. The llama-launcher MCP
   endpoint (`http://192.168.64.1:7331/mcp`) **cannot** be used: http-transported MCP
   servers pass the SSRF floor, which refuses private-range endpoints by design (no config
   override — `security.URLGuard`'s floor is tighten-only). A **stdio** server is the
   supported route. A dependency-free one is already in place and handshake-tested:
   `~/.apogee/mcp-ping-server.py` — it offers a single harmless `ping` tool (echoes
   `pong: <text>`).

2. **Config.** Uncomment/add this block in `~/.apogee/config.yaml` (remove it again after
   the run if you don't want the tool offered in every session):

   ```yaml
   mcp-servers:
     - name: demo
       transport: stdio
       command: python3
       args: ["/home/airic/.apogee/mcp-ping-server.py"]
   ```

   The tool will be offered to the model as **`demo__ping`**.

3. **Temp workspace.**

   ```sh
   mkdir -p ~/auto-run-ws && cd ~/auto-run-ws
   /home/airic/Repos/apogee/apogee --mode auto
   ```

   (Endpoint `http://192.168.64.1:1111` comes from the config file.) At startup, confirm
   the notice says filesystem confinement is **active** (landlock backend) and that the
   `demo` MCP server connected with 1 tool. If it instead warns that confinement is
   unavailable and Auto will ask, stop — something changed since `apogee probe` last
   reported landlock available.

## The prompt — paste as ONE message

Written for a small model (gemma-4-E4B): numbered, one tool per step, expected outcomes
stated inline so it doesn't fight the denial in step 1.

```text
We are testing this workspace setup. Follow the numbered steps exactly, in order, one
step at a time. After each step, report its result in one short sentence, then go on to
the next step.

1. Use the terminal tool to run exactly this command:
   echo escape-test > /home/airic/apogee-escape-test.txt
   This command is EXPECTED TO FAIL with a permission error. That failure is the correct
   result. Do not retry it and do not try a different location. Report the exact error
   message you got.

2. Use the terminal tool to run exactly this command:
   echo hello-from-auto > inside.txt && cat inside.txt
   This should succeed and print hello-from-auto.

3. Call the demo__ping tool with the argument text set to "confinement-check". Report
   the exact reply text.

4. Use the sub_agent tool to delegate exactly this task: "Create a file named NOTES.md
   with a three-line summary of this session: line 1 which write was denied, line 2
   which write succeeded, line 3 what the ping tool replied." Report what the sub-agent
   did.

5. Read NOTES.md and show its content, then say DONE.
```

## What YOU observe (the actual checklist)

| Step | Expect | Proves |
|------|--------|--------|
| 1 | Terminal runs **with no approval prompt**; command fails with an OS `Permission denied` on `/home/airic/apogee-escape-test.txt` | (i) out-of-workspace write OS-denied |
| 2 | Runs with no prompt; prints `hello-from-auto`; `inside.txt` exists in the workspace | (i) in-workspace write succeeds |
| 3 | **Approval prompt appears** for `demo__ping` → allow it → reply `pong: confinement-check` | (ii) MCP still gates in Auto |
| 4 | A sub-agent is delegated; its nested tool work (writing `NOTES.md`) renders in the transcript | (iii) delegation renders |
| 5 | `NOTES.md` content shown; model says DONE | the conversation produced a deliverable |

Small-model tolerance: exact wording of its reports doesn't matter, and mild flailing is
fine — the checklist is about what **Apogee** does (deny / not-prompt / prompt / render),
not about the model's prose. If it stalls mid-list, just tell it "continue with step N".

## Afterwards

1. Sanity check from a normal shell: `ls -l /home/airic/apogee-escape-test.txt` must say
   **No such file or directory**.
2. Optionally remove the `mcp-servers:` block from `~/.apogee/config.yaml` (the script can
   stay; it does nothing unless configured).
3. Flip the CHANGELOG "Live Auto-confined deliverable run" bullet's **Linux arm** to ✅
   with the date and box (Ubuntu devbox, kernel 7.0.0-28-generic aarch64, landlock), and
   update TODO.md's "Phase-5 verification leftovers" if applicable. Then archive this
   runbook to `docs/plans/archived/`.
