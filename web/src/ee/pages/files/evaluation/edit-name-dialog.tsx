import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';

interface EditNameDialogProps {
  visible: boolean;
  onCancel: () => void;
  onOk: (newName: string) => void;
  initialName: string;
}

export function EditNameDialog({
  visible,
  onCancel,
  onOk,
  initialName,
}: EditNameDialogProps) {
  const { t } = useTranslation();
  const [name, setName] = useState(initialName);
  const [error, setError] = useState('');

  const handleOk = () => {
    if (!name.trim()) {
      setError(t('common.namePlaceholder'));
      return;
    }
    onOk(name.trim());
    setError('');
  };

  const handleCancel = () => {
    setError('');
    onCancel();
  };

  return (
    <Dialog open={visible} onOpenChange={onCancel}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t('fileManager.evaluation.editName')}</DialogTitle>
        </DialogHeader>
        <div className="py-4">
          <div className="space-y-2">
            <label className="text-sm font-medium">
              {t('fileManager.name')}
            </label>
            <Input
              value={name}
              onChange={(e) => {
                setName(e.target.value);
                if (error) setError('');
              }}
              placeholder={t('common.namePlaceholder')}
              className={error ? 'border-destructive' : ''}
            />
            {error && <p className="text-sm text-destructive mt-1">{error}</p>}
          </div>
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={handleCancel}>
            {t('common.cancel')}
          </Button>
          <Button onClick={handleOk}>{t('common.ok')}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
