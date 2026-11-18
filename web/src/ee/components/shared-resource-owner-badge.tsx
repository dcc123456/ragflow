import type { PropsWithChildren } from 'react';

type SharedResourceOwnerBadgeProps = PropsWithChildren<{
  userId?: string;
  currentUserId?: string;
  currentUserNickname?: string;
  ownerNickname?: string;
}>;

/** Shows the resource owner's name returned with the shared resource. */
export function SharedResourceOwnerBadge({
  userId,
  currentUserId,
  ownerNickname,
}: SharedResourceOwnerBadgeProps) {
  const showOwner = !!userId && !!currentUserId && userId !== currentUserId;
  if (!showOwner) return null;

  const ownerName = ownerNickname?.trim() || userId;

  return (
    <span
      title={ownerName}
      className="inline-block max-w-[120px] truncate align-middle bg-bg-card rounded-sm px-1 text-xs"
    >
      {ownerName}
    </span>
  );
}
