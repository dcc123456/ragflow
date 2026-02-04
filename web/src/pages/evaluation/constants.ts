export { EvaluationSearchParams, EvaluationType } from '@/constants/evaluation';

export enum RunningStatus {
  RUNNING = 'RUNNING',
  COMPLETED = 'COMPLETED',
  FAILED = 'FAILED',
  CANCEL = 'CANCEL',
}

export enum RunType {
  All = 'all',
  Relevancy = 'context_relevance',
  Factuality = 'faithfulness',
  Consistency = 'semantic_similarity',
}
