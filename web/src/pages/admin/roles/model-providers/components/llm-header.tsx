import { LlmIcon } from '@/components/svg-icon';
import { Button } from '@/components/ui/button';
import { APIMapUrl } from '@/constants/llm';

import { LucideArrowUpRight } from 'lucide-react';
import { Link } from 'react-router';

export default function LlmHeader({ name }: { name: string }) {
  return (
    <div className="pr-16 flex items-center">
      <LlmIcon name={name} imgClass="flex-none size-8 text-text-primary" />

      <div className="ml-4 flex-1 w-0 flex items-center">
        <span className="flex-grow-0 font-normal text-base truncate">
          {name}
        </span>

        {APIMapUrl[name as keyof typeof APIMapUrl] && (
          <Link
            to={APIMapUrl[name as keyof typeof APIMapUrl]}
            target="_blank"
            rel="noopener noreferrer"
            className="ml-2 self-center"
          >
            <Button
              variant="ghost"
              className="flex bg-transparent size-[1.5em]"
            >
              <LucideArrowUpRight size={16} />
            </Button>
          </Link>
        )}
      </div>
    </div>
  );
}
