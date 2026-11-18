type ModelSharingBadgeProps = {
  tenantName?: string;
  tenantId?: string;
  currentTenantId?: string;
  userId?: string;
  nickname?: string;
  currentUserId?: string;
};

/** Shows the actual model provider owner for resources shared by a teammate. */
export function ModelSharingBadge({
  userId,
  nickname,
  currentUserId,
}: ModelSharingBadgeProps) {
  const ownerName = nickname?.trim() || userId;
  if (!userId || !currentUserId || userId === currentUserId || !ownerName) {
    return null;
  }

  return (
    <span
      title={ownerName}
      className="text-text-secondary bg-bg-card rounded-sm px-1 truncate flex-shrink-0"
    >
      {ownerName}
    </span>
  );
}
