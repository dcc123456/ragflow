import { EvaluationType } from '@/constants/evaluation';

// Collection
export interface IEvaluationCreateCollectionRequestBody {
  name: string;
  description?: string;
}

export interface IEvaluationUpdateCollectionRequestBody {
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
export interface IEvaluationCreateRunRequestBody {
  collection_id: string;
  target_type: EvaluationType;
  target_id: string;
  name?: string;
  config_snapshot?: Record<string, any>;
}

export interface IEvaluationUpdateRunRequestBody {
  collection_id?: string;
  name?: string;
  config_snapshot?: Record<string, any>;
}

export interface IEvaluationStartRunRequestBody {
  case_ids?: string[];
  metrics_name?: string[];
}
