import accountService from '@/ee/services/account-service';
import storage, { redirectToLogin } from '@/utils/authorization-util';
import { useMutation, useQueryClient } from '@tanstack/react-query';

export function useDeleteAccount() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (confirmationEmail: string) => {
      const { data } = await accountService.deleteOwnAccount({
        confirmation_email: confirmationEmail,
      });
      if (data.code !== 0) {
        throw new Error(data.message);
      }
    },
    onSuccess: () => {
      storage.removeAll();
      queryClient.clear();
      redirectToLogin();
    },
  });
}
