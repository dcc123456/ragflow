// Dataset
export interface IEvaluationDataset {
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
  dataset_id: string;
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
export interface IEvaluationRun {
  id?: string;
  dataset_id: string;
  name: string;
  target_type: 'agent' | 'chat';
  target_id: string;
  status: 'PENDING' | 'RUNNING' | 'COMPLETED' | 'FAILED';
  created_by?: string;
  complete_time?: string;
  config_snapshot?: Record<string, any>;
  metrics_summary?: Record<string, any>;
  create_date?: string;
  update_date?: string;
  create_time?: number;
  update_time?: number;
}

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
