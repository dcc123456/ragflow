import { LlmSettingFieldItems } from '@/components/llm-setting-items/next';

type ChatModelSettingsProps = {
  prefix?: string;
};

export function ChatModelSettings({
  prefix = 'llm_setting',
}: ChatModelSettingsProps) {
  return (
    <div className="space-y-8">
      <LlmSettingFieldItems
        prefix={prefix}
        llmId="llm_id"
      ></LlmSettingFieldItems>
    </div>
  );
}
