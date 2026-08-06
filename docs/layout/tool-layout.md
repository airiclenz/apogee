- clicking anywhere in the tool body must expand/collapse it.
- grouped tools calls should display the group-count
- expanded tools should be printed with a brighter gray than collapsed

# Single non-grouped tools
(non gouped)

available space / with:
-------------------------------------------------------------------------------

## single example call collapsed

✦ Run ▶
  ┕ cd . && head -3 go.mod && echo "---" && wc -l $(find . -name '*.go' | grep 
    -v dist)2>/dev/null | tail -1 …
    +5 more lines

## singe example call expanded

✦ Run ▼
  ┕ cd . && head -3 go.mod && echo "---" && wc -l $(find . -name '*.go' | grep 
    -v dist)2>/dev/null | tail -1
    module github.com/airiclenz/apogee
   
    go 1.26.3
    ---
    173441 total


# Grouped tools

available space / with:
-------------------------------------------------------------------------------

## grouped example calls collapsed

✦ Run (3)
  ┝ cd . && git log --oneline -10 2>/dev/null | head -20; echo "---"; git …   ▶
  ┝ cd . && head -3 go.mod && echo "---" && wc -l $(find . -name '*.go' | …   ▶
  ┕ S=""; for d in ".claude/skills/code-audit" "$HOME/.claude/skills/code …   ▶


## grouped example calls with middle one expanded

✦ Run (3)
  ┝ cd . && git log --oneline -10 2>/dev/null | head -20; echo "---"; git …   ▶
  ┝ cd . && head -3 go.mod && echo "---" && wc -l $(find . -name '*.go' |     ▼
  │ grep -v dist) 2>/dev/null | tail -1
  │ module github.com/airiclenz/apogee
  │
  │ go 1.26.3
  │ ---
  │ 173441 total
  │                                                                   see less…
  ┕ S=""; for d in ".claude/skills/code-audit" "$HOME/.claude/skills/code-…   ▶