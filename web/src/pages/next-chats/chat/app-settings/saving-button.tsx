import { ButtonLoading } from '@/components/ui/button';
import { useFetchDialog } from '@/hooks/use-chat-request';
import { useTranslation } from 'react-i18next';

type SaveButtonProps = {
  loading: boolean;
};

export function SavingButton({ loading }: SaveButtonProps) {
  const { t } = useTranslation();
  const { data } = useFetchDialog();

  return (
    <ButtonLoading
      data-testid="chat-settings-save"
      type="submit"
      loading={loading}
    >
      {t('common.save')}
    </ButtonLoading>
  );
}
