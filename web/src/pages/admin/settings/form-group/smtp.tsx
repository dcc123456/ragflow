import { useTranslate } from '@/hooks/common-hooks';
import { useFormContext } from 'react-hook-form';
import z from 'zod';

import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form';

import PasswordInput from '@/components/originui/password-input';
// import { Button } from '@/components/ui/button';
import { Input, NumberInput } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import { t } from 'i18next';
// import { LucideUnplug } from 'lucide-react';

import FormGroup from './FormGroup';

/*
function useSMTPTestConnection() {
  const form = useFormContext<{ smtp: SMTPSettingsFormGroup.SchemaType }>();
  const {

  } = useQuery({
    queryKey: ['admin/config/smtpTestConnection'],
    queryFn: () => {
    },
  });
}
*/

function SMTPSettingsFormGroup() {
  const { t: tf } = useTranslate('admin.settingsForm.smtp');
  const form = useFormContext<{ smtp: SMTPSettingsFormGroup.SchemaType }>();

  return (
    <FormGroup title={tf('title')} description={tf('description')}>
      <FormField
        control={form.control}
        name="smtp.server"
        render={({ field }) => (
          <FormItem className="space-y-0 contents">
            <FormLabel required className="h-10 flex items-center">
              {tf('fields.server')}
            </FormLabel>

            <div className="relative">
              <FormControl>
                <Input
                  {...field}
                  className="m-0 h-10"
                  placeholder={tf('placeholder.server')}
                />
              </FormControl>

              <FormMessage className="absolute top-full" />
            </div>
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="smtp.port"
        render={({ field }) => (
          <FormItem className="space-y-0 contents">
            <FormLabel required className="h-10 flex items-center">
              {tf('fields.port')}
            </FormLabel>

            <div className="relative">
              <FormControl>
                <NumberInput {...field} className="m-0 h-10" />
              </FormControl>

              <FormMessage className="absolute top-full" />
            </div>
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="smtp.timeout"
        render={({ field }) => (
          <FormItem className="space-y-0 contents">
            <FormLabel required className="h-10 flex items-center">
              {tf('fields.timeout')}
            </FormLabel>

            <div className="relative">
              <FormControl>
                <NumberInput {...field} className="m-0 h-10" />
              </FormControl>

              <FormMessage className="absolute top-full" />
            </div>
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="smtp.username"
        render={({ field }) => (
          <FormItem className="space-y-0 contents">
            <FormLabel required className="h-10 flex items-center">
              {tf('fields.username')}
            </FormLabel>

            <div className="relative">
              <FormControl>
                <Input
                  {...field}
                  type="email"
                  className="m-0 h-10"
                  placeholder={tf('placeholder.username')}
                  autoComplete="email"
                />
              </FormControl>

              <FormMessage className="absolute top-full" />
            </div>
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="smtp.password"
        render={({ field }) => (
          <FormItem className="space-y-0 contents">
            <FormLabel required className="h-10 flex items-center">
              {tf('fields.password')}
            </FormLabel>

            <div className="relative">
              <FormControl>
                <PasswordInput
                  {...field}
                  className="m-0 h-10"
                  placeholder=""
                  autoComplete="new-password"
                />
              </FormControl>

              <FormMessage className="absolute top-full" />
            </div>
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="smtp.defaultSender"
        render={({ field }) => (
          <FormItem className="space-y-0 contents">
            <FormLabel required className="h-10 flex items-center">
              {tf('fields.defaultSender')}
            </FormLabel>

            <div className="relative">
              <FormControl>
                <Input
                  {...field}
                  type="email"
                  className="m-0 h-10"
                  placeholder={tf('placeholder.defaultSender')}
                  autoComplete="email"
                />
              </FormControl>

              <FormMessage className="absolute top-full" />
            </div>
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="smtp.ssl"
        render={({ field }) => (
          <FormItem className="space-y-0 contents">
            <FormLabel className="h-10 flex items-center">
              {tf('fields.ssl')}
            </FormLabel>

            <div className="flex justify-end items-center">
              <FormControl>
                <Switch
                  ref={field.ref}
                  name={field.name}
                  checked={!!field.value}
                  onCheckedChange={field.onChange}
                  disabled={field.disabled}
                  onBlur={field.onBlur}
                />
              </FormControl>
            </div>
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="smtp.tls"
        render={({ field }) => (
          <FormItem className="space-y-0 contents">
            <FormLabel className="h-10 flex items-center">
              {tf('fields.tls')}
            </FormLabel>

            <div className="flex justify-end items-center">
              <FormControl>
                <Switch
                  ref={field.ref}
                  name={field.name}
                  checked={!!field.value}
                  onCheckedChange={field.onChange}
                  disabled={field.disabled}
                  onBlur={field.onBlur}
                />
              </FormControl>
            </div>
          </FormItem>
        )}
      />

      {/* Feature not supported yet */}
      {/* <div className="col-span-2 text-right">
        <Button type="button" variant="outline" className="h-10 px-4">
          <LucideUnplug />
          {tf('testConnection')}
        </Button>
      </div> */}
    </FormGroup>
  );
}

SMTPSettingsFormGroup.mapValuesFromData = (
  data: AdminService.VariableDictionary,
): SMTPSettingsFormGroup.SchemaType => {
  return {
    server: data['mail.server']?.value,
    port: data['mail.port']?.value as number,
    timeout: data['mail.timeout']?.value as number,
    username: data['mail.username']?.value,
    password: data['mail.password']?.value,
    defaultSender: data['mail.default_sender']?.value,
    ssl: data['mail.use_ssl']?.value,
    tls: data['mail.use_tls']?.value,
  };
};

SMTPSettingsFormGroup.mapValuesToData = (
  formValues: SMTPSettingsFormGroup.SchemaType,
): AdminService.SetVariablesInput => {
  return {
    'mail.server': formValues.server,
    'mail.port': formValues.port,
    'mail.timeout': formValues.timeout,
    'mail.username': formValues.username,
    'mail.password': formValues.password,
    'mail.default_sender': formValues.defaultSender,
    'mail.use_ssl': formValues.ssl,
    'mail.use_tls': formValues.tls,
  };
};

SMTPSettingsFormGroup.defaultValues = Object.freeze({
  server: '',
  port: 465,
  timeout: 30,
  username: '',
  password: '',
  defaultSender: '',
  ssl: true,
  tls: false,
});

SMTPSettingsFormGroup.schema = z.object({
  server: z.string().min(1, t('admin.settingsForm.smtp.messages.server')),
  port: z
    .number()
    .int(t('admin.settingsForm.smtp.messages.port'))
    .min(1, t('admin.settingsForm.smtp.messages.port'))
    .max(65535, t('admin.settingsForm.smtp.messages.port')),
  timeout: z.number().min(0, t('admin.settingsForm.smtp.messages.timeout')),
  username: z.string().min(1, t('admin.settingsForm.smtp.messages.username')),
  password: z.string().min(1, t('admin.settingsForm.smtp.messages.password')),
  defaultSender: z
    .string()
    .min(1, t('admin.settingsForm.smtp.messages.defaultSender')),
  ssl: z.boolean().optional(),
  tls: z.boolean().optional(),
});

// eslint-disable-next-line
namespace SMTPSettingsFormGroup {
  export type SchemaType = z.infer<typeof SMTPSettingsFormGroup.schema>;
}

export default SMTPSettingsFormGroup;
