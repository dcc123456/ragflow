import { zodResolver } from '@hookform/resolvers/zod';
import { mapValues, noop } from 'lodash';
import { DefaultValues, useForm } from 'react-hook-form';
import z from 'zod';

import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
} from '@/components/ui/form';

import PasswordInput from '@/components/originui/password-input';
import { Input } from '@/components/ui/input';

import { useTranslate } from '@/hooks/common-hooks';

import { useSSOVariables } from '../../hooks/useSSOVariables';
import SSOProviderCloudIdpCard from './Card';
import IconGoogle from './icons/google';

const SSOProviderGoogle = {
  key: 'google',
  Icon: IconGoogle,
  Form: SSOProviderGoogleForm,
  Card: SSOProviderGoogleCard,
} as const;

function SSOProviderGoogleForm({
  id,
  defaultValues,
  onSubmit = noop,
}: {
  id?: string;
  defaultValues?: DefaultValues<SchemaType> | (() => Promise<SchemaType>);
  onSubmit?: (data: SchemaType) => void;
}) {
  const { t } = useTranslate('admin.ssoProviderTabs.cloud_idp.google');
  const form = useForm<SchemaType>({
    resolver: zodResolver(SSOProviderGoogleForm.schema),
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
              <FormLabel>{t('fields.redirectUri')}</FormLabel>
              <FormControl>
                <Input
                  {...field}
                  className="h-10"
                  placeholder={t('placeholder.redirectUri')}
                />
              </FormControl>
            </FormItem>
          )}
        />
      </form>
    </Form>
  );
}

SSOProviderGoogleForm.schema = z.object({
  client_id: z.string().min(1),
  client_secret: z.string().min(1),
  redirect_uri: z.string(),
});

type SchemaType = z.infer<typeof SSOProviderGoogleForm.schema>;

function SSOProviderGoogleCard() {
  const { t } = useTranslate('admin.ssoProviderTabs.cloud_idp.google');
  const {
    variables: { google },
  } = useSSOVariables();

  return (
    <SSOProviderCloudIdpCard
      id="google"
      title={t('title')}
      dialogTitle={t('dialogTitle')}
      iconComponent={SSOProviderGoogle.Icon}
      form={
        <SSOProviderGoogleForm defaultValues={mapValues(google, 'value')} />
      }
    />
  );
}

export default SSOProviderGoogle;
