export enum LogTabs {
  FILE_LOGS = 'fileLogs',
  DATASET_LOGS = 'datasetLogs',
}

export enum ProcessingType {
  knowledgeGraph = 'Graph',
  raptor = 'RAPTOR',
  clone = 'Clone',
  artifact = 'Artifact',
  skill = 'Skill',
}

export const ProcessingTypeMap = {
  [ProcessingType.knowledgeGraph]: 'Knowledge Graph',
  [ProcessingType.raptor]: 'RAPTOR',
  [ProcessingType.clone]: 'Clone',
  [ProcessingType.artifact]: 'Artifact',
  [ProcessingType.skill]: 'Skill',
  GraphRAG: 'Knowledge Graph',
} as const;
