import { z } from 'zod';

export const EvaluationMetricSchema = z.object({
  enabled: z.boolean().default(false),
  model: z.string().optional(),
});

export const EvaluationSettingsFormSchema = z.object({
  collection_id: z.string().min(1, 'evaluation.errors.collectionRequired'),
  relevancy: EvaluationMetricSchema,
  factuality: EvaluationMetricSchema,
  consistency: EvaluationMetricSchema,
  prompt: z.string().optional(),
});

export type EvaluationSettingsFormType = z.infer<
  typeof EvaluationSettingsFormSchema
>;
