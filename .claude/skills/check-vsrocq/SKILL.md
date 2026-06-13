---
name: check-vsrocq
description: Audit the vsrocq Go wrapper against upstream vsrocq source. Use when you want to know if the wrapper is out of date, missing endpoints, or has protocol mismatches.
argument-hint: "[git-ref]"
---

Check whether the Go wrapper in `vsrocq/` is up-to-date with the upstream vsrocq LSP server.

## Steps

1. **Fetch the latest vsrocq protocol sources from GitHub** using ref `$ARGUMENTS` if provided, otherwise `main`:
   - `https://raw.githubusercontent.com/rocq-prover/vsrocq/$ARGUMENTS/language-server/protocol/extProtocol.ml` — all custom request/notification types (default ref: `main`)
   - `https://raw.githubusercontent.com/rocq-prover/vsrocq/$ARGUMENTS/language-server/protocol/settings.ml` — initializationOptions schema
   - `https://raw.githubusercontent.com/rocq-prover/vsrocq/$ARGUMENTS/language-server/protocol/proofState.ml` — goal/proof state types
   - `https://raw.githubusercontent.com/rocq-prover/vsrocq/$ARGUMENTS/language-server/protocol/printing.ml` — Pp/Ppcmd types

   If `$ARGUMENTS` is empty, use `main` as the ref.

2. **Read the current wrapper** files `vsrocq/protocol.go` and `vsrocq/client.go`.

3. **Compare and report** across three dimensions:

   ### A. Custom requests (`prover/*`)
   Extract every `| "prover/..."` branch from `extProtocol.ml` (both `Request.Client.t_of_jsonrpc` and `Notification.Client.of_jsonrpc`). For each one, check:
   - Is the params type defined in `protocol.go`?
   - Is the result type (from `yojson_of_result`) defined in `protocol.go`?
   - Is there a method on `Client` in `client.go`?

   ### B. Settings schema (`settings.ml`)
   Walk every record type in `settings.ml`. For each field, check:
   - Is the field present in the corresponding Go struct in `protocol.go`?
   - If the OCaml field is `T option` **without** `[@yojson.option]`, is the Go tag free of `omitempty`? (Critical correctness invariant: omitting such a field causes a silent parse error with no response.)
   - If the OCaml field has `[@default ...]`, does Go have a matching default in `DefaultInitOptions()`?

   ### C. Notification payload types
   Extract every `type t` from `extProtocol.ml` notification params. Check that the corresponding Go struct fields and JSON tags match.

4. **Output a structured report**:
   - **Missing from wrapper**: endpoints/types in upstream not in our code
   - **Possibly stale**: fields that exist in both but may have drifted (type changes, new optional fields, renamed variants)
   - **omitempty violations**: Go fields tagged `omitempty` that correspond to OCaml `T option` without `[@yojson.option]`
   - **Up to date**: brief confirmation of what was verified correct
