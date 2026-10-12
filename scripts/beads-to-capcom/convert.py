"""Convert apogee's final beads export into capcom issue files.

Follows capcom's docs/migrating-from-beads.md field mapping, plus apogee's own rules from
docs/plans/2026-10-12 - 00 - beads-to-capcom-migration-plan.md, item 2. Every capcom id comes from
idtable.json, which maps each beads id to a hand-chosen slug; this script never derives an id.

usage: python3 -I convert.py <export.jsonl> <repo-root> [<idtable.json>]

The id table defaults to the idtable.json beside this script. The repo root must already hold a
store made by `capcom init` that declares the types epic and decision.
"""

import json
import os
import re
import sys

SLUG_PATTERN = re.compile(r"^[a-z0-9]+(-[a-z0-9]+){1,3}$")
SLUG_MAX_LENGTH = 40
DATED_LINE_PATTERN = re.compile(r"^(\d{4}-\d{2}-\d{2})\b(.*)$")
LEADING_COLON_PATTERN = re.compile(r"^\s*:")
PLAN_DIRECTORY_PREFIX = "docs/plans/"
LOG_AUTHOR = "beads"
DEFAULT_CLOSE_REASON = "Closed in beads."
DROPPED_LABELS = ("parked", "planned")
PARKED_LABEL = "parked"
ISO_DATE_LENGTH = len("2026-01-01")


def is_valid_slug(text):
    return len(text) <= SLUG_MAX_LENGTH and SLUG_PATTERN.match(text) is not None


def load_id_table(path, rows):
    """Read the beads-id-to-slug table and refuse one that does not cover the export exactly."""
    with open(path, encoding="utf-8") as handle:
        table = json.load(handle)
    export_ids = {row["id"] for row in rows}
    missing = sorted(export_ids - table.keys())
    extra = sorted(table.keys() - export_ids)
    invalid = sorted(slug for slug in table.values() if not is_valid_slug(slug))
    if missing or extra or invalid:
        raise SystemExit(f"id table mismatch: missing={missing} extra={extra} invalid={invalid}")
    if len(set(table.values())) != len(table):
        raise SystemExit("id table maps two beads ids to one slug")
    if export_ids & set(table.values()):
        raise SystemExit("id table reuses a beads id as a slug")
    return table


def yaml_string(value):
    # A JSON string is a valid YAML double-quoted scalar; `capcom fmt` canonicalises it after.
    return json.dumps(value, ensure_ascii=False)


def yaml_list(values):
    return "[" + ", ".join(yaml_string(value) for value in values) + "]"


def log_entry(date, author, text):
    lines = text.rstrip("\n").split("\n")
    while lines and not lines[-1].strip():
        lines.pop()
    first_line = lines[0] if lines else ""
    head = f"- {date} {author}:" + (f" {first_line}" if first_line else "")
    continuation = ["  " + line if line.strip() else "" for line in lines[1:]]
    return "\n".join([head] + continuation)


def split_notes(notes, undated_date, created_date):
    """Split a notes value into (date, text) entries, one per dated block.

    Only a date at the start of a line opens an entry; the date and one colon after it are
    dropped from the text. Notes with no dated line become one entry at undated_date (closed_at,
    else created_at). Undated text before the first dated line is dated created_date.
    """
    lead_lines = []
    dated_entries = []
    for line in notes.split("\n"):
        match = DATED_LINE_PATTERN.match(line)
        if match:
            text = LEADING_COLON_PATTERN.sub("", match.group(2), count=1).lstrip()
            dated_entries.append((match.group(1), [text]))
        elif dated_entries:
            dated_entries[-1][1].append(line)
        else:
            lead_lines.append(line)
    lead_text = "\n".join(lead_lines).strip("\n")
    if not dated_entries:
        return [(undated_date, lead_text)] if lead_text.strip() else []
    entries = [(created_date, lead_text)] if lead_text.strip() else []
    entries.extend((date, "\n".join(lines).strip("\n")) for date, lines in dated_entries)
    return entries


def collect_links(rows, by_id, ids):
    """Sort beads dependencies into parents, deps, related links and discovered-from notes."""
    parent_of = {}
    deps = {}
    related = {}
    discovered = {}
    for row in rows:
        for dependency in row.get("dependencies") or []:
            source = dependency["issue_id"]
            target = dependency["depends_on_id"]
            kind = dependency["type"]
            if source != row["id"] or target not in by_id:
                raise SystemExit(f"malformed or dangling dependency {dependency}")
            if kind == "parent-child":
                if source in parent_of:
                    raise SystemExit(f"two parents for {source}")
                parent_of[source] = target
            elif kind == "blocks":
                deps.setdefault(source, []).append(ids[target])
            elif kind == "related":
                related.setdefault(source, []).append(ids[target])
            elif kind == "discovered-from":
                date = dependency["created_at"][:ISO_DATE_LENGTH]
                discovered.setdefault(source, []).append((date, ids[target]))
            else:
                raise SystemExit(f"unmapped dependency type {kind!r} on {source}")
    return parent_of, deps, related, discovered


def issue_path(root, beads_id, parent_of, ids):
    ancestors = []
    seen = set()
    current = beads_id
    while current in parent_of:
        if current in seen:
            raise SystemExit(f"parent cycle at {current}")
        seen.add(current)
        current = parent_of[current]
        ancestors.append(ids[current])
    return os.path.join(root, ".capcom", "issues", *reversed(ancestors), ids[beads_id] + ".md")


def map_status(row, has_deps):
    """Return (status, closed_at, reason). A closed beads issue is always done: no apogee close
    reason says duplicate, abandoned or rejected, so nothing maps to dropped."""
    beads_status = row["status"]
    is_parked = PARKED_LABEL in (row.get("labels") or [])
    if beads_status == "closed":
        return "done", row["closed_at"], row.get("close_reason") or DEFAULT_CLOSE_REASON
    if beads_status == "open":
        return ("parked" if is_parked else "open"), None, None
    if beads_status == "in_progress":
        return "active", None, None
    if beads_status == "deferred":
        return "parked", None, None
    if beads_status == "blocked":
        return ("open" if has_deps else "parked"), None, None
    raise SystemExit(f"unmapped status {beads_status!r} on {row['id']}")


def build_log(row, discovered, ids):
    """Return (plan basename or None, the sorted Log entry lines)."""
    created_date = row["created_at"][:ISO_DATE_LENGTH]
    undated_date = (row.get("closed_at") or row["created_at"])[:ISO_DATE_LENGTH]
    entries = []  # (date, insertion order, text); the order keeps same-date entries stable

    def add(date, author, text):
        entries.append((date, len(entries), log_entry(date, author, text)))

    plan = None
    spec = row.get("spec_id")
    if spec:
        basename = os.path.basename(spec)
        if spec.startswith(PLAN_DIRECTORY_PREFIX) and basename.endswith(".md"):
            plan = basename[: -len(".md")]
        else:
            add(created_date, LOG_AUTHOR, f"spec: {spec}")
    if row.get("notes"):
        for date, text in split_notes(row["notes"], undated_date, created_date):
            add(date, LOG_AUTHOR, text)
    for date, other_id in discovered.get(row["id"], []):
        add(date, LOG_AUTHOR, f"discovered while working on {other_id}")
    for comment in sorted(row.get("comments") or [], key=lambda c: (c["created_at"], c["id"])):
        add(comment["created_at"][:ISO_DATE_LENGTH], comment["author"], comment["text"])
    entries.sort(key=lambda entry: (entry[0], entry[1]))
    return plan, [entry[2] for entry in entries]


def render_issue(row, ids, deps, related, discovered):
    beads_id = row["id"]
    status, closed_at, reason = map_status(row, bool(deps.get(beads_id)))
    plan, log_lines = build_log(row, discovered, ids)
    labels = [label for label in (row.get("labels") or []) if label not in DROPPED_LABELS]

    frontmatter = [
        "---",
        f"title: {yaml_string(row['title'])}",
        f"status: {status}",
        f"type: {row['issue_type']}",
        f"priority: {row['priority']}",
    ]
    if labels:
        frontmatter.append(f"labels: {yaml_list(labels)}")
    if deps.get(beads_id):
        frontmatter.append(f"deps: {yaml_list(deps[beads_id])}")
    if related.get(beads_id):
        frontmatter.append(f"related: {yaml_list(related[beads_id])}")
    if plan:
        frontmatter.append(f"plan: {yaml_string(plan)}")
    frontmatter.append(f"aliases: {yaml_list([beads_id])}")
    frontmatter.append(f"created: {row['created_at']}")
    if closed_at:
        frontmatter.append(f"closed: {closed_at}")
        frontmatter.append(f"reason: {yaml_string(reason)}")
    frontmatter.append("---")

    body = [row.get("description") or ""]
    if row.get("design"):
        body.append("## Design\n\n" + row["design"])
    if row.get("acceptance_criteria"):
        body.append("## Acceptance\n\n" + row["acceptance_criteria"])
    if log_lines:
        body.append("## Log\n\n" + "\n".join(log_lines))
    sections = "\n\n".join(part.strip("\n") for part in body if part.strip())
    return "\n".join(frontmatter) + "\n\n" + sections + "\n"


def main():
    if len(sys.argv) not in (3, 4):
        raise SystemExit(__doc__)
    export_path, root = sys.argv[1], sys.argv[2]
    default_table = os.path.join(os.path.dirname(os.path.abspath(__file__)), "idtable.json")
    table_path = sys.argv[3] if len(sys.argv) == 4 else default_table

    with open(export_path, encoding="utf-8") as handle:
        rows = [json.loads(line) for line in handle if line.strip()]
    by_id = {row["id"]: row for row in rows}
    ids = load_id_table(table_path, rows)
    parent_of, deps, related, discovered = collect_links(rows, by_id, ids)

    for row in rows:
        path = issue_path(root, row["id"], parent_of, ids)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w", encoding="utf-8", newline="\n") as handle:
            handle.write(render_issue(row, ids, deps, related, discovered))

    print(
        f"wrote {len(rows)} issues; nested={len(parent_of)} "
        f"deps={sum(map(len, deps.values()))} related={sum(map(len, related.values()))} "
        f"discovered={sum(map(len, discovered.values()))}"
    )


if __name__ == "__main__":
    main()
