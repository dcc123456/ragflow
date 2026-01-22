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
import IconFeishu from './icons/feishu';

const SSOProviderFeishu = {
  key: 'feishu',
  Icon: IconFeishu,
  Form: SSOProviderFeishuForm,
  Card: SSOProviderFeishuCard,
} as const;

function SSOProviderFeishuForm({
  id,
  defaultValues,
  onSubmit = noop,
}: {
  id?: string;
  defaultValues?: DefaultValues<SchemaType> | (() => Promise<SchemaType>);
  onSubmit?: (data: SchemaType) => void;
}) {
  const { t } = useTranslate('admin.ssoProviderTabs.cloud_idp.feishu');
  const form = useForm<SchemaType>({
    resolver: zodResolver(SSOProviderFeishuForm.schema),
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
          name="app_id"
          render={({ field }) => (
            <FormItem>
              <FormLabel required>{t('fields.appId')}</FormLabel>
              <FormControl>
                <Input {...field} className="h-10" />
              </FormControl>
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name="app_secret"
          render={({ field }) => (
            <FormItem>
              <FormLabel required>{t('fields.appSecret')}</FormLabel>
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
          name="app_access_token_url"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('fields.appAccessTokenUrl')}</FormLabel>
              <FormControl>
                <Input
                  {...field}
                  className="h-10"
                  placeholder={
                    SSOProviderFeishuForm.defaultValues.app_access_token_url
                  }
                />
              </FormControl>
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name="user_access_token_url"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('fields.userAccessTokenUrl')}</FormLabel>
              <FormControl>
                <Input
                  {...field}
                  className="h-10"
                  placeholder={
                    SSOProviderFeishuForm.defaultValues.user_access_token_url
                  }
                />
              </FormControl>
            </FormItem>
          )}
        />
      </form>
    </Form>
  );
}

SSOProviderFeishuForm.defaultValues = {
  app_access_token_url:
    'https://open.feishu.cn/open-apis/auth/v3/app_access_token/internal',
  user_access_token_url:
    'https://open.feishu.cn/open-apis/authen/v1/oidc/access_token',
};

SSOProviderFeishuForm.schema = z.object({
  app_id: z.string().min(1),
  app_secret: z.string().min(1),
  app_access_token_url: z.string(),
  user_access_token_url: z.string(),
});

type SchemaType = z.infer<typeof SSOProviderFeishuForm.schema>;

function SSOProviderFeishuCard() {
  const { t } = useTranslate('admin.ssoProviderTabs.cloud_idp.feishu');

  const {
    variables: { feishu },
  } = useSSOVariables();

  return (
    <SSOProviderCloudIdpCard
      id="feishu"
      title={t('title')}
      dialogTitle={t('dialogTitle')}
      iconComponent={SSOProviderFeishu.Icon}
      form={
        <SSOProviderFeishuForm defaultValues={mapValues(feishu, 'value')} />
      }
    />
  );
}

export default SSOProviderFeishu;
