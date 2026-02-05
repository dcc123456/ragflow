export enum EvaluationType {
  Agent = 'agent',
  Chat = 'chat',
}

export enum EvaluationSearchParams {
  Type = 'type',
  RunId = 'runId',
  Page = 'page',
}

export enum RunningStatus {
  RUNNING = 'RUNNING',
  COMPLETED = 'COMPLETED',
  FAILED = 'FAILED',
  CANCEL = 'CANCEL',
}
