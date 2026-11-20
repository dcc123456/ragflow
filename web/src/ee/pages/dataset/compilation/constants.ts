import { CalendarRange } from 'lucide-react';
import { GenerateType, ProcessingType } from '@/ee/constants/knowledge';

export enum ViewMode {
  Timeline = 'timeline',
}

export const StructureKinds = [ViewMode.Timeline] as const;
export const ViewModeLabelKeyMap = {
  [ViewMode.Timeline]: 'knowledgeCompilation.structureTimeline',
};
export const ViewModeIconMap = { [ViewMode.Timeline]: CalendarRange };
export const ProcessingTypeViewModeMap = {
  [ProcessingType.timeline]: ViewMode.Timeline,
};
export const ViewModeGenerateTypeMap = {
  [ViewMode.Timeline]: GenerateType.Timeline,
};
export const EmptyStateTitleKeyMap = {
  [ViewMode.Timeline]: 'knowledgeCompilation.noStructureTimeline',
};
