// Dataset
export interface IEvaluationCreateDatasetRequestBody {
  name: string;
  description?: string;
}

export interface IEvaluationUpdateDatasetRequestBody {
  name?: string;
  description?: string;
}

// Case
export interface IEvaluationAddCaseQueryParams {
  question: string;
  reference_answer?: string;
  relevant_kb_ids?: string[];
  relevant_doc_ids?: string[];
  metadata?: Record<string, any>;
}

export interface IEvaluationUpdateCaseRequestBody {
  variable?: {
    question?: string;
    reference_answer?: string;
  };
  relevant_kb_ids?: string[];
  relevant_doc_ids?: string[];
  metadata?: Record<string, any>;
}

export interface IEvaluationImportCaseRequestBody {
  file?: File;
  cases?: Array<{
    question: string;
    reference_answer?: string;
  }>;
}

// Run
export interface IEvaluationStartRunRequestBody {
  dataset_id: string;
  target_type: 'agent' | 'chat';
  target_id: string;
  name?: string;
}

export interface IEvaluationUpdateRunRequestBody {
  name?: string;
  config_snapshot?: Record<string, any>;
}
