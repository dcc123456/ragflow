import { useTranslate } from '@/hooks/common-hooks';
import { isBillingEnabled } from '@/services/billingStatus';
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

function ExposeApiFormGroup() {
  const { t: tf } = useTranslate('admin.settingsForm.exposeApi');
  const form = useFormContext<{
    exposeApi: ExposeApiFormGroup.SchemaType;
  }>();

  return (
    <FormGroup title={tf('title')} description={tf('description')}>
      <FormField
        control={form.control}
        name="exposeApi.enabled"
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

ExposeApiFormGroup.mapValuesToData = (
  formValues: ExposeApiFormGroup.SchemaType,
) => {
  return {
    'expose_api.enabled': formValues.enabled,
  } satisfies AdminService.SetVariablesInput;
};

ExposeApiFormGroup.mapValuesFromData = (
  data: AdminService.SystemVariables,
): ExposeApiFormGroup.SchemaType => {
  const value = data['expose_api.enabled']?.value;
  return {
    enabled: value === undefined ? isBillingEnabled() : !!value,
  };
};

ExposeApiFormGroup.schema = z.object({
  enabled: z.boolean().optional(),
});

ExposeApiFormGroup.defaultValues = Object.freeze({
  enabled: false,
});

// eslint-disable-next-line
namespace ExposeApiFormGroup {
  export type SchemaType = z.infer<typeof ExposeApiFormGroup.schema>;
}

export default ExposeApiFormGroup;
