import { Info } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { MetadataType } from '@/pages/dataset/components/metedata/constant';

// The 14 document metadata keys the Resume pipeline's Extractor owns and
// re-populates (or clears) on every re-parse. Mirrors resumeLaneMetadataKeys
// in internal/ingestion/component/resume_merge_ee.go — keep the two lists
// in sync.
const RESUME_MANAGED_METADATA_FIELDS: string[] = [
  'name',
  'skills',
  'certificates',
  'expected_positions',
  'expected_cities',
  'current_location',
  'languages',
  'work_experience_years',
  'companies',
  'positions',
  'highest_degree',
  'highest_degree_level',
  'schools',
  'majors',
];

/**
 * Mounted by the document metadata editor: on a resume-parsed document it
 * lists the fields the Resume parser rewrites, so a hand-filled value under
 * one of those names is not lost unknowingly.
 */
export const MetadataManagedFieldsHint = ({
  metadataType,
  doc,
}: {
  metadataType?: MetadataType;
  doc?: Record<string, any>;
}) => {
  const { t } = useTranslation();

  if (
    metadataType !== MetadataType.UpdateSingle ||
    doc?.chunk_method !== 'resume'
  ) {
    return null;
  }

  return (
    <div
      data-testid="resume-managed-fields-hint"
      className="flex items-start gap-2 rounded-md border border-border-button bg-bg-base px-3 py-2 text-xs text-text-secondary"
    >
      <Info className="mt-0.5 size-3.5 shrink-0" />
      <div>
        {t('knowledgeDetails.metadata.parserManagedFields')}
        <span className="font-medium">
          {RESUME_MANAGED_METADATA_FIELDS.join(', ')}
        </span>
      </div>
    </div>
  );
};
