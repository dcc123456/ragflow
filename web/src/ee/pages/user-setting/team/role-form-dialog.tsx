import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';

export function RoleFormDialog({
  roleName: initialRoleName = '',
  description: initialDescription = '',
  canEditRoleName = true,
  loading,
  onClose,
  onSubmit,
}: {
  roleName?: string;
  description?: string | null;
  canEditRoleName?: boolean;
  loading: boolean;
  onClose: () => void;
  onSubmit: (values: { roleName: string; description: string }) => void;
}) {
  const { t } = useTranslation();
  const [roleName, setRoleName] = useState(initialRoleName);
  const [description, setDescription] = useState(initialDescription ?? '');
  const editing = !!initialRoleName;

  return (
    <Dialog open onOpenChange={(value) => !value && onClose()}>
      <DialogContent className="sm:max-w-[480px]">
        <DialogHeader>
          <DialogTitle>
            {t(editing ? 'permission.editRole' : 'permission.createRole')}
          </DialogTitle>
        </DialogHeader>
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault();
            onSubmit({
              roleName: roleName.trim(),
              description: description.trim(),
            });
          }}
        >
          {canEditRoleName && (
            <div className="space-y-2">
              <Label htmlFor="role-name">{t('admin.roleName')}</Label>
              <Input
                id="role-name"
                autoComplete="off"
                required
                maxLength={64}
                value={roleName}
                onChange={(event) => setRoleName(event.target.value)}
              />
            </div>
          )}
          <div className="space-y-2">
            <Label htmlFor="role-description">{t('admin.description')}</Label>
            <Textarea
              id="role-description"
              rows={3}
              value={description}
              onChange={(event) => setDescription(event.target.value)}
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              {t('common.cancel')}
            </Button>
            <Button
              type="submit"
              loading={loading}
              disabled={
                canEditRoleName &&
                (!roleName.trim() || roleName.trim().length > 64)
              }
            >
              {t('common.save')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
