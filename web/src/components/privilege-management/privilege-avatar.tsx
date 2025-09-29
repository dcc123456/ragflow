import { cn } from '@/lib/utils';
import { Avatar, AvatarFallback, AvatarImage } from '../ui/avatar';

export function PrivilegeAvatar({
  avatar,
  className,
}: {
  avatar?: string;
  className?: string;
}) {
  return (
    <Avatar className={cn('size-7', className)}>
      <AvatarImage
        src={avatar || 'https://github.com/shadcn.png'}
        alt="@shadcn"
      />
      <AvatarFallback>CN</AvatarFallback>
    </Avatar>
  );
}
