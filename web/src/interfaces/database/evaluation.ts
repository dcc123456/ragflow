// Collection
export interface IEvaluationCollection {
  id?: string;
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
export interface IEvaluationRunResult {
  cases?: IEvaluationCase[];
  results?: Array<{
    [key: string]: any;
  }>;
  run?: IEvaluationRun;
}

// Recommendations
export interface IEvaluationRecommendation {
  type: string;
  message: string;
  severity?: 'info' | 'warning' | 'error';
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
  config_snapshot: Configsnapshot;
  create_date: string;
  create_time: number;
  created_by: string;
  id: string;
  metrics_summary: LlmSetting;
  name: string;
  status: string;
  target_id: string;
  target_type: string;
  update_date: string;
  update_time: number;
}

interface Configsnapshot {
  create_date: string;
  create_time: number;
  description: string;
  do_refer: string;
  icon: string;
  id: string;
  kb_ids: string[];
  language: string;
  llm_id: string;
  llm_setting: LlmSetting;
  meta_data_filter: Metadatafilter;
  name: string;
  prompt_config: Promptconfig;
  prompt_type: string;
  rerank_id: string;
  similarity_threshold: number;
  status: string;
  tenant_id: string;
  top_k: number;
  top_n: number;
  update_date: string;
  update_time: number;
  vector_similarity_weight: number;
}

interface Promptconfig {
  empty_response: string;
  keyword: boolean;
  parameters: Parameter[];
  prologue: string;
  quote: boolean;
  reasoning: boolean;
  refine_multiturn: boolean;
  system: string;
  toc_enhance: boolean;
  tts: boolean;
  use_kg: boolean;
}

interface Parameter {
  key: string;
  optional: boolean;
}

interface Metadatafilter {
  method: string;
}

type LlmSetting = Record<string, any>;
