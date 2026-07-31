import {
  AutoKeywordsFormField,
  AutoQuestionsFormField,
} from '@/components/auto-keywords-form-field';
import { LayoutRecognizeFormField } from '@/components/layout-recognize-form-field';
import { useKnowledgeBaseContext } from '../../contexts/knowledge-base-context';
import { ConfigurationFormContainer } from '../configuration-form-container';
import { AutoMetadata } from './common-item';
import { FormLayout } from '@/constants/form';

export function OneConfiguration() {
  const ownerTenantId = useKnowledgeBaseContext().knowledgeBase?.tenant_id;
  return (
    <ConfigurationFormContainer>
      <LayoutRecognizeFormField
        ownerTenantId={ownerTenantId}
      ></LayoutRecognizeFormField>
      <>
        <AutoMetadata />
        <AutoKeywordsFormField
          layout={FormLayout.Horizontal}
        ></AutoKeywordsFormField>
        <AutoQuestionsFormField
          layout={FormLayout.Horizontal}
        ></AutoQuestionsFormField>
      </>

      {/* <TagItems></TagItems> */}
    </ConfigurationFormContainer>
  );
}
