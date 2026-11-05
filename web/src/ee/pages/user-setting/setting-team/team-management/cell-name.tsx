import { Avatar, AvatarFallback, AvatarImage } from '@/components/ui/avatar';
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip';
import { cn } from '@/lib/utils';

export function CellAvatar({ avatar }: { avatar?: string }) {
  return (
    <Avatar className="size-7">
      <AvatarImage
        src={avatar || 'https://github.com/shadcn.png'}
        alt="@shadcn"
      />
      <AvatarFallback>CN</AvatarFallback>
    </Avatar>
  );
}

interface CellNameProps {
  name: string;
  avatar?: string;
}

export function CellName({ avatar, name }: CellNameProps) {
  return (
    <div className="flex items-center gap-2">
      <CellAvatar avatar={avatar}></CellAvatar>
      <span className={cn('truncate')}>{name}</span>
    </div>
  );
}

export function CellNameWithToolTip({ name, avatar }: CellNameProps) {
  return (
    <Tooltip>
      <CellName name={name} avatar={avatar}></CellName>
      <TooltipTrigger asChild></TooltipTrigger>
      <TooltipContent>
        <p>{name}</p>
      </TooltipContent>
    </Tooltip>
  );
}
