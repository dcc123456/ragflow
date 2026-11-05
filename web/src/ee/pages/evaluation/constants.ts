export {
  EvaluationSearchParams,
  EvaluationType,
  NewEvaluationRunId,
  RunningStatus,
} from '@/ee/constants/evaluation';

export enum RunType {
  All = 'all',
  Relevancy = 'context_relevance',
  Factuality = 'faithfulness',
  Consistency = 'semantic_similarity',
}
