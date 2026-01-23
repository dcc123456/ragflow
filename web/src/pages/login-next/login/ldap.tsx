import { zodResolver } from '@hookform/resolvers/zod';
import { t } from 'i18next';
import { useCallback, useId } from 'react';
import { useForm } from 'react-hook-form';
import z from 'zod';

import { Button } from '@/components/ui/button';
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form';
import { Input } from '@/components/ui/input';

import { useTranslate } from '@/hooks/common-hooks';
import {
  useLoginChannels,
  useLoginWithChannel,
} from '@/hooks/use-login-request';
import { rsaPsw } from '@/utils';

import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import RememberMeCheckbox from '../components/RememberMeCheckbox';

export interface LDAPLoginFormState {
  username: string;
  password: string;
  ldap_server: string;
  remember: boolean;
}

const schema = z.object({
  username: z.string().min(1, { message: t('login.usernamePlaceholder') }),
  password: z.string().min(1, { message: t('login.passwordPlaceholder') }),
  ldap_server: z.string().min(1, { message: t('login.ldapServerPlaceholder') }),
  remember: z.boolean().optional(),
});

type SchemaType = z.infer<typeof schema>;

function LdapLogin() {
  const id = useId();
  const { t } = useTranslate('login');
  const { typeGroupedChannels } = useLoginChannels();

  const { loginLdap: loginWithChannel, loading: loginLoading } =
    useLoginWithChannel();

  const form = useForm<SchemaType>({
    resolver: zodResolver(schema),
    defaultValues: {
      username: '',
      password: '',
      ldap_server: typeGroupedChannels.ldap?.[0]?.channel || '',
      remember: false,
    },
  });

  const onSubmit = useCallback(
    async (data: SchemaType) => {
      try {
        const rsaPassword = rsaPsw(data.password) as string;

        return await loginWithChannel({
          serverName: data.ldap_server,
          username: data.username,
          password: rsaPassword,
        });
      } catch (error) {
        console.error('Login failed:', error);
      }
    },
    [loginWithChannel],
  );

  return (
    <Form {...form}>
      <form
        id={id}
        className="space-y-8 text-text-primary"
        onSubmit={form.handleSubmit(onSubmit)}
      >
        <FormField
          control={form.control}
          name="username"
          render={({ field }) => (
            <FormItem>
              <FormLabel required>{t('usernameLabel')}</FormLabel>
              <FormControl>
                <Input
                  className="h-10"
                  placeholder={t('usernamePlaceholder')}
                  autoComplete="username"
                  {...field}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name="password"
          render={({ field }) => (
            <FormItem>
              <FormLabel required>{t('passwordLabel')}</FormLabel>
              <FormControl>
                <div className="relative">
                  <Input
                    className="h-10"
                    type={'password'}
                    placeholder={t('passwordPlaceholder')}
                    autoComplete="current-password"
                    {...field}
                  />
                </div>
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name="ldap_server"
          render={({ field }) => (
            <FormItem>
              <FormLabel required>{t('ldapServerLabel')}</FormLabel>
              <FormControl>
                <Select
                  name={field.name}
                  value={field.value}
                  onValueChange={field.onChange}
                  disabled={field.disabled}
                >
                  <SelectTrigger
                    ref={field.ref}
                    className="h-10"
                    disabled={field.disabled}
                    onBlur={field.onBlur}
                  >
                    <SelectValue placeholder={t('ldapServerPlaceholder')} />
                  </SelectTrigger>
                  <SelectContent>
                    {typeGroupedChannels.ldap?.map((ch) => (
                      <SelectItem value={ch.channel} key={ch.channel}>
                        {ch.display_name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name="remember"
          render={({ field }) => (
            <FormItem>
              <FormLabel>
                <FormControl>
                  <RememberMeCheckbox
                    {...field}
                    checked={!!field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </FormLabel>
              <FormMessage />
            </FormItem>
          )}
        />

        <Button
          type="submit"
          variant="metallic"
          loading={loginLoading}
          block
          className="!mt-12 h-10"
        >
          {t('login')}
        </Button>
      </form>
    </Form>
  );
}

export default LdapLogin;
