import { cn } from '@/lib/utils';
import { parseUnifiedDiff } from '@/ee/utils/unified-diff';

// TextDiff renders a unified diff the way the version panels show it: one row per
// line, added lines green, removed red, unchanged lines plain. The backend emits
// this format for every text comparison it reports — a chunk's history against
// itself, a dataset version against another — so the renderer lives here once and
// the panels stay consistent with each other.
function TextDiff({ diff }: { diff: string }) {
  const lines = parseUnifiedDiff(diff).flatMap((hunk) => hunk.lines);

  return (
    <div className="rounded-sm bg-bg-card/40 p-2 font-mono text-sm leading-6">
      {lines.map((line, index) => (
        <div
          key={index}
          className={cn(
            'whitespace-pre-wrap break-words px-1',
            line.type === 'added' && 'bg-[#3BA05C]/10',
            line.type === 'removed' && 'bg-[#D8494B]/10',
          )}
        >
          {line.type === 'added' ? '+' : line.type === 'removed' ? '-' : ' '}
          {line.content}
        </div>
      ))}
    </div>
  );
}

export default TextDiff;
