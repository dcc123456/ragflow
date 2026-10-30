import { useTranslate } from '@/hooks/common-hooks';
import { useFormContext } from 'react-hook-form';

import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
} from '@/components/ui/form';
import { Switch } from '@/components/ui/switch';
import z from 'zod';
import FormGroup from './FormGroup';

function EmailVerificationFormGroup() {
  const { t: tf } = useTranslate('admin.settingsForm.emailVerification');
  const form = useFormContext<{
    emailVerification: EmailVerificationFormGroup.SchemaType;
  }>();

  return (
    <FormGroup title={tf('title')} description={tf('description')}>
      <FormField
        control={form.control}
        name="emailVerification.enabled"
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

EmailVerificationFormGroup.mapValuesToData = (
  formValues: EmailVerificationFormGroup.SchemaType,
) => {
  return {
    'email_verification.enabled': formValues.enabled,
  } satisfies AdminService.SetVariablesInput;
};

EmailVerificationFormGroup.mapValuesFromData = (
  data: AdminService.SystemVariables,
): EmailVerificationFormGroup.SchemaType => {
  return {
    enabled: !!data['email_verification.enabled']?.value,
  };
};

EmailVerificationFormGroup.schema = z.object({
  enabled: z.boolean().optional(),
});

EmailVerificationFormGroup.defaultValues = Object.freeze({
  enabled: false,
});

// eslint-disable-next-line
namespace EmailVerificationFormGroup {
  export type SchemaType = z.infer<typeof EmailVerificationFormGroup.schema>;
}

export default EmailVerificationFormGroup;
