import { ButtonLoading } from '@/components/ui/button';
import { useFetchDialog } from '@/hooks/use-chat-request';
import { hasPreviewPermission } from '@/utils/permission-util';
import { useTranslation } from 'react-i18next';

type SaveButtonProps = {
  loading: boolean;
};

export function SavingButton({ loading }: SaveButtonProps) {
  const { t } = useTranslation();
  const { data } = useFetchDialog();

  return (
    <ButtonLoading
      type="submit"
      loading={loading}
      disabled={hasPreviewPermission(data.operator_permission)}
    >
      {t('common.save')}
    </ButtonLoading>
  );
}
