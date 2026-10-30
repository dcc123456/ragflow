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

function ExposeModelProviderFormGroup() {
  const { t: tf } = useTranslate('admin.settingsForm.exposeModelProvider');
  const form = useFormContext<{
    exposeModelProvider: ExposeModelProviderFormGroup.SchemaType;
  }>();

  return (
    <FormGroup title={tf('title')} description={tf('description')}>
      <FormField
        control={form.control}
        name="exposeModelProvider.enabled"
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

ExposeModelProviderFormGroup.mapValuesToData = (
  formValues: ExposeModelProviderFormGroup.SchemaType,
) => {
  return {
    'expose_model_provider.enabled': formValues.enabled,
  } satisfies AdminService.SetVariablesInput;
};

ExposeModelProviderFormGroup.mapValuesFromData = (
  data: AdminService.SystemVariables,
): ExposeModelProviderFormGroup.SchemaType => {
  return {
    enabled: !!data['expose_model_provider.enabled']?.value,
  };
};

ExposeModelProviderFormGroup.schema = z.object({
  enabled: z.boolean().optional(),
});

ExposeModelProviderFormGroup.defaultValues = Object.freeze({
  enabled: false,
});

// eslint-disable-next-line
namespace ExposeModelProviderFormGroup {
  export type SchemaType = z.infer<typeof ExposeModelProviderFormGroup.schema>;
}

export default ExposeModelProviderFormGroup;
