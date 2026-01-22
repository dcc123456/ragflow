import { mapValues, noop } from 'lodash';

import { zodResolver } from '@hookform/resolvers/zod';
import { DefaultValues, useForm } from 'react-hook-form';
import z from 'zod';

import { useId } from 'react';

import { LucidePlus, LucideSettings, LucideTrash2 } from 'lucide-react';

import { Button } from '@/components/ui/button';
import { Card, CardContent } from '@/components/ui/card';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
} from '@/components/ui/form';
import { Switch } from '@/components/ui/switch';

import PasswordInput from '@/components/originui/password-input';
import { Input } from '@/components/ui/input';
import { useSetModalState, useTranslate } from '@/hooks/common-hooks';
import { cn } from '@/lib/utils';
import {
  type SSOLDAPData,
  useAddLdapServer,
  useMutateLdapServer,
  useSSOVariables,
} from '../hooks/useSSOVariables';

export function SSOProviderLDAPForm({
  id,
  defaultValues,
  onSubmit = noop,
}: {
  id?: string;
  defaultValues?: DefaultValues<SchemaType> | (() => Promise<SchemaType>);
  onSubmit?: (data: SchemaType) => void;
}) {
  const { t } = useTranslate('admin.ssoProviderTabs.ldap');

  const form = useForm<SchemaType>({
    resolver: zodResolver(SSOProviderLDAPForm.schema),
    defaultValues,
  });

  return (
    <Form {...form}>
      <form
        id={id}
        className="space-y-8"
        spellCheck={false}
        autoComplete="off"
        onSubmit={form.handleSubmit((data) =>
          onSubmit({
            ...data,
            // Use default values if not provided
            search_filter:
              data.search_filter ||
              SSOProviderLDAPForm.defaultValues.search_filter,
            attribute_list:
              data.attribute_list ||
              SSOProviderLDAPForm.defaultValues.attribute_list,
          }),
        )}
      >
        <FormField
          control={form.control}
          name="name"
          render={({ field }) => (
            <FormItem>
              <FormLabel required>{t('fields.name')}</FormLabel>
              <FormControl>
                <Input {...field} className="h-10" />
              </FormControl>
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name="url"
          render={({ field }) => (
            <FormItem>
              <FormLabel required>{t('fields.url')}</FormLabel>
              <FormControl>
                <Input {...field} className="h-10" />
              </FormControl>
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name="dn"
          render={({ field }) => (
            <FormItem>
              <FormLabel required>{t('fields.dn')}</FormLabel>
              <FormControl>
                <Input {...field} className="h-10" />
              </FormControl>
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name="password"
          render={({ field }) => (
            <FormItem>
              <FormLabel required>{t('fields.password')}</FormLabel>
              <FormControl>
                <PasswordInput
                  {...field}
                  className="h-10"
                  placeholder=""
                  autoComplete="new-password"
                />
              </FormControl>
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name="search_filter"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('fields.searchFilter')}</FormLabel>
              <FormControl>
                <Input
                  {...field}
                  className="h-10"
                  placeholder={SSOProviderLDAPForm.defaultValues.search_filter}
                />
              </FormControl>
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name="attribute_list"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('fields.attributeList')}</FormLabel>
              <FormControl>
                <Input
                  {...field}
                  className="h-10"
                  placeholder={SSOProviderLDAPForm.defaultValues.attribute_list}
                />
              </FormControl>
            </FormItem>
          )}
        />
      </form>
    </Form>
  );
}

SSOProviderLDAPForm.schema = z.object({
  name: z.string().min(1),
  url: z.string().min(1),
  dn: z.string().min(1),
  password: z.string().min(1),
  search_filter: z.string().optional(),
  attribute_list: z.string().optional(),
});

SSOProviderLDAPForm.defaultValues = {
  search_filter: '(|(userPrincipalName={username})(mail={username}))',
  attribute_list:
    'cn,mail,uid,givenName,sn,jpegPhoto,sAMAccountName,userPrincipalName',
};

export type SchemaType = z.infer<typeof SSOProviderLDAPForm.schema>;

function SSOProviderLDAPAddButton() {
  const { t } = useTranslate('admin.ssoProviderTabs.ldap');
  const { t: tCommon } = useTranslate('common');
  const { visible, showModal, hideModal } = useSetModalState();

  const mutation = useAddLdapServer();

  const _formId = useId();

  return (
    <>
      <Button
        variant="transparent"
        size="icon"
        className="w-full h-10 border-dashed"
        onClick={showModal}
      >
        <LucidePlus />
        <span>{tCommon('add')}</span>
      </Button>

      <Dialog open={visible} onOpenChange={hideModal}>
        <DialogContent closeDisabled={mutation.isAdding}>
          <DialogHeader>
            <DialogTitle>{t('addDialogTitle')}</DialogTitle>
          </DialogHeader>

          <DialogDescription className="sr-only">
            {t('addDialogTitle')}
          </DialogDescription>

          <div className="px-6">
            <SSOProviderLDAPForm
              id={_formId}
              onSubmit={async (data) => {
                await mutation.add(data);
                hideModal();
              }}
              defaultValues={SSOProviderLDAPForm.defaultValues}
            />
          </div>

          <DialogFooter className="px-6 py-4">
            <Button
              className="px-4 h-10"
              variant="outline"
              onClick={hideModal}
              disabled={mutation.isAdding}
            >
              {tCommon('cancel')}
            </Button>

            <Button
              form={_formId}
              type="submit"
              className="px-4 h-10"
              loading={mutation.isAdding}
            >
              {tCommon('add')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

type ListItemProps = {
  id: string;
  data: SSOLDAPData;
};

function SSOProviderLDAPListItem(props: ListItemProps) {
  const { id, data } = props;

  // Default LDAP server cannot be deleted
  const noDelete = id === 'default';
  const title = data?.name.value;

  const { t } = useTranslate('admin.ssoProviderTabs.ldap');
  const { t: tCommon } = useTranslate('common');

  const { visible, showModal, hideModal } = useSetModalState();
  const mutation = useMutateLdapServer(id);

  const _formId = useId();

  return (
    <Card className={cn('bg-transparent', mutation.isDeleting && 'opacity-50')}>
      <CardContent className="p-4">
        <div className="flex items-center gap-4">
          <span className="text-base">
            {title || <i className="text-text-disabled">&lt;{id}&gt;</i>}
          </span>

          <div className="ml-auto flex items-center gap-2">
            <Switch
              className="mr-4"
              checked={!!data.enabled.value}
              disabled={mutation.isSwitchingState}
              onCheckedChange={async (checked) => {
                mutation[checked ? 'enable' : 'disable']();
              }}
            />

            <Button
              className="size-8 border-none"
              variant="transparent"
              size="icon"
              onClick={showModal}
              disabled={mutation.isUpdating || mutation.isDeleting}
            >
              <LucideSettings />
            </Button>

            {!noDelete && (
              <Button
                className="size-8 border-none"
                variant="danger"
                size="icon"
                onClick={() => mutation.delete()}
                loading={mutation.isDeleting}
              >
                <LucideTrash2 />
              </Button>
            )}
          </div>
        </div>
      </CardContent>

      <Dialog open={visible} onOpenChange={hideModal}>
        <DialogContent closeDisabled={mutation.isUpdating}>
          <DialogHeader>
            <DialogTitle>{t('editDialogTitle')}</DialogTitle>
          </DialogHeader>

          <DialogDescription className="sr-only">
            {t('editDialogTitle')}
          </DialogDescription>

          <div className="px-6">
            <SSOProviderLDAPForm
              id={_formId}
              defaultValues={mapValues(data, 'value')}
              onSubmit={async (data) => {
                await mutation.update(data);
                hideModal();
              }}
            />
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
              form={_formId}
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

export default function SSOProviderLDAPList() {
  const {
    variables: { ldap },
  } = useSSOVariables();

  const { default: defaultLdap, ...restLdap } = ldap;

  return (
    <ul className="list-none space-y-2">
      {defaultLdap && (
        <SSOProviderLDAPListItem
          key="default"
          id="default"
          data={defaultLdap}
        />
      )}

      {Object.entries(restLdap).map(([id, data]) => (
        <SSOProviderLDAPListItem key={id} id={id} data={data} />
      ))}

      <li>
        <SSOProviderLDAPAddButton />
      </li>
    </ul>
  );
}
