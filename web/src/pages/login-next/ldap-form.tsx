import { zodResolver } from '@hookform/resolvers/zod';
import { useForm } from 'react-hook-form';
import z from 'zod';

import { Checkbox } from '@/components/ui/checkbox';
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
import { cn } from '@/lib/utils';

export interface LDAPLoginFormState {
  username: string;
  password: string;
  ldap_server: string;
  remember: boolean;
}

interface IProps {
  id?: string;
  onSubmit: (data: LDAPLoginFormState) => void;
}

function LoginWithLDAPForm({ id, onSubmit }: IProps) {
  const { t } = useTranslate('login');

  const schema = z.object({
    username: z.string().min(1, { message: t('usernamePlaceholder') }),
    password: z.string().min(1, { message: t('passwordPlaceholder') }),
    ldap_server: z.string().min(1, { message: t('ldapServerPlaceholder') }),
    remember: z.boolean().optional(),
  });

  const form = useForm({
    defaultValues: {
      username: '',
      password: '',
      ldap_server: '',
      remember: false,
    },
    resolver: zodResolver(schema),
  });

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
                {/* TODO: subject to change */}
                {/* <Select
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
                    <SelectValue
                      placeholder={t('ldapServerPlaceholder')}
                    />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="unknown">
                      Default
                    </SelectItem>
                  </SelectContent>
                </Select> */}

                <Input
                  {...field}
                  className="h-10"
                  placeholder={t('ldapServerPlaceholder')}
                />
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
              <FormControl>
                <div className="flex gap-2">
                  <Checkbox
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />

                  <FormLabel
                    className={cn('hover:text-text-primary', {
                      'text-text-disabled': !field.value,
                      'text-text-primary': field.value,
                    })}
                  >
                    {t('rememberMe')}
                  </FormLabel>
                </div>
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
      </form>
    </Form>
  );
}

export default LoginWithLDAPForm;
