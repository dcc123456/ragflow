import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form';
import { NumberInput } from '@/components/ui/input';
import { useFormContext } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import z from 'zod';
import FormGroup from './FormGroup';

function ApiKeyFormGroup() {
  const { t } = useTranslation(undefined, {
    keyPrefix: 'admin.settingsForm.apiKey',
  });
  const form = useFormContext<{ apiKey: ApiKeyFormGroup.SchemaType }>();

  return (
    <FormGroup title={t('title')} description={t('description')}>
      <FormField
        control={form.control}
        name="apiKey.maxCount"
        render={({ field }) => (
          <FormItem className="space-y-0 contents">
            <FormLabel required className="min-h-10 flex items-center">
              {t('maxCount')}
            </FormLabel>
            <div>
              <FormControl>
                <NumberInput {...field} min={1} step={1} className="h-10" />
              </FormControl>
              <FormMessage />
            </div>
          </FormItem>
        )}
      />
    </FormGroup>
  );
}

ApiKeyFormGroup.schema = z.object({
  maxCount: z.number().int().positive(),
});

ApiKeyFormGroup.mapValuesFromData = (
  data: AdminServiceEe.SystemVariables,
): ApiKeyFormGroup.SchemaType => ({
  maxCount: data['api_key.max_count']?.value ?? 16,
});

ApiKeyFormGroup.mapValuesToData = (values: ApiKeyFormGroup.SchemaType) =>
  ({
    'api_key.max_count': values.maxCount,
  }) satisfies AdminServiceEe.SetVariablesInput;

namespace ApiKeyFormGroup {
  export type SchemaType = z.infer<typeof ApiKeyFormGroup.schema>;
}

export default ApiKeyFormGroup;
