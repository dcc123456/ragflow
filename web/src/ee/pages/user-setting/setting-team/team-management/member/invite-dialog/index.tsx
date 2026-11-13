import { Button, ButtonLoading } from '@/components/ui/button';
import CopyToClipboard from '@/components/copy-to-clipboard';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { TagRenameId } from '@/constants/knowledge';
import { IModalProps } from '@/interfaces/common';
import { createTenantInviteLink } from '@/ee/services/tenant-invite-service';
import { formatDate } from '@/utils/date';
import { useMutation } from '@tanstack/react-query';
import { Link2 } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { InviteForm } from '@/ee/pages/user-setting/setting-team/team-management/member/invite-dialog/invite-form';

type InviteDialogProps = IModalProps<any> & { tenantId: string };

export function InviteDialog({
  hideModal,
  onOk,
  loading,
  tenantId,
}: InviteDialogProps) {
  const { t } = useTranslation();
  const [inviteLink, setInviteLink] = useState('');
  const [expiresAt, setExpiresAt] = useState('');
  const { isPending: creatingLink, mutateAsync: createLink } = useMutation({
    mutationFn: async () => {
      const { data } = await createTenantInviteLink(tenantId);

      return data;
    },
  });

  const handleCreateLink = async () => {
    const result = await createLink();
    const token = result?.data?.token;

    if (result?.code === 0 && token) {
      const base = import.meta.env.VITE_BASE_URL || '/';
      const basePath = base.endsWith('/') ? base : `${base}/`;

      setInviteLink(
        `${window.location.origin}${basePath}tenant-invitations/${encodeURIComponent(token)}`,
      );
      setExpiresAt(result.data.expires_at);
    }
  };

  return (
    <Dialog open onOpenChange={hideModal}>
      <DialogContent className="sm:max-w-[500px]">
        <DialogHeader>
          <DialogTitle>{t('setting.invite')}</DialogTitle>
        </DialogHeader>
        <div className="space-y-3 rounded-lg border border-border-button p-4">
          <div className="space-y-1">
            <h3 className="text-sm font-medium text-text-primary">
              {t('permission.inviteByLink')}
            </h3>
            <p className="text-sm text-text-secondary">
              {t('permission.inviteLinkDescription')}
            </p>
          </div>
          {inviteLink ? (
            <div className="space-y-2">
              <div className="flex min-w-0 items-center gap-2">
                <Input
                  readOnly
                  value={inviteLink}
                  aria-label={t('permission.inviteLink')}
                  className="min-w-0 flex-1 truncate"
                  title={inviteLink}
                />
                <CopyToClipboard
                  text={inviteLink}
                  aria-label={t('common.copy')}
                  title={t('common.copy')}
                  className="shrink-0"
                />
              </div>
              <p className="text-xs text-text-secondary">
                {t('permission.inviteLinkExpiresAt', {
                  date: formatDate(expiresAt),
                })}
              </p>
            </div>
          ) : (
            <Button
              type="button"
              variant="outline"
              loading={creatingLink}
              disabled={!tenantId}
              onClick={handleCreateLink}
            >
              <Link2 />
              {t('permission.createInviteLink')}
            </Button>
          )}
        </div>
        <InviteForm hideModal={hideModal} onOk={onOk}></InviteForm>
        <DialogFooter>
          <ButtonLoading type="submit" form={TagRenameId} loading={loading}>
            {t('common.save')}
          </ButtonLoading>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
