import { LucidePlus } from 'lucide-react';
import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';

import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { cn } from '@/lib/utils';

export function SSOProviderAddButton({
  children,
  disabled,
  onClick,
}: {
  children: ReactNode;
  disabled?: boolean;
  onClick: () => void;
}) {
  return (
    <Button
      type="button"
      variant="transparent"
      size="icon"
      className="h-10 w-full border-dashed"
      disabled={disabled}
      onClick={onClick}
    >
      <LucidePlus />
      <span>{children}</span>
    </Button>
  );
}

export function SSOProviderFormDialog({
  open,
  onClose,
  title,
  description,
  formId,
  submitLabel,
  pending,
  className,
  bodyClassName,
  children,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  description?: string;
  formId: string;
  submitLabel: string;
  pending: boolean;
  className?: string;
  bodyClassName?: string;
  children: ReactNode;
}) {
  const { t } = useTranslation();

  const handleOpenChange = (nextOpen: boolean) => {
    if (!nextOpen && !pending) {
      onClose();
    }
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent closable={false} className={className}>
        <DialogHeader>
          <DialogTitle className="break-words">{title}</DialogTitle>
          <DialogDescription
            className={description ? 'text-text-secondary' : 'sr-only'}
          >
            {description || title}
          </DialogDescription>
        </DialogHeader>
        <div className={cn('px-6', bodyClassName)}>{children}</div>
        <DialogFooter className="px-6 py-4">
          <Button
            type="button"
            className="h-10 px-4"
            variant="outline"
            onClick={onClose}
            disabled={pending}
          >
            {t('common.cancel')}
          </Button>
          <Button
            form={formId}
            type="submit"
            className="h-10 px-4"
            loading={pending}
            disabled={pending}
          >
            {submitLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
