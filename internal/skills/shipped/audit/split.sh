#!/bin/sh
# split.sh — the scope split of apogee's shipped `audit` recipe.
#
# Usage (run from the workspace root: scope paths resolve against the cwd):
#   sh split.sh <RUN> <SCOPE> [FOCUS] [PART_BYTES]
#   sh split.sh --flags <RUN>
#
# The engine runs both forms as the recipe's script stages, <RUN> being the
# workflow folder. It copies this file into <RUN>/skill/ first, because the
# skill's own `shipped:` address is not a path any shell can run.
#
# The split form lists the workspace's source files under <SCOPE> (one or more
# workspace-relative files or folders, whitespace-separated; `.` or `all` for
# the whole workspace) and writes, all under <RUN>:
#   scope.txt              every file in scope, one workspace-relative path a line
#   src.txt, tests.txt     the scope split into source and test files (TEST_PATTERN)
#   part-<name>/scope.txt  one part's source files
#   part-<name>/tests.txt  the test files that belong next to that part
#   group-<name>/parts.txt the part folders one rollup child folds (absolute)
#   group-<name>/cap.txt   how many claims that group's enumerator may pass on
#   run-dir.txt            <RUN> itself, absolute — the item the one-off stages take
#   parts.txt              every part folder, absolute, one a line
#   conc-parts.txt         the part folders holding a concurrency primitive
#   groups.txt             every group folder, absolute, one a line
#   split.txt              one `<part> <files> <lines> conc=<yes|no> group=<name>`
#                          line a part, for a human or a child to read
#   layout.txt             the layout and bounds the split was made with
#
# Its stdout is KEY=value lines only, one a line — the receipt the script stage
# reads: files, src, tests, parts, groups, conc_parts, part_lines, focus and a
# summary. focus is FOCUS when it names a focus area and `none` otherwise, so
# the recipe asks the user only when the input left the focus open.
#
# A part holds at most PART_LINES source lines and PART_FILES files. PART_LINES
# is the child's window: an environment value wins; otherwise it is derived
# from PART_BYTES (the engine's split budget in bytes for one child) as half
# that budget at BYTES_PER_LINE bytes a line — the other half is the child's
# room for the bundle, the tools digest, the tests and its own work — clamped
# to MIN_PART_LINES..MAX_PART_LINES; with no budget it is MAX_PART_LINES.
# A scope that fits one part is one part, `all`. Env knobs: PART_LINES,
# PART_FILES (15), GROUP_PARTS (8).
#
# Re-running on a folder whose scope and bounds have not changed touches
# nothing and re-prints the same receipt; a changed scope or bound re-splits.
#
# The flags form reads the CONCURRENCY line the ground-truth child writes into
# <RUN>/bundle.md and prints `concurrency=yes` or `concurrency=no`.
#
# Paths are read one a line and always quoted, so a name holding spaces, `*`,
# `?` or `[` is one file, never several or a glob. A name holding a newline
# cannot sit in a one-a-line list: it is skipped.
#
# POSIX sh only — hosts may run it with `sh`, never assume bash.

set -eu
# Byte-order sorting and globbing so the split is identical on every host.
LC_ALL=C
export LC_ALL

PART_FILES=${PART_FILES:-15}
GROUP_PARTS=${GROUP_PARTS:-8}
MAX_PART_LINES=8000
MIN_PART_LINES=200
BYTES_PER_LINE=40
# A part with fewer files than this merges into a neighbour of its directory.
MIN_PART_FILES=3
# The claims every group's enumerator may pass on together, and one group's bounds.
TOTAL_CLAIMS=60
MIN_GROUP_CLAIMS=2
MAX_GROUP_CLAIMS=12
LAYOUT_VERSION=recipe-1
# The part a scope that fits one part is given.
WHOLE_PART=all
FOCUS_AREAS='all bugs security tests standards'

# Test files: language-conventional suffixes, test directories, pytest names.
TEST_PATTERN='(_test\.go|_test\.py|\.test\.[cm]?[jt]sx?|\.spec\.[cm]?[jt]sx?|Tests?\.cs|Tests\.swift|Test\.java|_spec\.rb)$|(^|/)(test|tests|__tests__|spec|specs|testing)/|(^|/)test_[^/]+\.py$'
# Concurrency primitives across the supported languages; one hit gates the concurrency lens.
CONC_PATTERN='go func|(^|[[:space:]])go [A-Za-z_(]|<-|chan |sync\.|atomic\.|errgroup|async |await |Thread|Task\.Run|Parallel\.|pthread_|std::thread|std::mutex|DispatchQueue|actor |Promise\.all|Worker\(|context\.With(Cancel|Timeout|Deadline)|time\.After'
# Never source code: docs, dependencies, build output, fixtures, generated files.
EXCLUDED_DIRS='^docs/|(^|/)(vendor|node_modules|build|dist|target|bin|obj|testdata|fixtures|__snapshots__)/'
EXCLUDED_FILES='\.(md|lock|sum|png|jpe?g|gif|svg|ico|pdf|pb\.go|min\.js)$|CHANGELOG|_generated\.'

# ------------------------------------------------------------------------------
# Prints a failure as the receipt's summary and on stderr, and exits non-zero.
die() {
  printf 'summary=split.sh: %s\n' "$1"
  printf 'split.sh: %s\n' "$1" >&2
  exit 1
}

# ------------------------------------------------------------------------------
# Prints the part name encoded in a scope-<part>.txt path.
part_name() {
  name=${1##*/scope-}
  printf '%s\n' "${name%.txt}"
}

# ------------------------------------------------------------------------------
# Prints the number of lines in one file; a missing file counts as 0.
file_lines() {
  { wc -l < "$1" 2>/dev/null || echo 0; } | tr -d ' '
}

# ------------------------------------------------------------------------------
# Prints the number of entries in a list file; a missing file counts as 0.
list_length() {
  if [ -f "$1" ]; then wc -l < "$1" | tr -d ' '; else echo 0; fi
}

# ------------------------------------------------------------------------------
# Prints the total source-line count of every file named in a list file.
list_source_lines() {
  total=0
  if [ -f "$1" ]; then
    while IFS= read -r file; do
      total=$((total + $(file_lines "$file")))
    done < "$1"
  fi
  printf '%s\n' "$total"
}

# ------------------------------------------------------------------------------
# Succeeds when any file named in a list file holds a concurrency primitive.
holds_concurrency() {
  while IFS= read -r file; do
    if grep -qE -- "$CONC_PATTERN" "$file" 2>/dev/null; then return 0; fi
  done < "$1"
  return 1
}

# ------------------------------------------------------------------------------
# Succeeds when a part exceeds either bound.
is_oversized() {
  [ "$(list_length "$1")" -gt "$PART_FILES" ] \
    || [ "$(list_source_lines "$1")" -gt "$PART_LINES" ]
}

# ------------------------------------------------------------------------------
# Prints the focus area FOCUS names, or `none`.
focus_area() {
  given=$(printf '%s' "$1" | tr 'A-Z' 'a-z')
  for area in $FOCUS_AREAS; do
    if [ "$given" = "$area" ]; then printf '%s\n' "$area"; return 0; fi
  done
  printf 'none\n'
}

# ------------------------------------------------------------------------------
# Sets PART_LINES from the environment, else from PART_BYTES (see the header).
resolve_part_lines() {
  case ${PART_LINES:-} in
    '') ;;
    *[!0-9]*) die "PART_LINES is not a whole number: $PART_LINES" ;;
    *) return 0 ;;
  esac
  case $1 in ''|*[!0-9]*) budget=0 ;; *) budget=$1 ;; esac
  if [ "$budget" -le 0 ]; then PART_LINES=$MAX_PART_LINES; return 0; fi
  PART_LINES=$((budget / (2 * BYTES_PER_LINE)))
  [ "$PART_LINES" -ge "$MIN_PART_LINES" ] || PART_LINES=$MIN_PART_LINES
  [ "$PART_LINES" -le "$MAX_PART_LINES" ] || PART_LINES=$MAX_PART_LINES
}

# ------------------------------------------------------------------------------
# Writes the workspace's source files under the scope paths to $1: tracked and
# untracked-but-not-ignored files in a git repo, every file otherwise, less
# docs, dependencies, build output, fixtures and generated files. git lists
# NUL-terminated and unquoted, so a name is printed as it is on disk; a listed
# line naming no regular file (the fragments of a name holding a newline) is
# dropped.
list_scope() {
  if git -c core.quotePath=false ls-files -z --cached --others --exclude-standard \
    > "$TMP/listed" 2>/dev/null; then
    tr '\000' '\n' < "$TMP/listed"
  else
    find . -type f -not -path '*/.git/*' | sed 's|^\./||'
  fi \
    | { grep -vE -- "$EXCLUDED_DIRS" || true; } \
    | { grep -vE -- "$EXCLUDED_FILES" || true; } \
    | sort -u > "$TMP/workspace"
  : > "$TMP/wanted"
  set -f
  for path in $SCOPE; do
    path=${path#./}
    while [ "${path%/}" != "$path" ]; do path=${path%/}; done
    case $path in
      ''|.|all|everything) cat "$TMP/workspace" >> "$TMP/wanted" ;;
      *) awk -v p="$path" '$0 == p || index($0, p "/") == 1' "$TMP/workspace" >> "$TMP/wanted" ;;
    esac
  done
  set +f
  sort -u "$TMP/wanted" | while IFS= read -r line; do
    if [ -f "$line" ]; then printf '%s\n' "$line"; fi
  done > "$1"
}

# ------------------------------------------------------------------------------
# Prints "<part>\t<top-level dir>\t<second-level group>" for every part, in
# part-name order. Both keys come from the part's first file, so a part's
# directory never depends on its name.
part_index() {
  for part_file in "$W"/scope-*.txt; do
    [ -e "$part_file" ] || continue
    awk -F/ -v part="$(part_name "$part_file")" '
      NR == 1 {
        top = (NF > 1 ? $1 : "root")
        grp = (NF > 2 ? top "-" $2 : top)
        print part "\t" top "\t" grp
        exit
      }' "$part_file"
  done | sort
}

# ------------------------------------------------------------------------------
# Mechanic 1: one part per top-level directory of the source list, plus a
# root part for files at the workspace root (dropped when empty).
split_by_top_level_dir() {
  grep -v / "$1" > "$W/scope-root.txt" || true
  [ -s "$W/scope-root.txt" ] || rm -f "$W/scope-root.txt"
  { grep / "$1" || true; } | cut -d/ -f1 | sort -u > "$TMP/dirs"
  while IFS= read -r dir; do
    awk -v prefix="$dir/" 'index($0, prefix) == 1' "$1" > "$W/scope-$dir.txt"
  done < "$TMP/dirs"
}

# ------------------------------------------------------------------------------
# Mechanic 2: re-split one oversized top-level part one level deeper —
# scope-<dir>-<sub>.txt per second-level directory; files directly under
# <dir> keep the original part (dropped when there are none).
resplit_one_level_deeper() {
  part_file="$W/scope-$1.txt"
  awk -F/ 'NF > 2 { print $2 }' "$part_file" | sort -u > "$TMP/subs"
  while IFS= read -r sub; do
    awk -v prefix="$1/$sub/" 'index($0, prefix) == 1' "$part_file" \
      > "$W/scope-$1-$sub.txt"
  done < "$TMP/subs"
  awk -F/ 'NF == 2' "$part_file" > "$TMP/direct"
  if [ -s "$TMP/direct" ]; then cp "$TMP/direct" "$part_file"; else rm -f "$part_file"; fi
}

# ------------------------------------------------------------------------------
# Mechanic 3: chunk a still-oversized flat part in its sorted order, closing a
# chunk when the next file would push it over either bound. A part that fits
# in one chunk (a single huge file) keeps its name.
chunk_flat_part() {
  part_file="$W/scope-$1.txt"
  i=1; lines=0; files=0
  while IFS= read -r file; do
    n=$(file_lines "$file")
    if [ "$files" -gt 0 ] \
      && { [ $((lines + n)) -gt "$PART_LINES" ] || [ "$files" -ge "$PART_FILES" ]; }; then
      i=$((i + 1)); lines=0; files=0
    fi
    printf '%s\n' "$file" >> "$W/scope-$1-$i.txt"
    lines=$((lines + n)); files=$((files + 1))
  done < "$part_file"
  rm -f "$part_file"
  [ "$i" -gt 1 ] || mv "$W/scope-$1-1.txt" "$part_file"
}

# ------------------------------------------------------------------------------
# Mechanic 4: a part under MIN_PART_FILES files merges into the alphabetically
# next part of the same top-level directory — or, when it is the last one,
# into the previous part — unless the merge would push that part over either
# bound. A directory with a single part keeps it as is. Part names come from
# directory names, so they are read one a line and never word-split.
merge_small_parts() {
  part_index | cut -f2 | sort -u > "$TMP/merge-dirs"
  while IFS= read -r dir; do
    part_index | awk -F'\t' -v dir="$dir" '$2 == dir { print $1 }' > "$TMP/merge-parts"
    count=$(list_length "$TMP/merge-parts")
    previous=""
    i=0
    while [ "$i" -lt "$count" ]; do
      i=$((i + 1))
      current=$(sed -n "${i}p" "$TMP/merge-parts")
      part_file="$W/scope-$current.txt"
      if [ "$(list_length "$part_file")" -ge "$MIN_PART_FILES" ]; then
        previous=$current; continue
      fi
      if [ "$i" -lt "$count" ]; then target=$(sed -n "$((i + 1))p" "$TMP/merge-parts")
      elif [ -n "$previous" ]; then target=$previous
      else previous=$current; continue
      fi
      cat "$W/scope-$target.txt" "$part_file" | sort > "$TMP/merged"
      if is_oversized "$TMP/merged"; then previous=$current; continue; fi
      cp "$TMP/merged" "$W/scope-$target.txt"
      rm -f "$part_file"
    done
  done < "$TMP/merge-dirs"
}

# ------------------------------------------------------------------------------
# Writes tests-<part>.txt for every part: a test file follows the part holding
# its directory, then the part holding its directory with a trailing test
# segment stripped, then every part of its top-level directory, else the root
# part — or, when there is none, the first part, so no test file is dropped.
write_test_lists() {
  for part_file in "$W"/scope-*.txt; do
    [ -e "$part_file" ] || continue
    awk -v part="$(part_name "$part_file")" '{ print part "\t" $0 }' "$part_file"
  done > "$TMP/parts.idx"
  fallback=$(part_index | head -n 1 | cut -f1)
  [ ! -e "$W/scope-root.txt" ] || fallback=root
  awk -v fallback="$fallback" '
    BEGIN { FS = "\t" }
    function dirname(path,  d) { d = path; if (sub(/\/[^\/]*$/, "", d) == 0) d = "."; return d }
    function topdir(path,  t) { t = path; if (sub(/\/.*$/, "", t) == 0) t = "root"; return t }
    function strip_test_dir(dir,  s) {
      s = dir
      if (sub(/(^|\/)(test|tests|__tests__|spec|specs|testing)$/, "", s) && s == "") s = "."
      return s
    }
    NR == FNR {
      d = dirname($2); t = topdir($2)
      if (!((d, $1) in seen_dir)) { seen_dir[d, $1] = 1; by_dir[d] = by_dir[d] "\t" $1 }
      if (!((t, $1) in seen_top)) { seen_top[t, $1] = 1; by_top[t] = by_top[t] "\t" $1 }
      next
    }
    {
      d = dirname($0); stripped = strip_test_dir(d); t = topdir($0)
      if (d in by_dir) parts = by_dir[d]
      else if (stripped in by_dir) parts = by_dir[stripped]
      else if (t in by_top) parts = by_top[t]
      else parts = "\t" fallback
      n = split(parts, list, "\t")
      for (i = 1; i <= n; i++) if (list[i] != "") print list[i] "\t" $0
    }' "$TMP/parts.idx" "$RUN/tests.txt" > "$TMP/tests.idx"
  for part_file in "$W"/scope-*.txt; do
    [ -e "$part_file" ] || continue
    part=$(part_name "$part_file")
    awk -F'\t' -v part="$part" '$1 == part { print $2 }' "$TMP/tests.idx" > "$W/tests-$part.txt"
  done
}

# ------------------------------------------------------------------------------
# Writes the part folders, the absolute item lists and the split table from the
# working parts: part-<name>/, parts.txt, conc-parts.txt, split.txt.
write_parts() {
  : > "$RUN/parts.txt"; : > "$RUN/conc-parts.txt"
  for part_file in "$W"/scope-*.txt; do
    [ -e "$part_file" ] || continue
    part=$(part_name "$part_file")
    folder="$RUN/part-$part"
    mkdir -p "$folder"
    cp "$part_file" "$folder/scope.txt"
    if [ -f "$W/tests-$part.txt" ]; then cp "$W/tests-$part.txt" "$folder/tests.txt"; else : > "$folder/tests.txt"; fi
    printf '%s\n' "$folder" >> "$RUN/parts.txt"
    if holds_concurrency "$part_file"; then printf '%s\n' "$folder" >> "$RUN/conc-parts.txt"; fi
  done
}

# ------------------------------------------------------------------------------
# Writes one group-<name>/ folder per top-level directory — or per second path
# segment when the directory holds more than GROUP_PARTS parts — with the part
# folders it folds and its claim cap, then groups.txt and split.txt.
write_groups() {
  part_index > "$TMP/index"
  : > "$TMP/grouping"
  cut -f2 "$TMP/index" | sort -u > "$TMP/group-dirs"
  while IFS= read -r dir; do
    count=$(awk -F'\t' -v dir="$dir" '$2 == dir' "$TMP/index" | wc -l | tr -d ' ')
    if [ "$count" -gt "$GROUP_PARTS" ]; then
      awk -F'\t' -v dir="$dir" '$2 == dir { print $1 "\t" $3 }' "$TMP/index" >> "$TMP/grouping"
    else
      awk -F'\t' -v dir="$dir" '$2 == dir { print $1 "\t" $2 }' "$TMP/index" >> "$TMP/grouping"
    fi
  done < "$TMP/group-dirs"
  cut -f2 "$TMP/grouping" | sort -u > "$TMP/group-names"
  groups=$(list_length "$TMP/group-names")
  cap=$((TOTAL_CLAIMS / groups))
  [ "$cap" -le "$MAX_GROUP_CLAIMS" ] || cap=$MAX_GROUP_CLAIMS
  [ "$cap" -ge "$MIN_GROUP_CLAIMS" ] || cap=$MIN_GROUP_CLAIMS
  : > "$RUN/groups.txt"
  while IFS= read -r grp; do
    folder="$RUN/group-$grp"
    mkdir -p "$folder"
    awk -F'\t' -v grp="$grp" -v run="$RUN" '$2 == grp { print run "/part-" $1 }' "$TMP/grouping" > "$folder/parts.txt"
    printf '%s\n' "$cap" > "$folder/cap.txt"
    printf '%s\n' "$folder" >> "$RUN/groups.txt"
  done < "$TMP/group-names"
  : > "$RUN/split.txt"
  while IFS="$(printf '\t')" read -r part grp; do
    folder="$RUN/part-$part"
    if grep -qxF -- "$folder" "$RUN/conc-parts.txt"; then conc=yes; else conc=no; fi
    printf '%s %s %s conc=%s group=%s\n' "$part" "$(list_length "$folder/scope.txt")" \
      "$(list_source_lines "$folder/scope.txt")" "$conc" "$grp" >> "$RUN/split.txt"
  done < "$TMP/grouping"
}

# ------------------------------------------------------------------------------
# Prints the receipt, derived from the files on disk so a re-run and a fresh
# split print the same thing.
print_receipt() {
  files=$(list_length "$RUN/scope.txt")
  parts=$(list_length "$RUN/parts.txt")
  groups=$(list_length "$RUN/groups.txt")
  printf 'files=%s\n' "$files"
  printf 'src=%s\n' "$(list_length "$RUN/src.txt")"
  printf 'tests=%s\n' "$(list_length "$RUN/tests.txt")"
  printf 'parts=%s\n' "$parts"
  printf 'groups=%s\n' "$groups"
  printf 'conc_parts=%s\n' "$(list_length "$RUN/conc-parts.txt")"
  printf 'part_lines=%s\n' "$PART_LINES"
  printf 'focus=%s\n' "$FOCUS"
  printf 'summary=%s files in %s parts and %s groups, at most %s lines a part\n' \
    "$files" "$parts" "$groups" "$PART_LINES"
}

# ------------------------------------------------------------------------------
# Prints the layout key: what a split of this folder was made with.
layout_key() {
  printf '%s part_lines=%s part_files=%s group_parts=%s\n' \
    "$LAYOUT_VERSION" "$PART_LINES" "$PART_FILES" "$GROUP_PARTS"
}

# ------------------------------------------------------------------------------
# Removes every file a previous split wrote so a re-split starts clean.
remove_previous_split() {
  rm -rf "$RUN"/part-* "$RUN"/group-*
  rm -f "$RUN/src.txt" "$RUN/tests.txt" "$RUN/run-dir.txt" "$RUN/parts.txt" \
    "$RUN/conc-parts.txt" "$RUN/groups.txt" "$RUN/split.txt" "$RUN/layout.txt"
}

# ------------------------------------------------------------------------------
# Prints the ground-truth child's concurrency answer as a receipt.
print_flags() {
  [ $# -eq 1 ] || die 'usage: sh split.sh --flags <RUN>'
  if grep -Eiq '^[*_[:space:]]*CONCURRENCY[*_[:space:]]*:[*_[:space:]]*yes' "${1%/}/bundle.md" 2>/dev/null; then
    concurrency=yes
  else
    concurrency=no
  fi
  printf 'concurrency=%s\n' "$concurrency"
  printf 'summary=the ground truth says concurrency %s\n' "$concurrency"
}

# ------------------------------------------------------------------------------
# Splits the scope end to end and prints the receipt.
main() {
  if [ "${1:-}" = --flags ]; then shift; print_flags "$@"; return 0; fi
  [ $# -ge 2 ] && [ $# -le 4 ] || die 'usage: sh split.sh <RUN> <SCOPE> [FOCUS] [PART_BYTES]'
  [ -d "$1" ] || die "workflow folder not found: $1"
  RUN=$(cd "$1" && pwd)
  SCOPE=$2
  FOCUS=$(focus_area "${3:-}")
  resolve_part_lines "${4:-}"
  # Scratch lives inside RUN, the one place this script must be able to write:
  # the system temp dir is refused by workspace-confined hosts, and a bare
  # `mktemp -d` ignores TMPDIR on macOS. Plain mkdir, since mktemp is not POSIX.
  TMP="$RUN/.split.$$"
  W="$TMP/parts"
  rm -rf "$TMP"
  mkdir -p "$W"
  trap 'rm -rf "$TMP"' EXIT

  list_scope "$TMP/scope"
  [ -s "$TMP/scope" ] || die "nothing in scope for: $SCOPE"
  if [ -f "$RUN/scope.txt" ] && cmp -s "$TMP/scope" "$RUN/scope.txt" \
    && [ -f "$RUN/layout.txt" ] && [ "$(cat "$RUN/layout.txt")" = "$(layout_key)" ]; then
    print_receipt
    return 0
  fi
  remove_previous_split
  cp "$TMP/scope" "$RUN/scope.txt"
  printf '%s\n' "$RUN" > "$RUN/run-dir.txt"

  { grep -E -- "$TEST_PATTERN" "$RUN/scope.txt" || true; } > "$RUN/tests.txt"
  { grep -vE -- "$TEST_PATTERN" "$RUN/scope.txt" || true; } > "$RUN/src.txt"
  # A test-only scope is split from scope.txt itself, with empty test lists.
  if [ -s "$RUN/src.txt" ]; then
    cp "$RUN/src.txt" "$TMP/sources"
  else
    cp "$RUN/scope.txt" "$TMP/sources"
    : > "$RUN/tests.txt"
  fi

  if is_oversized "$TMP/sources"; then
    split_by_top_level_dir "$TMP/sources"
    for part_file in "$W"/scope-*.txt; do
      [ -e "$part_file" ] || continue
      part=$(part_name "$part_file")
      if [ "$part" != root ] && is_oversized "$part_file"; then
        resplit_one_level_deeper "$part"
      fi
    done
    for part_file in "$W"/scope-*.txt; do
      [ -e "$part_file" ] || continue
      if is_oversized "$part_file"; then chunk_flat_part "$(part_name "$part_file")"; fi
    done
    merge_small_parts
  else
    cp "$TMP/sources" "$W/scope-$WHOLE_PART.txt"
  fi

  write_test_lists
  write_parts
  write_groups
  layout_key > "$RUN/layout.txt"
  print_receipt
}

main "$@"
