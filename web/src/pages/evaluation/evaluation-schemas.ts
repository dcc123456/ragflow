import { EvaluationType } from '@/constants/evaluation';
import { useEvaluationUrl } from '@/hooks/use-evaluation-url';
import { z } from 'zod';
import { useChatSettingSchema } from '../next-chats/chat/app-settings/use-chat-setting-schema';

export const EvaluationMetricItemSchema = z.object({
  enable: z.boolean().default(false),
  llm_id: z.string().default(''),
});

export const EvaluationMetricsSchema = z
  .object({
    context_relevance: EvaluationMetricItemSchema,
    faithfulness: EvaluationMetricItemSchema,
    semantic_similarity: EvaluationMetricItemSchema,
  })
  .optional();

export type EvaluationMetricsType = z.infer<typeof EvaluationMetricsSchema>;

export function useEvaluationSchema() {
  const chatSettingSchema = useChatSettingSchema();
  const { type } = useEvaluationUrl();

  return z.object({
    collection_id: z.string(),
    config_snapshot: z.object({
      target: type === EvaluationType.Chat ? chatSettingSchema : z.object({}),
      metrics: EvaluationMetricsSchema,
    }),
  });
}

export type EvaluationSettingsFormType = z.infer<
  ReturnType<typeof useEvaluationSchema>
>;
