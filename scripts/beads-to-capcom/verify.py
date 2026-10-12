"""Check a converted capcom store against apogee's final beads export.

Runs capcom in the repo root and compares what it reports with the export, through the id table.
Every check prints PASS or FAIL; any FAIL exits 1.

usage: python3 -I verify.py <export.jsonl> <repo-root> [<idtable.json>]

The id table defaults to the idtable.json beside this script. Run it after `capcom reindex`.
"""

import json
import os
import subprocess
import sys

# The counts of apogee's frozen beads register (2026-10-12), asserted on both sides.
EXPECTED_ISSUES = 312
EXPECTED_DEPS = 5
EXPECTED_RELATED = 2
EXPECTED_NESTED = 60
EXPECTED_PLANNED = 139
EXPECTED_OPEN = 41
EXPECTED_READY = 15

PLAN_DIRECTORY_PREFIX = "docs/plans/"
PARKED_LABEL = "parked"
FRONTMATTER_FENCE = "---"
RELATED_KEY = "related:"


def capcom_data(root, *arguments):
    completed = subprocess.run(
        ["capcom", *arguments, "--json"], cwd=root, capture_output=True, text=True, check=False
    )
    if completed.returncode != 0:
        raise SystemExit(f"capcom {' '.join(arguments[:3])} failed: {completed.stderr.strip()}")
    return json.loads(completed.stdout)["data"]


def expected_status(row):
    if row["status"] == "closed":
        return "done"
    return "parked" if PARKED_LABEL in (row.get("labels") or []) else "open"


def is_plan_spec(spec):
    return bool(spec) and spec.startswith(PLAN_DIRECTORY_PREFIX) and spec.endswith(".md")


def stores_related(root, relative_path):
    """Report whether the issue file's frontmatter holds a related: key."""
    with open(os.path.join(root, relative_path), encoding="utf-8") as handle:
        lines = handle.read().split("\n")
    closing = lines.index(FRONTMATTER_FENCE, 1)
    return any(line.startswith(RELATED_KEY) for line in lines[1:closing])


def main():
    if len(sys.argv) not in (3, 4):
        raise SystemExit(__doc__)
    export_path, root = sys.argv[1], sys.argv[2]
    default_table = os.path.join(os.path.dirname(os.path.abspath(__file__)), "idtable.json")
    table_path = sys.argv[3] if len(sys.argv) == 4 else default_table
    with open(export_path, encoding="utf-8") as handle:
        rows = [json.loads(line) for line in handle if line.strip()]
    with open(table_path, encoding="utf-8") as handle:
        ids = json.load(handle)

    all_issues = capcom_data(root, "list", "--all")
    open_issues = capcom_data(root, "list")
    ready_issues = capcom_data(root, "ready")
    shown = capcom_data(root, "show", "--no-elsewhere", *sorted(ids.values()))
    resolved = capcom_data(root, "show", "--no-elsewhere", *sorted(ids))
    by_slug = {issue["id"]: issue for issue in all_issues}

    failures = []

    def check(name, is_passing, detail=""):
        print(f"{'PASS' if is_passing else 'FAIL'} {name}" + (f": {detail}" if detail else ""))
        if not is_passing:
            failures.append(name)

    def dependency_pairs(kind):
        return sorted(
            (ids[dependency["issue_id"]], ids[dependency["depends_on_id"]])
            for row in rows
            for dependency in row.get("dependencies") or []
            if dependency["type"] == kind
        )

    check("export issue count", len(rows) == EXPECTED_ISSUES, f"{len(rows)}")
    check("list --all count", len(all_issues) == len(rows), f"{len(all_issues)}")
    check("id table covers the export", set(ids) == {row["id"] for row in rows})
    check("capcom ids are the id table", set(by_slug) == set(ids.values()))

    one_to_one = [
        issue for issue in all_issues
        if len(issue.get("aliases") or []) == 1 and ids.get(issue["aliases"][0]) == issue["id"]
    ]
    check("aliases 1:1", len(one_to_one) == EXPECTED_ISSUES, f"{len(one_to_one)}")
    alias_targets = sorted(issue["id"] for issue in resolved)
    check("every alias resolves", alias_targets == sorted(ids[beads_id] for beads_id in sorted(ids)))

    mismatched = sorted(
        (ids[row["id"]], by_slug[ids[row["id"]]]["status"], expected_status(row))
        for row in rows
        if ids[row["id"]] in by_slug and by_slug[ids[row["id"]]]["status"] != expected_status(row)
    )
    check("status mapping", not mismatched, f"{mismatched}" if mismatched else "")
    titles_differ = [row["id"] for row in rows if by_slug.get(ids[row["id"]], {}).get("title") != row["title"]]
    check("titles verbatim", not titles_differ, f"{titles_differ}" if titles_differ else "")

    expected_open = {ids[row["id"]] for row in rows if row["status"] != "closed"}
    actual_open = {issue["id"] for issue in open_issues}
    check("open count", len(actual_open) == EXPECTED_OPEN == len(expected_open), f"{len(actual_open)}")
    check("open set", actual_open == expected_open,
          f"missing={expected_open - actual_open} extra={actual_open - expected_open}")

    expected_deps = dependency_pairs("blocks")
    actual_deps = sorted((issue["id"], dep) for issue in all_issues for dep in issue.get("deps") or [])
    check("deps", len(actual_deps) == EXPECTED_DEPS and actual_deps == expected_deps, f"{len(actual_deps)}")

    # `capcom show` lists a related link on both issues; the file of only one side stores it.
    expected_related = {frozenset(pair) for pair in dependency_pairs("related")}
    actual_related = {frozenset((issue["id"], other)) for issue in shown for other in issue.get("related") or []}
    storing_sides = [issue["id"] for issue in all_issues if stores_related(root, issue["path"])]
    check("related", len(actual_related) == EXPECTED_RELATED and actual_related == expected_related,
          f"{len(actual_related)}")
    check("related stored on one side", len(storing_sides) == EXPECTED_RELATED, f"{storing_sides}")

    expected_nesting = dependency_pairs("parent-child")
    actual_nesting = sorted((issue["id"], issue["parent"]) for issue in all_issues if issue.get("parent"))
    check("nesting", len(actual_nesting) == EXPECTED_NESTED and actual_nesting == expected_nesting,
          f"{len(actual_nesting)}")

    expected_planned = sorted(
        (ids[row["id"]], os.path.basename(row["spec_id"])[: -len(".md")])
        for row in rows if is_plan_spec(row.get("spec_id"))
    )
    actual_planned = sorted((issue["id"], issue["plan"]) for issue in all_issues if issue.get("plan"))
    check("planned", len(actual_planned) == EXPECTED_PLANNED and actual_planned == expected_planned,
          f"{len(actual_planned)}")

    open_unparked = {
        ids[row["id"]] for row in rows
        if row["status"] == "open" and PARKED_LABEL not in (row.get("labels") or [])
    }
    ready_ids = {issue["id"] for issue in ready_issues}
    check("ready count", len(ready_ids) == EXPECTED_READY, f"{len(ready_ids)}")
    check("ready is open and unparked", ready_ids <= open_unparked, f"{ready_ids - open_unparked or ''}")

    if failures:
        print(f"{len(failures)} check(s) failed")
        sys.exit(1)
    print("all checks passed")


if __name__ == "__main__":
    main()
