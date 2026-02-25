import { LlmSettingFieldItems } from '@/components/llm-setting-items/next';

type ChatModelSettingsProps = {
  prefix?: string;
  llmId?: string;
};

export function ChatModelSettings({
  prefix = 'llm_setting',
  llmId = 'llm_id',
}: ChatModelSettingsProps) {
  return (
    <div className="space-y-8">
      <LlmSettingFieldItems
        prefix={prefix}
        llmId={llmId}
      ></LlmSettingFieldItems>
    </div>
  );
}
