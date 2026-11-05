const DISMISSED_NOTIFICATION_KEY = 'dismissed_notification_id';

export const getDismissedNotificationId = (): string | null =>
  localStorage.getItem(DISMISSED_NOTIFICATION_KEY);

export const setDismissedNotificationId = (id: string): void => {
  localStorage.setItem(DISMISSED_NOTIFICATION_KEY, id);
};
