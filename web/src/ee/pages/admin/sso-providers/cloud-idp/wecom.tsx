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
import IconWecom from './icons/wecom';

const SSOProviderWecom = {
  key: 'wecom',
  Icon: IconWecom,
  Form: SSOProviderWecomForm,
  Card: SSOProviderWecomCard,
} as const;

function SSOProviderWecomForm({
  id,
  defaultValues,
  onSubmit = noop,
}: {
  id?: string;
  defaultValues?: DefaultValues<SchemaType> | (() => Promise<SchemaType>);
  onSubmit?: (data: SchemaType) => void;
}) {
  const { t } = useTranslate('admin.ssoProviderTabs.cloud_idp.wecom');
  const form = useForm<SchemaType>({
    resolver: zodResolver(SSOProviderWecomForm.schema),
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
          name="agent_id"
          render={({ field }) => (
            <FormItem>
              <FormLabel required>{t('fields.agentId')}</FormLabel>
              <FormControl>
                <Input {...field} className="h-10" />
              </FormControl>
              <FormDescription>{t('description.agentId')}</FormDescription>
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

SSOProviderWecomForm.schema = z.object({
  client_id: z.string().min(1),
  agent_id: z.string().min(1),
  client_secret: z.string().min(1),
  // The wwlogin consent page takes the callback URL on every login; without
  // it the member is dropped on WeCom's error page instead of ours.
  redirect_uri: z.string().min(1),
});

type SchemaType = z.infer<typeof SSOProviderWecomForm.schema>;

function SSOProviderWecomCard({ disabled = false }: { disabled?: boolean }) {
  const { t } = useTranslate('admin.ssoProviderTabs.cloud_idp.wecom');

  const {
    variables: {
      sso: { wecom },
    },
  } = useSSOVariables();

  return (
    <SSOProviderCloudIdpCard
      id="wecom"
      disabled={disabled}
      title={t('title')}
      dialogTitle={t('dialogTitle')}
      iconComponent={SSOProviderWecom.Icon}
      form={<SSOProviderWecomForm defaultValues={mapValues(wecom, 'value')} />}
    />
  );
}

export default SSOProviderWecom;
