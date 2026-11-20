---
sidebar_label: Sensitive word filtering
title: Sensitive word filtering
---

# Sensitive word filtering

In the Enterprise Admin UI, open **Settings** and configure **Sensitive Word Filtering**.
Upload one UTF-8 `.txt` file of at most 30 MiB, with one word or phrase per line.
Blank lines are ignored; duplicate lines are allowed. UTF-8 files with a BOM are accepted.
Enable the filter and optionally customize the response returned for restricted questions.
The default is disabled.

The Go API checks chat questions, search answers, dataset searches, retrieval tests,
Dify retrieval, and agent queries before retrieval or generation. It uses the configured
RAG tokenizer and matches sequences of up to five tokens, both space-separated and
joined without spaces, against trimmed, lowercased entries in the uploaded file.
The dictionary entries themselves are not tokenized. With Infinity, the tokenizer
passes the input through, so matching follows the input's case and spaces.

Restricted searches and retrieval requests succeed with empty results and the configured
rejection text in `message`. Search answers complete with an empty answer and references,
with the rejection text in the final answer event's `message`. Chat and agent requests
return the configured rejection text as a normal response, without an `**ERROR**` prefix or error event.
Rejected requests do not invoke retrieval, model generation, or workflow nodes.

Settings are refreshed on requests at most once every 60 seconds in each API process.
The file is reloaded when its version changes or when filtering is enabled again.
If a reload fails, the process retains its last successfully loaded dictionary and retries
on the next refresh. Until a dictionary has loaded successfully, requests pass through.

Administrators can upload with `POST /api/v1/admin/sensitive-words-file`
(multipart field `file`) and download with `GET /api/v1/admin/sensitive-words-file`.
Both endpoints require administrator authentication. The file is stored in the configured
object storage at `ragflow-system/sensitive_word_filter/compliance.txt`.
The existing settings API manages `sensitive_word_filter.enabled` and
`sensitive_word_filter.response`; uploads update `sensitive_word_filter.file_version`
and `sensitive_word_filter.file_name` automatically.
