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
import IconGithub from './icons/github';

const SSOProviderGithub = {
  key: 'github',
  Icon: IconGithub,
  Form: SSOProviderGithubForm,
  Card: SSOProviderGithubCard,
} as const;

function SSOProviderGithubForm({
  id,
  defaultValues,
  onSubmit = noop,
}: {
  id?: string;
  defaultValues?: DefaultValues<SchemaType> | (() => Promise<SchemaType>);
  onSubmit?: (data: SchemaType) => void;
}) {
  const { t } = useTranslate('admin.ssoProviderTabs.cloud_idp.github');
  const form = useForm<SchemaType>({
    resolver: zodResolver(SSOProviderGithubForm.schema),
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
      </form>
    </Form>
  );
}

SSOProviderGithubForm.schema = z.object({
  client_id: z.string().min(1),
  client_secret: z.string().min(1),
});

type SchemaType = z.infer<typeof SSOProviderGithubForm.schema>;

function SSOProviderGithubCard({ disabled = false }: { disabled?: boolean }) {
  const { t } = useTranslate('admin.ssoProviderTabs.cloud_idp.github');

  const {
    variables: {
      sso: { github },
    },
  } = useSSOVariables();

  return (
    <SSOProviderCloudIdpCard
      id="github"
      disabled={disabled}
      title={t('title')}
      dialogTitle={t('dialogTitle')}
      iconComponent={SSOProviderGithub.Icon}
      form={
        <SSOProviderGithubForm defaultValues={mapValues(github, 'value')} />
      }
    />
  );
}

export default SSOProviderGithub;
