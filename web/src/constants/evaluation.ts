export enum EvaluationType {
  Agent = 'agent',
  Chat = 'chat',
}

export enum EvaluationSearchParams {
  Type = 'type',
  RunId = 'runId',
  Page = 'page',
}

export const NewEvaluationRunId = 'new';

export enum RunningStatus {
  PENDING = 'PENDING',
  RUNNING = 'RUNNING',
  COMPLETED = 'COMPLETED',
  FAILED = 'FAILED',
  CANCEL = 'CANCEL',
}
