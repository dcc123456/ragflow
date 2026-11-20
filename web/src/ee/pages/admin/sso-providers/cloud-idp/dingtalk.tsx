import { zodResolver } from '@hookform/resolvers/zod';
import { mapValues, noop } from 'lodash';
import { DefaultValues, useForm } from 'react-hook-form';
import z from 'zod';

import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
} from '@/components/ui/form';

import PasswordInput from '@/components/originui/password-input';
import { Input } from '@/components/ui/input';

import { useTranslate } from '@/hooks/common-hooks';

import { useSSOVariables } from '@/ee/pages/admin/hooks/useSSOVariables';
import SSOProviderCloudIdpCard from './Card';
import IconDingtalk from './icons/dingtalk';

const SSOProviderDingtalk = {
  key: 'dingtalk',
  Icon: IconDingtalk,
  Form: SSOProviderDingtalkForm,
  Card: SSOProviderDingtalkCard,
} as const;

function SSOProviderDingtalkForm({
  id,
  defaultValues,
  onSubmit = noop,
}: {
  id?: string;
  defaultValues?: DefaultValues<SchemaType> | (() => Promise<SchemaType>);
  onSubmit?: (data: SchemaType) => void;
}) {
  const { t } = useTranslate('admin.ssoProviderTabs.cloud_idp.dingtalk');
  const form = useForm<SchemaType>({
    resolver: zodResolver(SSOProviderDingtalkForm.schema),
    defaultValues,
  });

  return (
    <Form {...form}>
      <form
        id={id}
        className="space-y-8"
        spellCheck={false}
        autoComplete="off"
        onSubmit={form.handleSubmit((data) => onSubmit(data))}
      >
        <FormField
          control={form.control}
          name="client_id"
          render={({ field }) => (
            <FormItem>
              <FormLabel required>{t('fields.clientId')}</FormLabel>
              <FormControl>
                <Input {...field} className="h-10" />
              </FormControl>
              <FormDescription>{t('description.clientId')}</FormDescription>
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name="client_secret"
          render={({ field }) => (
            <FormItem>
              <FormLabel required>{t('fields.clientSecret')}</FormLabel>
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
          name="redirect_uri"
          render={({ field }) => (
            <FormItem>
              <FormLabel required>{t('fields.redirectUri')}</FormLabel>
              <FormControl>
                <Input
                  {...field}
                  className="h-10"
                  placeholder={t('placeholder.redirectUri')}
                />
              </FormControl>
              <FormDescription>{t('description.redirectUri')}</FormDescription>
            </FormItem>
          )}
        />
      </form>
    </Form>
  );
}

SSOProviderDingtalkForm.schema = z.object({
  client_id: z.string().min(1),
  client_secret: z.string().min(1),
  // The embedded QR widget hands the authCode to the callback URL, and
  // DingTalk requires the page rendering the QR to share the redirect's
  // origin; without the URL the login cannot come back to us.
  redirect_uri: z.string().min(1),
});

type SchemaType = z.infer<typeof SSOProviderDingtalkForm.schema>;

function SSOProviderDingtalkCard({ disabled = false }: { disabled?: boolean }) {
  const { t } = useTranslate('admin.ssoProviderTabs.cloud_idp.dingtalk');

  const {
    variables: {
      sso: { dingtalk },
    },
  } = useSSOVariables();

  return (
    <SSOProviderCloudIdpCard
      id="dingtalk"
      disabled={disabled}
      title={t('title')}
      dialogTitle={t('dialogTitle')}
      iconComponent={SSOProviderDingtalk.Icon}
      form={
        <SSOProviderDingtalkForm defaultValues={mapValues(dingtalk, 'value')} />
      }
    />
  );
}

export default SSOProviderDingtalk;
