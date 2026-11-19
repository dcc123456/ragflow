/**
 * Parser for the standard unified diff (`---`/`+++`/`@@` headers, ` ` / `+` /
 * `-` lines) produced by the backend's `utility.UnifiedDiff`. It is shared by
 * the wiki version diff and the chunk version diff so both render the same
 * format with the same code. Enterprise only: the version panels are the only
 * readers, and upstream keeps no such history.
 */

export type UnifiedDiffLineType = 'added' | 'removed' | 'context';

export interface UnifiedDiffLine {
  type: UnifiedDiffLineType;
  content: string;
}

export interface UnifiedDiffHunk {
  header: string;
  lines: UnifiedDiffLine[];
}

export function parseUnifiedDiff(diff: string): UnifiedDiffHunk[] {
  const rawLines = (diff || '').split('\n');
  const hunks: UnifiedDiffHunk[] = [];
  let currentHunk: UnifiedDiffHunk | null = null;

  for (const rawLine of rawLines) {
    // A real diff line always carries its one-character prefix - a context line
    // is " " plus the text - so an empty string can only be the blank left by
    // the trailing newline. Keeping it renders a row of nothing between two
    // fields that are otherwise adjacent.
    if (rawLine === '') {
      continue;
    }

    if (rawLine.startsWith('@@')) {
      if (currentHunk) {
        hunks.push(currentHunk);
      }
      currentHunk = { header: rawLine, lines: [] };
      continue;
    }

    if (rawLine.startsWith('---') || rawLine.startsWith('+++')) {
      // File header — not rendered per-line.
      continue;
    }

    let type: UnifiedDiffLineType = 'context';
    let content = rawLine;

    if (rawLine.startsWith('+')) {
      type = 'added';
      content = rawLine.slice(1);
    } else if (rawLine.startsWith('-')) {
      type = 'removed';
      content = rawLine.slice(1);
    }

    if (currentHunk) {
      currentHunk.lines.push({ type, content });
    }
  }

  if (currentHunk) {
    hunks.push(currentHunk);
  }

  return hunks;
}
