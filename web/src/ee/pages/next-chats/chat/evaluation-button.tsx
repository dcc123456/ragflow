import { Button } from '@/components/ui/button';
import { EvaluationType } from '@/ee/constants/evaluation';
import { useEvaluationNavigate } from '@/ee/hooks/use-evaluation-navigate';
import { ChartColumn } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useParams } from 'react-router';

export function ChatEvaluationButton() {
  const { t } = useTranslation();
  const { navigateToEvaluation } = useEvaluationNavigate();
  const { id } = useParams();

  return (
    <Button
      variant="ghost"
      onClick={navigateToEvaluation(EvaluationType.Chat, id!)}
    >
      <ChartColumn /> {t('evaluation.title')}
    </Button>
  );
}
