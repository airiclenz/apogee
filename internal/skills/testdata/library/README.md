# Real-library fixture for `Suggest`

A catalog-shaped copy of the owner's global skill library (`~/.apogee/skills/`, 24 skills as of
2026-08-27): each `skills/<id>/SKILL.md` is the source's frontmatter block verbatim (name,
description, any triggers) over a one-line placeholder body. Bodies are deliberately not copied —
`Suggest` never reads them, and `suggest_library_test.go` cares only about id, display name,
description (indexed in full — the 200-rune summary clamp is the "/" menu's alone) and triggers.
Loaded through the ordinary `Load(Sources{Home: "testdata/library"})`.

Refresh it from the live library with (run from the repo root):

```bash
for d in ~/.apogee/skills/*/; do id=$(basename "$d"); f="$d/SKILL.md"; [ -f "$f" ] || continue;
  mkdir -p internal/skills/testdata/library/skills/$id;
  { awk 'NR==1&&$0!="---"{exit} {print} NR>1&&$0=="---"{exit}' "$f";
    echo; echo "Fixture body — see testdata/library/README.md."; } \
    > internal/skills/testdata/library/skills/$id/SKILL.md; done
```
