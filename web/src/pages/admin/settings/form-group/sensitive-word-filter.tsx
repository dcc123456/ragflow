import { useTranslate } from '@/hooks/common-hooks';
import {
  downloadSensitiveWordFilter,
  uploadSensitiveWordFilter,
} from '@/services/admin-service';
import { downloadFileFromBlob } from '@/utils/file-util';
import { Download } from 'lucide-react';
import { useState } from 'react';
import { useFormContext } from 'react-hook-form';
import { toast } from 'sonner';
import z from 'zod';

import { Button } from '@/components/ui/button';
import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form';
import { Switch } from '@/components/ui/switch';
import { Textarea } from '@/components/ui/textarea';
import FormGroup from './FormGroup';

const MAX_FILE_SIZE = 30 * 1024 * 1024;
const DEFAULT_RESPONSE =
  'Your request could not be processed because it contains restricted content. Please modify your input and try again.';

function SensitiveWordFilterFormGroup() {
  const { t: tf } = useTranslate('admin.settingsForm.sensitiveWordFilter');
  const form = useFormContext<{
    sensitiveWordFilter: SensitiveWordFilterFormGroup.SchemaType;
  }>();
  const [isDownloading, setIsDownloading] = useState(false);

  return (
    <FormGroup title={tf('title')} description={tf('description')}>
      <FormField
        control={form.control}
        name="sensitiveWordFilter.response"
        render={({ field }) => (
          <FormItem className="space-y-0 contents">
            <FormLabel className="h-10 flex items-center">
              {tf('fields.response')}
            </FormLabel>

            <div className="relative">
              <FormControl>
                <Textarea
                  {...field}
                  className="m-0 min-h-[6rem] resize-none overflow-auto"
                  placeholder={tf('placeholder.response')}
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
        name="sensitiveWordFilter.file"
        render={({ field }) => (
          <FormItem className="space-y-0 contents">
            <FormLabel className="h-10 flex items-center">
              {tf('fields.file')}
            </FormLabel>

            <div className="space-y-3">
              <FormControl>
                <input
                  ref={field.ref}
                  name={field.name}
                  type="file"
                  accept=".txt,text/plain"
                  className="flex h-8 w-full rounded-md border-0.5 border-border-button bg-bg-input px-3 py-1.5 outline-none text-sm text-text-primary file:border-0 file:bg-transparent file:text-sm file:font-medium file:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-accent-primary disabled:cursor-not-allowed disabled:opacity-50 transition-colors"
                  onBlur={field.onBlur}
                  onChange={(e) => field.onChange(e.target.files?.[0])}
                  disabled={field.disabled}
                />
              </FormControl>
              <div className="flex items-center justify-between gap-3">
                <p className="text-xs text-text-secondary">{tf('fileTip')}</p>
                <Button
                  type="button"
                  variant="outline"
                  className="h-8 shrink-0 px-3"
                  loading={isDownloading}
                  onClick={async () => {
                    setIsDownloading(true);
                    try {
                      const res = await downloadSensitiveWordFilter();
                      downloadFileFromBlob(res.data, 'compliance.txt');
                    } catch {
                      toast.error(tf('downloadFailed'), {
                        position: 'top-center',
                      });
                    } finally {
                      setIsDownloading(false);
                    }
                  }}
                >
                  <Download className="mr-2 size-4" />
                  {tf('download')}
                </Button>
              </div>
              <FormMessage />
            </div>
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="sensitiveWordFilter.enabled"
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

SensitiveWordFilterFormGroup.mapValuesToData = (
  formValues: SensitiveWordFilterFormGroup.SchemaType,
) => {
  return {
    'sensitive_word_filter.enabled': formValues.enabled,
    'sensitive_word_filter.response': formValues.response,
  } satisfies AdminService.SetVariablesInput;
};

SensitiveWordFilterFormGroup.mapValuesFromData = (
  data: AdminService.SystemVariables,
): SensitiveWordFilterFormGroup.SchemaType => {
  const response = data['sensitive_word_filter.response']?.value;
  return {
    enabled: !!data['sensitive_word_filter.enabled']?.value,
    response:
      typeof response === 'string' && response ? response : DEFAULT_RESPONSE,
  };
};

SensitiveWordFilterFormGroup.uploadFile = (file: File) => {
  return uploadSensitiveWordFilter(file);
};

SensitiveWordFilterFormGroup.schema = z.object({
  enabled: z.boolean().optional(),
  response: z.string().min(1),
  file: z
    .any()
    .optional()
    .refine(
      (file) =>
        !file ||
        (file instanceof File && file.name.toLowerCase().endsWith('.txt')),
      'Only .txt files are allowed',
    )
    .refine(
      (file) => !file || file.size <= MAX_FILE_SIZE,
      'The file must be 30 MiB or smaller',
    ),
});

SensitiveWordFilterFormGroup.defaultValues = Object.freeze({
  enabled: false,
  response: DEFAULT_RESPONSE,
});

// eslint-disable-next-line
namespace SensitiveWordFilterFormGroup {
  export type SchemaType = z.infer<typeof SensitiveWordFilterFormGroup.schema>;
}

export default SensitiveWordFilterFormGroup;
