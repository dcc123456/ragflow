import { EvaluationSearchParams, EvaluationType } from '@/constants/evaluation';
import { useSearchParams } from 'react-router';

export const useEvaluationUrl = () => {
  const [searchParams, setSearchParams] = useSearchParams();

  const type = (searchParams.get(EvaluationSearchParams.Type) ||
    EvaluationType.Agent) as EvaluationType;

  const runId = searchParams.get(EvaluationSearchParams.RunId) || '';

  const setType = (newType: EvaluationType) => {
    searchParams.set(EvaluationSearchParams.Type, newType);
    setSearchParams(searchParams);
  };

  const setRunId = (newRunId: string) => {
    if (newRunId) {
      searchParams.set(EvaluationSearchParams.RunId, newRunId);
    } else {
      searchParams.delete(EvaluationSearchParams.RunId);
    }
    setSearchParams(searchParams);
  };

  return { type, runId, setType, setRunId };
};
