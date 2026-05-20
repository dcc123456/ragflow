import notificationService from '@/services/notification-service';
import storage from '@/utils/authorization-util';
import { useQuery } from '@tanstack/react-query';
import { md5 } from 'js-md5';
import { useCallback, useState } from 'react';

export const enum NotificationApiAction {
  GetNotification = 'getNotification',
}

export interface NotificationPayload {
  id: string;
  content: string;
  enabled: boolean;
}

export const useFetchNotification = () => {
  const [dismissedId, setDismissedId] = useState<string | null>(() =>
    storage.getDismissedNotificationId(),
  );

  const { data, isFetching: loading } = useQuery({
    queryKey: [NotificationApiAction.GetNotification],
    gcTime: 0,
    queryFn: async () => {
      try {
        const { data: responseData } =
          await notificationService.getNotification();
        if (responseData?.code === 0) {
          const data = {
            ...responseData.data,
            id: md5(responseData.data.content),
          };
          return data as NotificationPayload;
        }
        return null;
      } catch {
        return null;
      }
    },
  });

  const notification = data ?? null;

  // Show if: notification exists, enabled is true, and either no dismissal or new id
  const isVisible =
    !!notification &&
    notification.enabled &&
    (!dismissedId || dismissedId !== notification.id);

  const dismiss = useCallback(() => {
    if (notification?.id) {
      storage.setDismissedNotificationId(notification.id);
      setDismissedId(notification.id);
    }
  }, [notification?.id]);

  return { data: notification, loading, isVisible, dismiss };
};
