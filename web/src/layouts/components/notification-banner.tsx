import { useFetchNotification } from '@/hooks/use-notification-request';
import { LucideX } from 'lucide-react';
import { useTranslation } from 'react-i18next';

export function NotificationBanner() {
  const { t } = useTranslation();
  const { data: notification, isVisible, dismiss } = useFetchNotification();

  if (!isVisible || !notification) {
    return null;
  }

  return (
    <div className="w-full bg-[rgb(0,190,180,0.1)] h-16 px-4 py-2 flex items-center justify-between">
      <span className="text-base text-[#00BEB4] w-full text-center">
        {notification.content}
      </span>
      <button
        onClick={dismiss}
        className="p-1 hover:bg-bg-card rounded-full transition-colors"
        aria-label={t('common.close') || 'Close'}
      >
        <LucideX className="size-4 text-text-primary" />
      </button>
    </div>
  );
}
