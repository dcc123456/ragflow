#!/usr/bin/env python3
"""Scan agent PROMPT files for benchmark leakage.

A prompt is a model-facing artifact: any benchmark-specific vocabulary that
reaches it - a gold answer, a rival spelling, a plot summary of the question -
is handed to the model for free and voids the measurement. This happened for
real: conf/agentic_rag.yaml carried two "Measured:" narratives naming the gold
answer (and its rival spelling) of a browsecomp question, plus a fragment of
another question's gold book title used as a punctuation example.

What is checked, all word-boundary anchored (so `verity` inside `severity` is
not a hit):

  1. every gold answer of length >= --min-gold, verbatim;
  2. every ADJACENT PAIR of tokens from a multi-word gold answer, i.e. a gold
     phrase (punctuation between the two allowed: `Laudanum, Morphine`). Single
     tokens are deliberately NOT checked: a gold like `Nothing Is Predictable
     Across ...` would otherwise flag the prompt's own ordinary vocabulary
     (`predict`, `across`, `nothing`), which drowned an earlier version of this
     script in 792 false positives;
  3. any shared --ngram-word n-gram with a question's text, reported as a
     candidate for human review (generic English such as
     "at least two of the" is expected and harmless).

Exit status is 1 when anything from 1 or 2 is found, 0 otherwise, so the
script can gate a batch run.

Usage:
    python3 scripts/check_prompt_leakage.py                    # defaults
    python3 scripts/check_prompt_leakage.py conf/*.yaml -d data/questions.jsonl
"""

from __future__ import annotations

import argparse
import glob
import itertools
import json
import re
import sys
from pathlib import Path

DEFAULT_PROMPTS = "conf/agentic_rag.yaml"
DEFAULT_DATASET = "/home/zhichyu/Downloads/data/browsecomp-plus/questions.jsonl"

# A gold answer is often a whole sentence, so its adjacent token pairs are
# mostly ordinary English (`about the`, `and the`, `the Year`). A pair is only
# worth reporting when BOTH tokens are content-sized (>= 4 letters) and they are
# not both generic: function words, plus the prompt's own domain vocabulary,
# which every prompt uses constantly. This keeps the report to pairs like
# `Zimri Elder` and `Patent Medicines` instead of the 461-pair firehose.
STOPWORDS = {
    "about",
    "above",
    "after",
    "again",
    "against",
    "almost",
    "along",
    "already",
    "also",
    "among",
    "another",
    "any",
    "anything",
    "around",
    "because",
    "been",
    "before",
    "being",
    "below",
    "between",
    "both",
    "but",
    "came",
    "come",
    "could",
    "did",
    "does",
    "doing",
    "done",
    "down",
    "during",
    "each",
    "either",
    "else",
    "even",
    "ever",
    "every",
    "everything",
    "first",
    "from",
    "further",
    "have",
    "having",
    "here",
    "however",
    "into",
    "itself",
    "just",
    "last",
    "late",
    "least",
    "less",
    "like",
    "made",
    "make",
    "many",
    "might",
    "more",
    "most",
    "much",
    "must",
    "neither",
    "never",
    "next",
    "none",
    "nothing",
    "often",
    "once",
    "only",
    "other",
    "others",
    "over",
    "own",
    "part",
    "past",
    "perhaps",
    "quite",
    "rather",
    "same",
    "several",
    "should",
    "since",
    "some",
    "something",
    "still",
    "such",
    "than",
    "that",
    "their",
    "them",
    "then",
    "there",
    "these",
    "they",
    "thing",
    "things",
    "this",
    "those",
    "though",
    "through",
    "thus",
    "time",
    "times",
    "together",
    "too",
    "toward",
    "under",
    "until",
    "upon",
    "used",
    "using",
    "very",
    "want",
    "well",
    "were",
    "what",
    "when",
    "where",
    "whether",
    "which",
    "while",
    "whose",
    "will",
    "with",
    "within",
    "without",
    "would",
    "your",
} | {
    "answer",
    "answers",
    "block",
    "blocks",
    "candidate",
    "candidates",
    "chain",
    "chains",
    "chunk",
    "chunks",
    "constraint",
    "constraints",
    "corpus",
    "corpus-absent",
    "derived",
    "document",
    "documents",
    "delivery",
    "eliminated",
    "evidence",
    "failure",
    "final",
    "form",
    "gate",
    "guessed",
    "line",
    "lines",
    "matrix",
    "node",
    "nodes",
    "opinion",
    "path",
    "plan",
    "query",
    "queries",
    "question",
    "questions",
    "reason",
    "reasoning",
    "record",
    "retained",
    "retrieval",
    "round",
    "rounds",
    "schema",
    "searched",
    "season",
    "seasons",
    "slot",
    "slots",
    "suspect",
    "suspects",
    "tested",
    "tool",
    "tools",
    "value",
    "values",
    "year",
    "years",
    "follows",
    "following",
}


def distinctive_pair(left: str, right: str) -> bool:
    """Report a gold pair only when it is not ordinary or domain vocabulary."""
    if len(left) < 4 or len(right) < 4:
        return False
    return not (left.lower() in STOPWORDS and right.lower() in STOPWORDS)


def load_questions(path: Path) -> list[dict]:
    if not path.exists():
        print(f"[leak] dataset not found: {path}", file=sys.stderr)
        return []
    rows = []
    for line in path.read_text(encoding="utf-8").splitlines():
        if line.strip():
            rows.append(json.loads(line))
    return rows


def context(text: str, start: int, match_len: int, width: int = 70) -> str:
    lo = max(0, start - width)
    hi = min(len(text), start + match_len + width)
    return re.sub(r"\s+", " ", text[lo:hi])


def scan_prompt(prompt_path: Path, questions: list[dict], min_gold: int, ngram: int) -> int:
    text = prompt_path.read_text(encoding="utf-8")
    graded = 0

    gold_hits: list[tuple[str, str, str]] = []
    token_hits: list[tuple[str, str, str]] = []
    for row in questions:
        gold = (row.get("gold_answer") or "").strip()
        if len(gold) < min_gold:
            continue
        pattern = re.compile(r"\b" + re.escape(gold) + r"\b", re.IGNORECASE)
        for m in pattern.finditer(text):
            gold_hits.append((str(row.get("id")), gold, context(text, m.start(), len(gold))))
        if len(gold.split()) < 2:
            continue
        gold_tokens = [t for t in re.findall(r"[A-Za-z][A-Za-z'\-]{2,}", gold)]
        for left, right in itertools.pairwise(gold_tokens):
            if not distinctive_pair(left, right):
                continue
            pattern = re.compile(
                r"\b" + re.escape(left) + r"[\s,;:'\-]{0,6}" + re.escape(right) + r"\b",
                re.IGNORECASE,
            )
            for m in pattern.finditer(text):
                token_hits.append((str(row.get("id")), f"{left} {right}", context(text, m.start(), len(left) + len(right))))

    if gold_hits:
        graded += len(gold_hits)
        print(f"[leak] {prompt_path}: {len(gold_hits)} gold answer(s) verbatim in the prompt")
        for qid, gold, ctx in gold_hits:
            print(f"  q{qid}: {gold!r}\n      ...{ctx}...")
    if token_hits:
        graded += len(token_hits)
        print(f"[leak] {prompt_path}: {len(token_hits)} adjacent gold token pair(s)")
        for qid, token, ctx in token_hits:
            print(f"  q{qid}: {token!r}\n      ...{ctx}...")

    words = re.findall(r"[A-Za-z0-9]+", text.lower())
    grams = {tuple(words[i : i + ngram]) for i in range(max(0, len(words) - ngram + 1))}
    review: list[tuple[str, str]] = []
    for row in questions:
        qwords = re.findall(r"[A-Za-z0-9]+", (row.get("question") or "").lower())
        for i in range(max(0, len(qwords) - ngram + 1)):
            gram = tuple(qwords[i : i + ngram])
            if gram in grams:
                review.append((str(row.get("id")), " ".join(gram)))
                break
    if review:
        print(f"[leak] {prompt_path}: {len(review)} question n-gram(s) shared with the prompt (review by hand)")
        for qid, gram in review[:20]:
            print(f"  q{qid}: {gram!r}")

    if graded == 0:
        print(f"[leak] {prompt_path}: clean (no gold answer, no gold token)")
    return graded


def main() -> int:
    parser = argparse.ArgumentParser(description="Scan prompt files for benchmark leakage")
    parser.add_argument("prompts", nargs="*", default=None, help="prompt files (default: conf/agentic_rag.yaml)")
    parser.add_argument("-d", "--dataset", default=DEFAULT_DATASET, help="benchmark questions.jsonl")
    parser.add_argument("--min-gold", type=int, default=6, help="shortest gold answer to check verbatim")
    parser.add_argument("--ngram-word", type=int, default=5, help="word n-gram size for the review list")
    args = parser.parse_args()

    prompt_args = args.prompts or [DEFAULT_PROMPTS]
    prompt_paths: list[Path] = []
    for item in prompt_args:
        found = [Path(p) for p in glob.glob(item)]
        prompt_paths.extend(found or [Path(item)])

    questions = load_questions(Path(args.dataset))
    if not questions:
        return 0

    graded = 0
    for prompt_path in prompt_paths:
        if not prompt_path.exists():
            print(f"[leak] prompt not found: {prompt_path}", file=sys.stderr)
            continue
        graded += scan_prompt(prompt_path, questions, args.min_gold, args.ngram_word)

    print(f"[leak] {len(prompt_paths)} prompt file(s), {len(questions)} questions, {graded} graded finding(s)")
    return 1 if graded else 0


if __name__ == "__main__":
    raise SystemExit(main())
