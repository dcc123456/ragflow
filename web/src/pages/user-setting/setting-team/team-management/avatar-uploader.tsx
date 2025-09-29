import { FileUploader } from '@/components/file-uploader';
import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form';
import { useFormContext } from 'react-hook-form';
import { useTranslation } from 'react-i18next';

export function AvatarUploader() {
  const { control } = useFormContext();
  const { t } = useTranslation();

  return (
    <FormField
      control={control}
      name="avatar"
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t('permission.avatar')}</FormLabel>
          <FormControl>
            <FileUploader
              value={field.value}
              onValueChange={field.onChange}
              maxFileCount={1}
              maxSize={4 * 1024 * 1024}
            />
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  );
}
