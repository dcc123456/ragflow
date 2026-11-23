import PasswordInput from '@/components/originui/password-input';
import { Button } from '@/components/ui/button';
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form';
import { Modal } from '@/components/ui/modal/modal';
import { CurrentUserInfoContext } from '@/pages/admin/layouts/root-layout';
import { Routes } from '@/routes';
import { rsaPsw } from '@/utils';
import { adminStorage } from '@/utils/authorization-util';
import { zodResolver } from '@hookform/resolvers/zod';
import { isAxiosError } from 'axios';
import { Loader2Icon, PenLine } from 'lucide-react';
import { useContext, useState } from 'react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router';
import { z } from 'zod';
import { changeOwnPassword } from '@/ee/services/admin-service';

export function ChangePasswordDialog({
  open: controlledOpen,
  onOpenChange,
}: {
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
}) {
  const { t } = useTranslation();
  const [internalOpen, setInternalOpen] = useState(false);
  const open = controlledOpen ?? internalOpen;
  const setOpen = onOpenChange ?? setInternalOpen;
  const [, setCurrentUserInfo] = useContext(CurrentUserInfoContext);
  const navigate = useNavigate();
  const schema = z
    .object({
      currentPassword: z.string().min(1, t('setting.currentPasswordMessage')),
      newPassword: z.string().refine((password) => [...password].length >= 8, {
        message: t('setting.newPasswordDescription'),
      }),
      confirmPassword: z.string().min(1, t('setting.confirmPasswordMessage')),
    })
    .refine(
      ({ newPassword, confirmPassword }) => newPassword === confirmPassword,
      {
        message: t('setting.confirmPasswordNonMatchMessage'),
        path: ['confirmPassword'],
      },
    );
  const form = useForm<z.infer<typeof schema>>({
    resolver: zodResolver(schema),
    defaultValues: {
      currentPassword: '',
      newPassword: '',
      confirmPassword: '',
    },
  });
  const { errors, isSubmitting } = form.formState;

  function handleOpenChange(nextOpen: boolean) {
    if (isSubmitting) return;
    if (!nextOpen) form.reset();
    setOpen(nextOpen);
  }

  function handleEdit() {
    handleOpenChange(true);
  }

  function handleCancel() {
    handleOpenChange(false);
  }

  function showError(message?: string) {
    form.setError('root', {
      message: message || t('admin.changeOwnPassword.failed'),
    });
  }

  const handleSubmit = form.handleSubmit(
    async ({ currentPassword, newPassword }) => {
      const encryptedCurrentPassword = rsaPsw(currentPassword);
      const encryptedNewPassword = rsaPsw(newPassword);
      if (!encryptedCurrentPassword || !encryptedNewPassword) {
        showError(t('admin.changeOwnPassword.encryptionFailed'));
        return;
      }

      try {
        const result = await changeOwnPassword(
          encryptedCurrentPassword,
          encryptedNewPassword,
        );
        if (result.code !== 0) {
          showError(result.message);
          return;
        }
      } catch (error) {
        showError(
          isAxiosError<{ message?: string }>(error)
            ? error.response?.data?.message
            : undefined,
        );
        return;
      }

      form.reset();
      setOpen(false);
      adminStorage.removeAll();
      setCurrentUserInfo({ userInfo: null, source: null });
      navigate(Routes.Admin, { replace: true });
    },
  );

  return (
    <>
      {controlledOpen === undefined && (
        <div className="flex items-start gap-4">
          <span className="w-[190px] text-sm font-medium">
            {t('setting.password')}
          </span>
          <div className="flex-1 flex items-center gap-4">
            <div className="text-sm text-text-primary border border-border-button flex-1 rounded-md py-1.5 px-2">
              <span className="inline-block translate-y-0.5">********</span>
            </div>
            <Button variant="outline" type="button" onClick={handleEdit}>
              <PenLine size={12} /> {t('setting.edit')}
            </Button>
          </div>
        </div>
      )}
      <Modal
        title={t('setting.editPassword')}
        open={open}
        showfooter={false}
        maskClosable={false}
        closable={!isSubmitting}
        titleClassName="text-base"
        onOpenChange={handleOpenChange}
        className="!w-[480px]"
      >
        <Form {...form}>
          <form
            className="flex flex-col mt-6 mb-8 ml-2 space-y-6"
            onSubmit={handleSubmit}
          >
            <FormField
              control={form.control}
              name="currentPassword"
              render={({ field }) => (
                <FormItem className="items-center space-y-0">
                  <div className="flex flex-col w-full gap-2">
                    <FormLabel
                      required
                      className="text-sm flex text-text-secondary whitespace-nowrap"
                    >
                      {t('setting.currentPassword')}
                    </FormLabel>
                    <FormControl className="w-full">
                      <PasswordInput
                        {...field}
                        placeholder={t('setting.currentPassword')}
                        autoComplete="current-password"
                        className="bg-bg-input border-border-default"
                      />
                    </FormControl>
                  </div>
                  <div className="flex w-full pt-1">
                    <FormMessage />
                  </div>
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="newPassword"
              render={({ field }) => (
                <FormItem className="items-center space-y-0">
                  <div className="flex flex-col w-full gap-2">
                    <FormLabel
                      required
                      className="text-sm text-text-secondary whitespace-nowrap"
                    >
                      {t('setting.newPassword')}
                    </FormLabel>
                    <FormControl className="w-full">
                      <PasswordInput
                        {...field}
                        placeholder={t('setting.newPassword')}
                        autoComplete="new-password"
                        className="bg-bg-input border-border-default"
                      />
                    </FormControl>
                  </div>
                  <div className="flex w-full pt-1">
                    <FormMessage />
                  </div>
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="confirmPassword"
              render={({ field }) => (
                <FormItem className="items-center space-y-0">
                  <div className="flex flex-col w-full gap-2">
                    <FormLabel
                      required
                      className="text-sm text-text-secondary whitespace-nowrap"
                    >
                      {t('setting.confirmPassword')}
                    </FormLabel>
                    <FormControl className="w-full">
                      <PasswordInput
                        {...field}
                        placeholder={t('setting.confirmPassword')}
                        className="bg-bg-input border-border-default"
                        autoComplete="new-password"
                      />
                    </FormControl>
                  </div>
                  <div className="flex w-full pt-1">
                    <FormMessage />
                  </div>
                </FormItem>
              )}
            />
            {errors.root?.message && (
              <p className="text-sm text-state-error" role="alert">
                {errors.root.message}
              </p>
            )}
            <div className="w-full text-right space-x-4 !mt-11">
              <Button
                type="reset"
                variant="secondary"
                onClick={handleCancel}
                disabled={isSubmitting}
              >
                {t('setting.cancel')}
              </Button>
              <Button type="submit" disabled={isSubmitting}>
                {isSubmitting && <Loader2Icon className="animate-spin" />}
                {t('common.save')}
              </Button>
            </div>
          </form>
        </Form>
      </Modal>
    </>
  );
}
