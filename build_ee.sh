#!/usr/bin/env bash
# EE (enterprise) build extensions, sourced by build.sh.
#
# This file holds ONLY EE-specific build/test logic so that the OSS build.sh body
# stays untouched and the merge-conflict surface against the OSS branch is
# limited to a single `source` line plus one guarded hook call. Keep this file
# free of executable side effects at source time: it must only DEFINE functions.
#
# The OSS branch (ragflow main) ships an empty stub of this file plus the
# `source` line and guarded hook in build.sh; the EE build (ragflow_enterprise
# saas) fills it with the actual logic below.

# Run the EE-deployment native integration/parity tests. Called from inside
# run_native_integration_tests() in build.sh (after the OSS native sections) via
# the guarded hook, so it inherits that function's scope (native_run_filter,
# PROJECT_ROOT, CGO_LDFLAGS, CGO_LDFLAGS_EE, EE_MODEL_DIR, print_section, go, ...).
run_ee_native_integration_tests() {
    # EE-deployment parity: the same native package, with the EE weights and
    # DLA post-processor activated at runtime via InitCPUEE (see eeActivate in
    # native_ee_test.go) and the EE-only integration tests (ocr_rec_integration_ee_test.go,
    # tsr_*_ee_test.go). These tests self-skip unless RUN_EE_TESTS=1 AND EE_DEEPDOC=1
    # AND EE_MODEL_DIR point at the EE model dir (see checkEE), so an OSS-only CI
    # box simply skips them. The EE DLA golden (testdata/<stem>.dla.ee.golden.json)
    # is the EE docker's own wire output — never the OSS golden. EE_MODEL_DIR is a
    # dedicated variable, fully separate from the OSS MODEL_DIR (which holds the
    # OSS *.ort weights) — the two weight sets are different files by design.
    # CGO_LDFLAGS_EE is optional: when CI does not inject an EE-specific link
    # line, fall back to the OSS CGO_LDFLAGS that check_go_deps already built
    # with -Wl,--undefined=OrtGetApiBase -Wl,--dynamic-list. Without that flag
    # the statically-linked ORT drops OrtGetApiBase and the EE inference tests
    # fail at runtime with "Error looking up OrtGetApiBase". The EE and OSS
    # backends share the same minimal patch2 ORT, so the fallback is equivalent.
    # NOTE: this comment MUST stay OUTSIDE the backslash-continued command
    # below. A '#' placed after a trailing '\' terminates that logical line,
    # which would drop every env assignment above it (RUN_EE_TESTS,
    # EE_MODEL_DIR, RAGFLOW_TESTDATA_REF) from `go test` and silently skip all
    # EE tests — a false-green gate.
    print_section "Running EE-deployment native integration tests (RUN_EE_TESTS=1)"
    ( cd "$PROJECT_ROOT" && \
      GOPROXY=${GOPROXY:-https://goproxy.cn,https://proxy.golang.org,direct} \
      CGO_ENABLED=1 \
      RUN_EE_TESTS=1 EE_DEEPDOC=1 \
      EE_MODEL_DIR="${EE_MODEL_DIR:-/home/infiniflow/runners_work/artifacts/infiniflow-ai/ee_weights}" \
      RAGFLOW_TESTDATA_REF=deepdoc-native-testdata-v2-ee \
      CGO_LDFLAGS="${CGO_LDFLAGS_EE:-$CGO_LDFLAGS}" \
      go test -tags "cgo static integration fetch_testdata${EXTRA_TAGS_SPACE}" -count=1 \
      "${native_run_filter[@]}" ./internal/deepdoc/native/... )
}
