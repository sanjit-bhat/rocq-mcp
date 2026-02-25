# Plan: `cmd/proof-trace` — debug utility to step through a .v file

## Goal

A standalone binary that opens a `.v` file in vsrocqtop, steps through every
sentence, and prints the proof state at each step. For debugging what vsrocqtop
actually returns.

## What it prints at each step

For each `stepForward`:

1. **Sentence text** — extracted from file content using `prover/moveCursor`
   position deltas. Track the previous cursor position; after each step, the
   new moveCursor position tells us where the prover advanced to. The sentence
   is the text between old and new positions.
2. **All focused goals** — each with full hypotheses and conclusion (using
   existing `renderGoalText`).
3. **Counts** for shelved, given-up, and unfocused goals (already in
   `UnfocusedCount`).
4. **Messages** — from the `messages` and `pp_messages` fields.
5. **Diagnostics** — severity, range, message.

## No changes to existing code

The existing `ProofView` struct and `parseProofView` already capture everything
we need. Focused goals are fully rendered, and the other categories are counted.
The trace binary just uses the existing infrastructure directly.

## Design

### File: `cmd/proof-trace/main.go`

A single-file `main` package in `cmd/proof-trace/`.

Reuses existing infrastructure:
- `newVsrocqClient` + `initialize` + `shutdown` — subprocess lifecycle
- `lspCodec` — framing
- Notification handlers from `stateManager` — proofView, diagnostics
- `parseProofView`, `renderPpcmd`, `renderGoalText` — rendering

### The binary

```
Usage: proof-trace [vsrocqtop flags...] <file.v>
```

Flow:
1. Parse args: last arg is the `.v` file, everything before is vsrocqtop flags.
2. Create `stateManager` with the extra args.
3. `openDoc(file)`.
4. Register a handler for `prover/moveCursor` to track cursor position.
5. Loop: `stepForward`, wait for proofView + diagnostics + moveCursor.
   - Extract sentence text from file content (between old and new cursor pos).
   - Print proof state.
   - Stop when we get no proofView and no cursor movement (end of file).
6. `closeDoc` + `shutdown`.

### Output format

```
=== Step 1 ===
> intros A B C HA HB HC.

Focused Goals (1):
Goal 1:
  A : Prop
  B : Prop
  C : Prop
  HA : A
  HB : B
  HC : C
  ────────────────────
  (A /\ B) /\ C

Unfocused: 0  Shelved: 0  Given-up: 0
Messages: (none)
Diagnostics: (none)

=== Step 2 ===
> assert (HAB : A /\ B).
...
```

### moveCursor tracking

The `prover/moveCursor` notification currently has a no-op handler in
`state.go`. We need to:
- Add a `cursorCh chan Position` to `docState` (like proofViewCh/diagnosticCh).
- Update the `prover/moveCursor` handler to parse the position and send it.
- In the trace loop, receive from cursorCh after each step.

This is a small change to `state.go` and `types.go` (add channel to docState).

### Termination

Step forward until we stop receiving moveCursor notifications (timeout). When
vsrocqtop has nothing left to step through, it simply doesn't respond to
stepForward, so a timeout (e.g. 2s) is the natural termination signal.

## Implementation checklist

- [ ] Add `cursorCh` to `docState` and wire up `prover/moveCursor` handler
- [ ] Create `cmd/proof-trace/main.go`
- [ ] Run existing tests to verify nothing breaks
- [ ] Manual test with `testdata/complex_goal_flow.v`
