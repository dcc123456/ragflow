import { IDialog, IReferenceObject } from './chat';

// Collection
export interface IEvaluationCollection {
  id: string;
  name: string;
  description?: string;
  status?: number;
  created_by?: string;
  tenant_id?: string;
  create_date?: string;
  update_date?: string;
  create_time?: number;
  update_time?: number;
}

export interface Variable {
  question: string;
  reference_answer: string;
}
export interface CaseItem {
  collection_id: string;
  create_date: string;
  create_time: number;
  id: string;
  metadata: Record<string, any>;
  relevant_doc_ids: Record<string, any>;
  relevant_kb_ids: Record<string, any>;
  update_date: string;
  update_time: number;
  variable: Variable;
}

export interface EvaluationDetailList {
  cases: CaseItem[];
  total: number;
}

// Case
export interface IEvaluationCase {
  id?: string;
  collection_id: string;
  variable: {
    question: string;
    reference_answer?: string;
  };
  relevant_kb_ids?: string[];
  relevant_doc_ids?: string[];
  metadata?: Record<string, any>;
  create_date?: string;
  update_date?: string;
  create_time?: number;
  update_time?: number;
}

// Run

// Run Result
export interface IEvaluationRunResultData {
  cases?: IEvaluationCase[];
  results?: Array<IEvaluationRunResult>;
  run?: IEvaluationRun;
  total: number;
}

// Recommendations
export interface IEvaluationRecommendation {
  type: string;
  message: string;
  severity?: 'info' | 'warning' | 'error';
}

export interface IMetricsSummary {
  answer_length: AnswerLength;
  bleu_score: AnswerLength;
  context_relevance: AnswerLength;
  execution_time: AnswerLength;
  faithfulness: AnswerLength;
  has_answer: AnswerLength;
  semantic_similarity: AnswerLength;
  semantic_similarity_reason: string;
  faithfulness_reason: string;
}

interface AnswerLength {
  config: string;
  summary: number;
  type: string;
}

// Metric types
export interface IEvaluationMetric {
  name: string;
  value: number;
  description?: string;
}

export interface IEvaluationRun {
  collection_id: string;
  complete_time: null;
  config_snapshot: IConfigSnapshot;
  create_date: string;
  create_time: number;
  created_by: string;
  id: string;
  metrics_summary: IMetricsSummary;
  name: string;
  status: string;
  target_id: string;
  target_type: string;
  update_date: string;
  update_time: number;
}

interface IConfigSnapshot {
  target: IDialog;
  metrics: IMetrics;
}

export interface IMetrics {
  answer_length: number;
  blue_score: number;
  faithfulness: number;
  faithfulness_reason: string;
  has_answer: number;
  semantic_similarity: number;
  semantic_similarity_reason: string;
  context_relevance: number;
  context_relevance_reason: string;
}

export interface IEvaluationRunResult {
  case_id: string;
  execution_time: number;
  generated_answer: string;
  id: string;
  metrics: IMetrics;
  retrieved_chunks: IReferenceObject;
  token_usage: Record<string, any>;
  variable: Variable;
}

export interface Variable {
  question: string;
  reference_answer: string;
}
