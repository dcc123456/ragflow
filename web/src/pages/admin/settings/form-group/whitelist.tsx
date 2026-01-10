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

function WhitelistSettingsFormGroup() {
  const { t: tf } = useTranslate('admin.settingsForm.whitelist');
  const form = useFormContext<{
    whitelist: WhitelistSettingsFormGroup.SchemaType;
  }>();

  return (
    <FormGroup title={tf('title')} description={tf('description')}>
      <FormField
        control={form.control}
        name="whitelist.enabled"
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

WhitelistSettingsFormGroup.mapValuesToData = (
  formValues: WhitelistSettingsFormGroup.SchemaType,
): AdminService.SetVariablesInput => {
  return {
    enable_whitelist: formValues.enabled,
  };
};

WhitelistSettingsFormGroup.mapValuesFromData = (
  data: AdminService.VariableDictionary,
): WhitelistSettingsFormGroup.SchemaType => {
  return {
    enabled: !!data.enable_whitelist?.value,
  };
};

WhitelistSettingsFormGroup.schema = z.object({
  enabled: z.boolean().optional(),
});

WhitelistSettingsFormGroup.defaultValues = Object.freeze({
  enabled: false,
});

// eslint-disable-next-line
namespace WhitelistSettingsFormGroup {
  export type SchemaType = z.infer<typeof WhitelistSettingsFormGroup.schema>;
}

export default WhitelistSettingsFormGroup;
