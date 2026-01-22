import { Card, CardContent } from '@/components/ui/card';

import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';

import { Button } from '@/components/ui/button';
import { Switch } from '@/components/ui/switch';

import { useSetModalState, useTranslate } from '@/hooks/common-hooks';
import { LucideSettings } from 'lucide-react';
import { cloneElement, useId } from 'react';
import {
  useMutateIdpProvider,
  useSSOVariables,
} from '../../hooks/useSSOVariables';

type CardProps = {
  id: AdminService.SystemVariables.SSO.IDP.ProviderId;
  title: string;
  dialogTitle: string;
  iconComponent?: (props?: any) => React.ReactNode;
  form: React.ReactElement;
};

export default function SSOProviderCloudIdpCard(props: CardProps) {
  const { id, title, dialogTitle, iconComponent: Icon, form } = props;

  const { t: tCommon } = useTranslate('common');

  const { visible, showModal, hideModal } = useSetModalState();
  const { variables } = useSSOVariables();
  const mutation = useMutateIdpProvider(id);

  const _formId = useId();
  const formId = form?.props?.id || _formId;

  return (
    <Card className="bg-transparent">
      <CardContent className="p-4">
        <div className="flex items-center gap-4">
          {Icon && <Icon className="size-[1.5em] fill-text-primary" />}
          <span className="text-base">{title}</span>

          <div className="ml-auto flex items-center gap-6">
            <Switch
              checked={!!variables[id]?.enabled.value}
              onCheckedChange={(checked) => {
                mutation[checked ? 'enable' : 'disable']();
              }}
            />

            <Button
              variant="transparent"
              size="icon"
              className="border-none size-8"
              onClick={() => showModal()}
            >
              <LucideSettings />
            </Button>
          </div>
        </div>
      </CardContent>

      <Dialog open={visible} onOpenChange={hideModal}>
        <DialogContent closeDisabled={mutation.isUpdating}>
          <DialogHeader>
            <DialogTitle>{dialogTitle}</DialogTitle>
          </DialogHeader>

          <DialogDescription className="sr-only">
            {dialogTitle}
          </DialogDescription>

          <div className="px-6">
            {cloneElement(form, {
              ...form.props,
              id: formId,
              onSubmit: async (data: any) => {
                await form.props?.onSubmit?.(data);
                await mutation.update(data);
                hideModal();
              },
            })}
          </div>

          <DialogFooter className="px-6 py-4">
            <Button
              className="px-4 h-10"
              variant="outline"
              onClick={hideModal}
              disabled={mutation.isUpdating}
            >
              {tCommon('cancel')}
            </Button>

            <Button
              form={formId}
              type="submit"
              className="px-4 h-10"
              loading={mutation.isUpdating}
            >
              {tCommon('confirm')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  );
}
