import { useTranslate } from '@/hooks/common-hooks';
import { useFormContext } from 'react-hook-form';

import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form';
import { Switch } from '@/components/ui/switch';
import { Textarea } from '@/components/ui/textarea';
import z from 'zod';
import FormGroup from './FormGroup';

function NotificationSettingsFormGroup() {
  const { t: tf } = useTranslate('admin.settingsForm.notification');
  const form = useFormContext<{
    notification: NotificationSettingsFormGroup.SchemaType;
  }>();

  return (
    <FormGroup title={tf('title')} description={tf('description')}>
      <FormField
        control={form.control}
        name="notification.content"
        render={({ field }) => (
          <FormItem className="space-y-0 contents">
            <FormLabel className="h-10 flex items-center">
              {tf('fields.content')}
            </FormLabel>

            <div className="relative">
              <FormControl>
                <Textarea
                  {...field}
                  className="m-0 min-h-[6rem] resize-none overflow-auto"
                  placeholder={tf('placeholder.content')}
                  disabled={field.disabled}
                />
              </FormControl>

              <FormMessage className="absolute top-full" />
            </div>
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="notification.enabled"
        render={({ field }) => (
          <FormItem className="space-y-0 contents">
            <FormLabel className="h-10 flex items-center">
              {tf('fields.enableModule')}
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
    </FormGroup>
  );
}

NotificationSettingsFormGroup.mapValuesToData = (
  formValues: NotificationSettingsFormGroup.SchemaType,
) => {
  return {
    'notification.enabled': formValues.enabled,
    'notification.content': formValues.content,
  } satisfies AdminService.SetVariablesInput;
};

NotificationSettingsFormGroup.mapValuesFromData = (
  data: AdminService.SystemVariables,
): NotificationSettingsFormGroup.SchemaType => {
  const enabled = !!data['notification.enabled']?.value;
  const content = data['notification.content']?.value;
  return {
    enabled: enabled,
    content: typeof content === 'string' ? content : '',
  };
};

NotificationSettingsFormGroup.schema = z.object({
  enabled: z.boolean().optional(),
  content: z.string(),
});

NotificationSettingsFormGroup.defaultValues = Object.freeze({
  enabled: false,
  content: '',
});

// eslint-disable-next-line
namespace NotificationSettingsFormGroup {
  export type SchemaType = z.infer<typeof NotificationSettingsFormGroup.schema>;
}

export default NotificationSettingsFormGroup;
