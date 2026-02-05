import { identity, noop } from 'lodash';

import { useId, useMemo } from 'react';
import { FieldValues } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router';

import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogPortal,
  DialogTitle,
} from '@/components/ui/dialog';

import { cn } from '@/lib/utils';

import DynamicForm from '../components/dynamic-form';
import LlmHeader from '../components/llm-header';
import { getModelFields, Models } from './fields';

export type ProviderModalProps = Omit<
  React.HTMLAttributes<HTMLDivElement>,
  'onSubmit'
> & {
  llmFactory: string;
  loading?: boolean;
  open?: boolean;
  footer?: React.ReactNode;
  closable?: boolean;
  onClose?: () => void;
  onSubmit: (
    data: AdminService.AddLlmFactoryInput,
    rawData: FieldValues,
  ) => void;
};

export default function ProviderModal({
  llmFactory,
  loading = false,
  className,
  open = false,
  footer,
  closable = true,
  onClose,
  onSubmit = noop,
  ...restProps
}: ProviderModalProps) {
  const { t } = useTranslation();
  const model = (Models as any)[llmFactory];

  const formId = useId();

  const { fields, defaultValues } = useMemo(
    () => getModelFields(llmFactory),
    [llmFactory],
  );

  return (
    <Dialog
      open={open}
      onOpenChange={(open) => {
        if (!open) {
          onClose?.();
        }
      }}
    >
      <DialogPortal>
        <DialogContent
          closable={closable}
          className={cn('w-[700px] max-w-full', className)}
          onInteractOutside={(e) => e.preventDefault()}
          {...restProps}
        >
          <DialogTitle className="text-lg font-medium text-foreground w-full">
            <LlmHeader name={llmFactory} />
          </DialogTitle>

          <DialogDescription className="sr-only">
            {llmFactory}
          </DialogDescription>

          <DynamicForm.Root
            id={formId}
            fields={fields}
            defaultValues={defaultValues}
            onSubmit={(data) => {
              const {
                // @ts-ignore
                transformFieldValues = identity,
              } = Models[llmFactory as keyof typeof Models] ?? {};

              onSubmit(transformFieldValues(data), data);
            }}
          />

          {footer || (
            <footer className="flex items-center gap-4">
              {model?.userGuideLink?.href ? (
                <Link
                  to={model.userGuideLink.href}
                  target="_blank"
                  rel="noreferrer noopener"
                  className="text-sm text-text-secondary mr-8 break-words transition-colors hover:text-text-primary focus-visible:text-text-primary"
                >
                  {typeof model.userGuideLink.text === 'function'
                    ? model.userGuideLink.text(llmFactory)
                    : model.userGuideLink.text || model.userGuideLink.href}
                </Link>
              ) : null}

              <Button
                className="ml-auto h-10"
                type="button"
                variant="outline"
                disabled={loading}
                onClick={() => {
                  onClose?.();
                }}
              >
                {t('modal.cancelText')}
              </Button>

              <Button
                className="h-10"
                form={formId}
                type="submit"
                variant="default"
                loading={loading}
              >
                {t('modal.okText')}
              </Button>
            </footer>
          )}
        </DialogContent>
      </DialogPortal>
    </Dialog>
  );
}
