export enum ProcessingType {
  timeline = 'Timeline',
}

export enum GenerateType {
  Timeline = 'Timeline',
}

export enum TraceType {
  Timeline = 'timeline',
}

export const ProcessingTypeMap = { [ProcessingType.timeline]: 'Timeline' };
export const GenerateTypeMap = {
  [GenerateType.Timeline]: ProcessingType.timeline,
};
export const TraceTypeMap = { [GenerateType.Timeline]: TraceType.Timeline };
