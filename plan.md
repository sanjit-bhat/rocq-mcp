# Plan: Remove diff'ing, return separate goal counts

## Summary

Remove all diff/delta logic from step_forward and step_backward. Both tools
(and check/check_all/get_proof_state) return the same unified format: current
goals in full, separate counts for unfocused/shelved/given-up goals, messages,
and diagnostics.

---

## 1. `types.go` — split `UnfocusedCount` into three fields

Replace:
```go
UnfocusedCount int
```
With:
```go
UnfocusedCount int // background goals (unfocusedGoals minus focused)
ShelvedCount   int
GivenUpCount   int
```

## 2. `format.go` — parse separate counts in `ParseProofView`

Change:
```go
pv := &ProofView{
    UnfocusedCount: unfocused + len(raw.Proof.ShelvedGoals) + len(raw.Proof.GivenUpGoals),
}
```
To:
```go
pv := &ProofView{
    UnfocusedCount: unfocused,
    ShelvedCount:   len(raw.Proof.ShelvedGoals),
    GivenUpCount:   len(raw.Proof.GivenUpGoals),
}
```

## 3. `format.go` — delete diff functions

Remove entirely:
- `DiffText`
- `ParseDiffHunks`
- `FormatDeltaResults`

Remove the `"os"`, `"os/exec"`, `"log"` imports (if no longer needed).

## 4. `format.go` — update `FormatFullResults` to show separate counts

When `len(Goals) == 0`, instead of just "Proof complete!" vs
"Sub-goal complete! N unfocused remaining.", show:

```
Proof complete!
```
when all three counts are zero, otherwise:
```
No focused goals.
  Unfocused: 3
  Shelved: 1
  Given up: 0
```

(Only show non-zero lines? Or always show all three for clarity?)

**Proposal**: show a summary line only for non-zero counts:
>> i like this. let's do it.
```
No focused goals. 3 unfocused, 1 shelved remaining.
```

When goals are present and there are unfocused/shelved/given-up, append a
summary after the goals section:
```
(+ 3 unfocused, 1 shelved)
```

## 5. `proof.go` — remove `collectResultsDelta`, update `DoStep`

- Delete `collectResultsDelta`.
- Change `DoStep` to call `collectResultsFull(doc)`.

## 6. `state.go` — remove `PrevProofView`

- Remove `PrevProofView` field from `DocState`.
- Remove `PrevProofView: &ProofView{}` initialization in `OpenDoc`.
- Remove `doc.PrevProofView = pv` assignments in `collectResultsFull`.
- Remove `doc.PrevProofView = nil` in `DoReset`.

## 7. `format_test.go` — update tests

Remove tests:
- `TestFormatDeltaResults_*` (all 7 tests)
- `TestParseDiffHunks`

Update tests:
- `TestFormatFullResults` — verify separate counts appear in output
- Add test for "no focused goals" with non-zero unfocused/shelved/given-up
- Add test for goals present + background counts summary

## 8. `testdata/diff_goal.v` — delete

No longer needed.

## 9. `cmd/proof-trace/main.go` — deduplicate formatting

Yes, it has duplicated logic:
- Goal printing (lines 119-129) duplicates `writeGoals`
- Diagnostic formatting (lines 146-165) duplicates `FormatDiagnostics`
- Unfocused count printing (line 133) needs updating for new separate counts

Replace with calls to shared functions:
- Use `FormatFullResults(pv, diags)` to get the unified text, then print it.
  Or, if proof-trace wants its own header/layout, at minimum reuse
  `writeGoals` and `FormatDiagnostics` (export them if needed).

**Approach**: Export `WriteGoals` and make proof-trace call it + `FormatDiagnostics`.
  Add a `FormatBackgroundCounts(pv)` helper that both `FormatFullResults` and
  proof-trace can use for the "3 unfocused, 1 shelved" summary.

---

## Files touched

| File | Action |
|---|---|
| `internal/rocq/types.go` | Add `ShelvedCount`, `GivenUpCount` fields |
| `internal/rocq/format.go` | Delete diff funcs, update ParseProofView + FormatFullResults |
| `internal/rocq/proof.go` | Delete `collectResultsDelta`, simplify `DoStep` |
| `internal/rocq/state.go` | Remove `PrevProofView` field and usages |
| `internal/rocq/format_test.go` | Remove delta/diff tests, add count tests |
| `testdata/diff_goal.v` | Delete |
| `cmd/proof-trace/main.go` | Check/update |
