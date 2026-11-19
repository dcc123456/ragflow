# Enterprise CI fallbacks

The `merge_oss` and `oss_changes` labels are equivalent: either starts CI without
requiring `ci` and allows changes to shared Go files. Use `merge_oss` for OSS
synchronization PRs and `oss_changes` for direct changes to shared OSS code.
In preflight, `.github/actions/ee-stubs` runs this standard-library-only Go tool
against the checked-out PR merge result. It scans all candidate packages,
classifies community hook bodies, generates supported panic fallbacks and
publishes the report. Community implementations without EE counterparts produce
warnings. Confirmed empty hooks without EE counterparts fail the check after
the full scan. Only a passing check allows the existing builds and tests to run.

```bash
go run ./tools/eestub -dry-run
go run ./tools/eestub
```

The default scan covers `cmd/` and `internal/`. Positional arguments restrict
the scan to directories relative to `-root`:

```bash
go run ./tools/eestub -dry-run internal/observability internal/router internal/service
```

To compare issues against a local base commit and write the grouped report:

```bash
go run ./tools/eestub -dry-run -base-ref <base-sha> -summary /tmp/eestub-report.md
```

## Generation boundary

- Candidate declarations come from community-only `stub.go` / `*_stub.go`
  production files. Tests are excluded.
- Exported functions/methods and private names referenced by shared production
  files are candidates. Shared references are identified conservatively with
  AST identifiers, excluding resolved local names and imported selectors.
  Community-only helpers such as `router.allowAll` are excluded.
- Existing enterprise declarations are never replaced. Signatures ignore
  parameter/result names, parameter grouping and import aliases. This is a
  syntax comparison, not full type equivalence: type aliases with different
  spellings can be reported as `MISMATCH`. Existing EE bodies are not audited for
  business correctness: a constructor or intentional no-op is not a missing
  declaration merely because its body is short or empty.
- A missing EE declaration alone does not trigger generation. A community hook
  with calls, forwarding to working helpers, computation, allocation or state
  writes produces an `OSS_IMPLEMENTED` warning and no generated code.
- Recognized placeholders include empty bodies, default zero/nil/false returns,
  parameter passthrough, no-op callbacks, calls to local no-op helpers and known
  not-implemented responses/panics. These are syntactic hook defaults, not a
  general proof of business behavior. Helpers are followed with cycle detection,
  and argument-side work must not be erased by an otherwise empty helper.
- Ambiguous bodies, including pure control flow, nonzero constant returns and
  recursive helper chains without confirmed work, produce `REVIEW` warnings.
  They are never guessed to be empty and never automatically replaced.
- Only missing functions/methods in a plain `//go:build !ee` stub can be
  generated. Receivers and local signature dependencies must already exist in
  the enterprise file set. Imports used by signatures are retained; function
  bodies and body-only imports are discarded. Imported type members are left
  to the subsequent compiler to validate.
- Types, constants, variables and `init` functions are never generated.
  Generic declarations/signature types, dot imports, cgo signature types, complex stub constraints
  and declarations found under inactive build constraints are reported as
  `UNSUPPORTED`. No type layouts, values or business behavior are inferred.
- File selection uses the host Go build context with the `static` tag, common
  to `build.sh` builds and tests. Generated files require `ee`, `static`, the
  current GOOS/GOARCH and the current CGO setting. They apply only to that
  environment; this is not a cross-platform compatibility check.
- Every generated body contains only `panic("missing enterprise implementation:
  ...")`. Parameter names are removed to avoid shadowing `panic`.
- The tool creates `zz_eestub_*_ee.go` in the owning package. It only replaces
  or removes files carrying its filename prefix and generated header. A rerun
  recomputes fallbacks; adding the real implementation removes its fallback.
  Handwritten files and real implementations are not edited.

## Reports and CI behavior

`EXISTS` means a corresponding EE declaration was found with a matching signature.
`OSS_IMPLEMENTED` and `REVIEW` are warnings: no code is generated and they do not
fail the scanner. They use warning annotations and are counted separately from
blocking errors. A warning-only run ends with `PASSED WITH WARNINGS`.
`GENERATED`, `MISSING` (dry run), `MISMATCH` and `UNSUPPORTED` all fail the check,
after the remaining packages have been scanned and supported fallbacks written.
Package parsing/filesystem errors are also collected and reported while other
packages continue. The report ends with counts and a final result. Generated
files never count as real implementations, including on a repeat invocation.
Before the final failure, every generated function/method is listed again in
red and receives a GitHub Actions error annotation naming the missing implementation.

The action writes the full output to the job summary even on failure. Generated
files remain in the CI workspace and must not be committed as the fix. The check
runs once in preflight, before Lefthook and before the build/test jobs, without
`continue-on-error`. A failure blocks the dependent jobs. Adding EE declarations
or classifying a hook as a concrete community implementation removes obsolete
generated fallbacks on the next invocation. Later builds and tests still report
their actual results: a warning does not guarantee that an EE build referencing
the absent declaration can compile.

The generator is not wired into `build.sh`, push/scheduled builds or release
workflows. It does not alter the PR branch or submit code for developers.

## Source attribution

All report text is English. Each issue includes its location, expected signature,
available previous/EE signatures, generated file and source commit. `Source PR`
and `PR author` are highlighted in red in logs. The Markdown summary uses caution
blocks for blocking errors and warning blocks for warning-only groups. Logs and
summary tables group issues by source PR and show their severity separately;
locations, PRs, authors and source commits have links in the summary. No owner is
inferred from the PR author, and no investigation suggestions are generated.

For PRs labeled `merge_oss` or `oss_changes`, preflight checks out full history
and supplies the target branch's base SHA. The scanner rechecks each affected
package in that base tree to distinguish `Detected in this merge` from
`Pre-existing`. An old Stub alone
does not prove an old issue: removing or changing its EE declaration can create
a new gap. `Unknown` is used when the baseline cannot be checked.

The source commit is the latest commit touching the declaration's signature,
using whitespace-insensitive Git blame. GitHub's commit-to-PR API is queried in
the OSS repository and then the current repository, with results cached per
commit. Later enterprise synchronization PRs are not selected solely because
they contain the commit. API failures, missing history and ambiguous associations
leave the relevant fields `Unknown`; they do not interrupt the full scan or
replace its failure result. Full history and `pull-requests: read` permission
are enabled only where the preflight check needs them. The action uses the
existing GitHub token for read-only API requests.

Validate the tool through the repository's test entry point:

```bash
bash build.sh --test ./tools/eestub
```
