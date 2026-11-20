// Includes lazy-loaded languages that have no per-language enterprise overlay.
export const CompilationTranslations: Record<string, object> = {
  en: {
    chunk: { representationKinds: { timeline: 'Timeline' } },
    knowledgeCompilation: {
      structureTimeline: 'Timeline',
      noStructureTimeline: 'No timeline yet',
    },
  },
  zh: {
    chunk: { representationKinds: { timeline: 'Timeline' } },
    knowledgeCompilation: {
      structureTimeline: 'Timeline',
      noStructureTimeline: '暂无 Timeline',
    },
  },
  ja: { chunk: { representationKinds: { timeline: 'タイムライン' } } },
  uz: {
    chunk: { representationKinds: { timeline: 'Vaqt chizig‘i' } },
    knowledgeCompilation: {
      structureTimeline: 'Vaqt chizig‘i',
      noStructureTimeline: 'Hozircha vaqt chizig‘i yo‘q',
    },
  },
  az: {
    chunk: { representationKinds: { timeline: 'Zaman xətti' } },
    knowledgeCompilation: {
      structureTimeline: 'Zaman zolağı',
      noStructureTimeline: 'Hələ zaman zolağı yoxdur',
    },
  },
  tr: {
    chunk: { representationKinds: { timeline: 'Zaman çizelgesi' } },
    knowledgeCompilation: {
      structureTimeline: 'Zaman çizelgesi',
      noStructureTimeline: 'Henüz zaman çizelgesi yok',
    },
  },
};
