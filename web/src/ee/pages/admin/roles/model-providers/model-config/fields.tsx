import { t } from 'i18next';
import { z } from 'zod';

import { LLMFactory } from '@/constants/llm';
import { isLocalLlmFactory } from '@/pages/user-setting/utils';
import { camelCase, upperFirst } from 'lodash';
import { DefaultValues, FieldValues } from 'react-hook-form';
import {
  FieldConfig,
  InteractiveFieldConfig,
} from '../components/dynamic-form';

const chatModelWithVision = ({
  model_type,
  vision,
  ...values
}: FieldValues) => ({
  ...values,
  model_type: model_type === 'chat' && vision ? 'image2text' : model_type,
});

const defaultModelFields: FieldConfig[] = [
  {
    name: 'api_key',
    label: t('setting.apiKey'),
    type: 'text',
    required: true,
    placeholder: t('setting.apiKeyMessage'),
    rules: z.string().nonempty(t('setting.apiKeyMessage')),
  },
];

const defaultLocalLlmModelFields: FieldConfig[] = [
  {
    name: 'model_type',
    label: t('setting.modelType'),
    type: 'select',
    required: true,
    rules: z.enum(['chat', 'embedding', 'rerank', 'image2text'], {
      message: t('setting.modelTypeMessage'),
    }),
    options: ['chat', 'embedding', 'rerank', 'image2text'],
    defaultValue: 'embedding',
  },
  {
    name: 'llm_name',
    label: t('setting.modelName'),
    type: 'text',
    required: true,
    placeholder: t('setting.modelNameMessage'),
    rules: z.string().nonempty(t('setting.modelNameMessage')),
  },
  {
    name: 'api_base',
    label: t('setting.addLlmBaseUrl'),
    type: 'text',
    required: true,
    placeholder: t('setting.baseUrlNameMessage'),
    rules: z.string().nonempty(t('setting.baseUrlNameMessage')),
  },
  {
    name: 'api_key',
    label: t('setting.apiKey'),
    type: 'text',
    placeholder: t('setting.apiKeyMessage'),
    rules: z.string().optional(),
  },
  {
    name: 'max_tokens',
    label: t('setting.maxTokens'),
    type: 'number',
    required: true,
    placeholder: t('setting.maxTokensTip'),
    rules: z.number().min(0, t('setting.maxTokensMinMessage')),
  },
  {
    name: 'vision',
    label: t('setting.vision'),
    type: 'switch',
    defaultValue: false,
    rules: z.boolean().optional(),
    shouldRender: (values: FieldValues) => values.model_type === 'chat',
  },
];

export const Models = {
  [LLMFactory.AzureOpenAI]: {
    modelTypes: ['chat', 'embedding', 'image2text'],
    transformFieldValues: chatModelWithVision,
  },
  [LLMFactory.Bedrock]: {
    modelTypes: ['chat', 'embedding'],
    authModes: ['access_key_secret', 'iam_role', 'assume_role'],
    regions: [
      'us-east-2',
      'us-east-1',
      'us-west-1',
      'us-west-2',
      'af-south-1',
      'ap-east-1',
      'ap-south-2',
      'ap-southeast-3',
      'ap-southeast-5',
      'ap-southeast-4',
      'ap-south-1',
      'ap-northeast-3',
      'ap-northeast-2',
      'ap-southeast-1',
      'ap-southeast-2',
      'ap-east-2',
      'ap-southeast-7',
      'ap-northeast-1',
      'ca-central-1',
      'ca-west-1',
      'eu-central-1',
      'eu-west-1',
      'eu-west-2',
      'eu-south-1',
      'eu-west-3',
      'eu-south-2',
      'eu-north-1',
      'eu-central-2',
      'il-central-1',
      'mx-central-1',
      'me-south-1',
      'me-central-1',
      'sa-east-1',
      'us-gov-east-1',
      'us-gov-west-1',
    ],
    transformFieldValues: ({
      bedrock_ak,
      bedrock_sk,
      aws_role_arn,
      ...values
    }: FieldValues) => {
      return {
        ...values,
        // @ts-ignore
        ...({
          access_key_secret: { bedrock_ak, bedrock_sk },
          iam_role: { aws_role_arn },
        }[values.auth_mode] ?? {}),
      };
    },
  },
  [LLMFactory.FishAudio]: {
    modelTypes: ['tts'],
    userGuideLink: {
      href: 'https://fish.audio',
      text: t('setting.FishAudioLink'),
    },
  },
  [LLMFactory.GoogleCloud]: {
    modelTypes: ['chat', 'image2text'],
  },
  [LLMFactory.TencentHunYuan]: {
    modelTypes: ['chat', 'image2text'],
  },
  [LLMFactory.MinerU]: {
    modelTypes: ['chat', 'image2text'],
    backendOptions: [
      'pipeline',
      'vlm-transformers',
      'vlm-vllm-engine',
      'vlm-http-client',
      'vlm-mlx-engine',
      'vlm-vllm-async-engine',
      'vlm-lmdeploy-engine',
    ],
  },
  [LLMFactory.TencentCloud]: {
    modelTypes: ['speech2text'],
    llmNames: [
      '16k_zh',
      '16k_zh_large',
      '16k_multi_lang',
      '16k_zh_dialect',
      '16k_en',
      '16k_yue',
      '16k_zh-PY',
      '16k_ja',
      '16k_ko',
      '16k_vi',
      '16k_ms',
      '16k_id',
      '16k_fil',
      '16k_th',
      '16k_pt',
      '16k_tr',
      '16k_ar',
      '16k_es',
      '16k_hi',
      '16k_fr',
      '16k_zh_medical',
      '16k_de',
    ],
    userGuideLink: {
      href: 'https://cloud.tencent.com/document/api/1093/37823',
      text: t('setting.TencentCloudLink'),
    },
  },
  [LLMFactory.TokenPony]: {
    userGuideLink: {
      href: 'https://docs.tokenpony.cn/#/',
      text: t('setting.ollamaLink', { name: LLMFactory.TokenPony }),
    },
  },
  [LLMFactory.XunFeiSpark]: {
    modelTypes: ['chat', 'tts'],
    transformFieldValues: chatModelWithVision,
  },
  [LLMFactory.VolcEngine]: {
    modelTypes: ['chat', 'embedding', 'image2text'],
    userGuideLink: {
      href: 'https://www.volcengine.com/docs/82379/1302008',
      text: t('setting.ollamaLink', { name: LLMFactory.VolcEngine }),
    },
    transformFieldValues: chatModelWithVision,
  },
  [LLMFactory.BaiduYiYan]: {
    modelTypes: ['chat', 'embedding', 'rerank'],
    transformFieldValues: ({ yiyan_ak, yiyan_sk, ...values }: FieldValues) => {
      return chatModelWithVision({
        ...values,
        api_key: {
          yiyan_ak,
          yiyan_sk,
        },
      });
    },
  },

  // Local LLMs
  [LLMFactory.Ollama]: {
    modelTypes: ['chat', 'embedding', 'rerank', 'image2text'],
    userGuideLink: {
      href: 'https://github.com/infiniflow/ragflow/blob/main/docs/guides/models/deploy_local_llm.mdx',
      text: t('setting.ollamaLink', { name: LLMFactory.Ollama }),
    },
    transformFieldValues: chatModelWithVision,
  },
  [LLMFactory.HuggingFace]: {
    modelTypes: ['embedding', 'chat', 'rerank'],
    userGuideLink: {
      href: 'https://huggingface.co/docs/text-embeddings-inference/quick_tour',
      text: t('setting.ollamaLink', { name: LLMFactory.HuggingFace }),
    },
    transformFieldValues: chatModelWithVision,
  },
  [LLMFactory.LMStudio]: {
    modelTypes: ['chat', 'embedding', 'image2text'],
    userGuideLink: {
      href: 'https://lmstudio.ai/docs/basics',
      text: t('setting.ollamaLink', { name: LLMFactory.LMStudio }),
    },
    transformFieldValues: chatModelWithVision,
  },
  [LLMFactory.Xinference]: {
    modelTypes: [
      'chat',
      'embedding',
      'rerank',
      'image2text',
      'speech2text',
      'tts',
    ],
    userGuideLink: {
      href: 'https://inference.readthedocs.io/en/latest/user_guide',
      text: t('setting.ollamaLink', { name: LLMFactory.Xinference }),
    },
    transformFieldValues: chatModelWithVision,
  },
  [LLMFactory.ModelScope]: {
    modelTypes: ['chat'],
    userGuideLink: {
      href: 'https://modelscope.cn/docs/model-service/API-Inference/intro',
      text: t('setting.ollamaLink', { name: LLMFactory.ModelScope }),
    },
    transformFieldValues: chatModelWithVision,
  },
  [LLMFactory.GPUStack]: {
    modelTypes: ['chat', 'embedding', 'rerank', 'speech2text', 'tts'],
    userGuideLink: {
      href: 'https://docs.gpustack.ai/latest/quickstart',
      text: t('setting.ollamaLink', { name: LLMFactory.GPUStack }),
    },
    transformFieldValues: chatModelWithVision,
  },
  [LLMFactory.OpenRouter]: {
    modelTypes: ['chat', 'image2text'],
    userGuideLink: {
      href: 'https://openrouter.ai/docs',
      text: t('setting.ollamaLink', { name: LLMFactory.OpenRouter }),
    },
    transformFieldValues: chatModelWithVision,
  },
  [LLMFactory.LocalAI]: {
    userGuideLink: {
      href: 'https://localai.io/getting-started/models/',
      text: t('setting.ollamaLink', { name: LLMFactory.LocalAI }),
    },
    transformFieldValues: chatModelWithVision,
  },
  [LLMFactory.OpenAiAPICompatible]: {
    userGuideLink: {
      href: 'https://platform.openai.com/docs/models/gpt-4',
      text: t('setting.ollamaLink', { name: LLMFactory.OpenAiAPICompatible }),
    },
    transformFieldValues: chatModelWithVision,
  },
  [LLMFactory.TogetherAI]: {
    userGuideLink: {
      href: 'https://docs.together.ai/docs/deployment-options',
      text: t('setting.ollamaLink', { name: LLMFactory.TogetherAI }),
    },
    transformFieldValues: chatModelWithVision,
  },
  [LLMFactory.Replicate]: {
    userGuideLink: {
      href: 'https://replicate.com/docs/topics/deployments',
      text: t('setting.ollamaLink', { name: LLMFactory.Replicate }),
    },
    transformFieldValues: chatModelWithVision,
  },
  [LLMFactory.VLLM]: {
    userGuideLink: {
      href: 'https://docs.vllm.ai/en/latest/',
      text: t('setting.ollamaLink', { name: LLMFactory.VLLM }),
    },
    transformFieldValues: chatModelWithVision,
  },
} as const;

type ModelFieldConfigAlias = [
  string,
  {
    override?: Record<string, Partial<InteractiveFieldConfig>>;
    prepend?: FieldConfig[];
    append?: FieldConfig[];
  }?,
];

// Map of model fields by model factory
const modelFields: Partial<
  Record<LLMFactory, FieldConfig[] | ModelFieldConfigAlias>
> = {
  [LLMFactory.OpenAI]: [
    {
      name: 'api_key',
      label: t('setting.apiKey'),
      type: 'text',
      required: true,
      placeholder: t('setting.apiKeyMessage'),
      rules: z.string().nonempty(t('setting.apiKeyMessage')),
    },
    {
      name: 'base_url',
      label: t('setting.baseUrl'),
      labelTooltip: t('setting.baseUrlTip'),
      type: 'text',
      placeholder: 'https://api.openai.com/v1',
      rules: z.string().optional(),
    },
  ],

  [LLMFactory.TongYiQianWen]: [
    LLMFactory.OpenAI,
    {
      override: {
        base_url: {
          labelTooltip: t('setting.tongyiBaseUrlTip'),
          placeholder: t('setting.tongyiBaseUrlPlaceholder'),
        },
      },
    },
  ],

  [LLMFactory.Anthropic]: [
    LLMFactory.OpenAI,
    {
      override: {
        base_url: {
          labelTooltip: undefined,
          placeholder: 'https://api.anthropic.com/v1',
        },
      },
    },
  ],

  [LLMFactory.MiniMax]: [
    LLMFactory.OpenAI,
    {
      override: {
        base_url: {
          labelTooltip: t('setting.minimaxBaseUrlTip'),
          placeholder: t('setting.minimaxBaseUrlPlaceholder'),
        },
      },
      append: [
        {
          name: 'group_id',
          label: t('setting.groupId'),
          type: 'text',
          rules: z.string().optional(),
        },
      ],
    },
  ],

  [LLMFactory.AzureOpenAI]: [
    {
      name: 'model_type',
      label: t('setting.modelType'),
      type: 'select',
      required: true,
      rules: z.enum(Models[LLMFactory.AzureOpenAI].modelTypes, {
        message: t('setting.modelTypeMessage'),
      }),
      options: Models[LLMFactory.AzureOpenAI].modelTypes,
      defaultValue: 'embedding',
    },
    {
      name: 'api_base',
      label: t('setting.addLlmBaseUrl'),
      type: 'text',
      required: true,
      placeholder: t('setting.baseUrlNameMessage'),
      rules: z.string().nonempty(t('setting.baseUrlNameMessage')),
    },
    {
      name: 'api_key',
      label: t('setting.apiKey'),
      type: 'text',
      placeholder: t('setting.apiKeyMessage'),
      rules: z.string().optional(),
    },
    {
      name: 'llm_name',
      label: t('setting.modelName'),
      type: 'text',
      required: true,
      placeholder: t('setting.modelNameMessage'),
      defaultValue: 'gpt-3.5-turbo',
      rules: z.string().nonempty(t('setting.modelNameMessage')),
    },
    {
      name: 'api_version',
      label: t('setting.apiVersion'),
      type: 'text',
      placeholder: t('setting.apiVersionMessage'),
      defaultValue: '2024-02-01',
      rules: z.string().optional(),
    },
    {
      name: 'max_tokens',
      label: t('setting.maxTokens'),
      type: 'number',
      required: true,
      placeholder: t('setting.maxTokensTip'),
      rules: z.number().min(0, t('setting.maxTokensMinMessage')),
    },
    {
      name: 'vision',
      label: t('setting.vision'),
      type: 'switch',
      defaultValue: false,
      rules: z.boolean().optional(),
      shouldRender: (values: FieldValues) => values.model_type === 'chat',
    },
  ],

  [LLMFactory.Bedrock]: [
    {
      name: 'model_type',
      label: t('setting.modelType'),
      type: 'select',
      required: true,
      placeholder: t('setting.modelTypeMessage'),
      rules: z.enum(Models.Bedrock.modelTypes, {
        message: t('setting.modelTypeMessage'),
      }),
      options: Models.Bedrock.modelTypes,
      defaultValue: 'chat',
    },
    {
      name: 'llm_name',
      label: t('setting.modelName'),
      type: 'text',
      required: true,
      placeholder: t('setting.modelNameMessage'),
      rules: z.string().nonempty(t('setting.modelNameMessage')),
    },
    {
      name: 'auth_mode',
      type: 'segmented',
      required: true,
      rules: z.enum(Models.Bedrock.authModes, {
        message: t('setting.authModeMessage'),
      }),
      options: Models.Bedrock.authModes.map((x) => ({
        label: t(`setting.awsAuthMode${upperFirst(camelCase(x))}`),
        value: x,
      })),
      defaultValue: 'access_key_secret',
    },
    {
      name: 'bedrock_ak',
      label: t('setting.awsAccessKeyId'),
      type: 'text',
      required: true,
      placeholder: t('setting.bedrockAKMessage'),
      rules: z.string().nonempty(t('setting.bedrockAKMessage')),
      // clearOnHide: true,
      shouldRender: (values: FieldValues) =>
        values.auth_mode === 'access_key_secret',
    },
    {
      name: 'bedrock_sk',
      label: t('setting.awsSecretAccessKey'),
      type: 'text',
      required: true,
      placeholder: t('setting.bedrockSKMessage'),
      rules: z.string().nonempty(t('setting.bedrockSKMessage')),
      clearOnHide: true,
      shouldRender: (values: FieldValues) =>
        values.auth_mode === 'access_key_secret',
    },
    {
      name: 'aws_role_arn',
      label: t('setting.awsRoleArn'),
      type: 'text',
      required: true,
      placeholder: t('setting.awsRoleArnMessage'),
      rules: z.string().nonempty(t('setting.awsRoleArnMessage')),
      clearOnHide: true,
      shouldRender: (values: FieldValues) => values.auth_mode === 'iam_role',
    },
    {
      type: 'display',
      element: (
        <p className="text-sm text-text-secondary mt-2 mb-4">
          {t('setting.awsAssumeRoleTip')}
        </p>
      ),
      shouldRender: (values: FieldValues) => values.auth_mode === 'assume_role',
    },
    {
      name: 'bedrock_region',
      label: t('setting.bedrockRegion'),
      type: 'select',
      searchable: true,
      required: true,
      placeholder: t('setting.bedrockRegionMessage'),
      rules: z.string().nonempty(t('setting.bedrockRegionMessage')),
      options: Models.Bedrock.regions.map((x) => ({
        value: x,
        label: t(`setting.${x}`),
      })),
    },
    {
      name: 'max_tokens',
      label: t('setting.maxTokens'),
      type: 'number',
      required: true,
      placeholder: t('setting.maxTokensTip'),
      rules: z.number().min(0, t('setting.maxTokensMinMessage')),
    },
  ],

  [LLMFactory.FishAudio]: [
    {
      name: 'model_type',
      label: t('setting.modelType'),
      type: 'select',
      required: true,
      rules: z.enum(Models[LLMFactory.FishAudio].modelTypes, {
        message: t('setting.modelTypeMessage'),
      }),
      options: Models[LLMFactory.FishAudio].modelTypes,
      defaultValue: 'tts',
    },
    {
      name: 'llm_name',
      label: t('setting.modelName'),
      type: 'text',
      required: true,
      placeholder: t('setting.FishAudioModelNameMessage'),
      rules: z.string().nonempty(t('setting.FishAudioModelNameMessage')),
    },
    {
      name: 'fish_audio_ak',
      label: t('setting.addFishAudioAK'),
      type: 'text',
      required: true,
      placeholder: t('setting.addFishAudioAKMessage'),
      rules: z.string().nonempty(t('setting.addFishAudioAKMessage')),
    },
    {
      name: 'fish_audio_refid',
      label: t('setting.addFishAudioRefID'),
      type: 'text',
      required: true,
      placeholder: t('setting.addFishAudioRefIDMessage'),
      rules: z.string().nonempty(t('setting.addFishAudioRefIDMessage')),
    },
    {
      name: 'max_tokens',
      label: t('setting.maxTokens'),
      type: 'number',
      required: true,
      placeholder: t('setting.maxTokensTip'),
      rules: z.number().min(0, t('setting.maxTokensMinMessage')),
    },
  ],

  [LLMFactory.GoogleCloud]: [
    {
      name: 'model_type',
      label: t('setting.modelType'),
      type: 'select',
      required: true,
      rules: z.enum(Models[LLMFactory.GoogleCloud].modelTypes, {
        message: t('setting.modelTypeMessage'),
      }),
      options: Models[LLMFactory.GoogleCloud].modelTypes,
      defaultValue: 'chat',
    },
    {
      name: 'llm_name',
      label: t('setting.modelName'),
      type: 'text',
      required: true,
      placeholder: t('setting.GoogleModelIDMessage'),
      rules: z.string().nonempty(t('setting.GoogleModelIDMessage')),
    },
    {
      name: 'google_project_id',
      label: t('setting.addGoogleProjectID'),
      type: 'text',
      required: true,
      placeholder: t('setting.GoogleProjectIDMessage'),
      rules: z.string().nonempty(t('setting.GoogleProjectIDMessage')),
    },
    {
      name: 'google_region',
      label: t('setting.addGoogleRegion'),
      type: 'text',
      required: true,
      placeholder: t('setting.GoogleRegionMessage'),
      rules: z.string().nonempty(t('setting.GoogleRegionMessage')),
    },
    {
      name: 'google_service_account_key',
      label: t('setting.addGoogleServiceAccountKey'),
      type: 'text',
      required: true,
      placeholder: t('setting.GoogleServiceAccountKeyMessage'),
      rules: z.string().nonempty(t('setting.GoogleServiceAccountKeyMessage')),
    },
    {
      name: 'max_tokens',
      label: t('setting.maxTokens'),
      type: 'number',
      required: true,
      placeholder: t('setting.maxTokensTip'),
      rules: z.number().min(0, t('setting.maxTokensMinMessage')),
    },
  ],

  [LLMFactory.TencentHunYuan]: [
    {
      name: 'hunyuan_sid',
      label: t('setting.addHunyuanSID'),
      type: 'text',
      required: true,
      placeholder: t('setting.HunyuanSIDMessage'),
      rules: z.string().nonempty(t('setting.HunyuanSIDMessage')),
    },
    {
      name: 'hunyuan_sk',
      label: t('setting.addHunyuanSK'),
      type: 'text',
      required: true,
      placeholder: t('setting.HunyuanSKMessage'),
      rules: z.string().nonempty(t('setting.HunyuanSKMessage')),
    },
  ],

  [LLMFactory.MinerU]: [
    {
      name: 'llm_name',
      label: t('setting.modelName'),
      type: 'text',
      required: true,
      placeholder: t('setting.modelNameMessage'),
      rules: z.string().nonempty(t('setting.modelNameMessage')),
    },
    {
      name: 'mineru_apiserver',
      label: t('setting.mineru.apiserver'),
      type: 'text',
      required: true,
      placeholder: 'http://host.docker.internal:9987',
      rules: z.string().nonempty(t('setting.mineru.apiserverMessage')),
    },
    {
      name: 'mineru_output_dir',
      label: t('setting.mineru.outputDir'),
      type: 'text',
      required: true,
      placeholder: '/tmp/mineru',
      rules: z.string().nonempty(t('setting.mineru.outputDirMessage')),
    },
    {
      name: 'mineru_backend',
      label: t('setting.mineru.backend'),
      type: 'select',
      required: true,
      options: Models.MinerU.backendOptions,
      defaultValue: 'pipeline',
      rules: z.enum(Models.MinerU.backendOptions, {
        message: t('setting.mineru.backendMessage'),
      }),
    },
    {
      name: 'mineru_server_url',
      label: t('setting.mineru.serverUrl'),
      type: 'text',
      required: true,
      placeholder: 'http://your-vllm-server:30000',
      rules: z.string().nonempty(t('setting.mineru.serverUrlMessage')),
      shouldRender: (values: FieldValues) =>
        values.mineru_backend === 'vlm-http-client',
    },
    {
      name: 'mineru_delete_output',
      label: t('setting.mineru.deleteOutput'),
      type: 'switch',
      defaultValue: true,
      rules: z.boolean().optional(),
    },
  ],

  [LLMFactory.TencentCloud]: [
    {
      name: 'model_type',
      label: t('setting.modelType'),
      type: 'select',
      required: true,
      rules: z.enum(Models[LLMFactory.TencentCloud].modelTypes, {
        message: t('setting.modelTypeMessage'),
      }),
      options: Models[LLMFactory.TencentCloud].modelTypes,
      defaultValue: 'speech2text',
    },
    {
      name: 'llm_name',
      label: t('setting.modelName'),
      type: 'select',
      required: true,
      placeholder: t('setting.SparkModelNameMessage'),
      rules: z.enum(Models[LLMFactory.TencentCloud].llmNames, {
        message: t('setting.SparkModelNameMessage'),
      }),
      options: Models[LLMFactory.TencentCloud].llmNames,
      defaultValue: '16k_zh',
    },
    {
      name: 'TencentCloud_sid',
      label: t('setting.addTencentCloudSID'),
      type: 'text',
      required: true,
      placeholder: t('setting.TencentCloudSIDMessage'),
      rules: z.string().nonempty(t('setting.TencentCloudSIDMessage')),
    },
    {
      name: 'TencentCloud_sk',
      label: t('setting.addTencentCloudSK'),
      type: 'text',
      required: true,
      placeholder: t('setting.TencentCloudSKMessage'),
      rules: z.string().nonempty(t('setting.TencentCloudSKMessage')),
    },
  ],

  [LLMFactory.XunFeiSpark]: [
    {
      name: 'model_type',
      label: t('setting.modelType'),
      type: 'select',
      required: true,
      rules: z.enum(Models[LLMFactory.XunFeiSpark].modelTypes, {
        message: t('setting.modelTypeMessage'),
      }),
      options: Models[LLMFactory.XunFeiSpark].modelTypes,
      defaultValue: 'chat',
    },
    {
      name: 'llm_name',
      label: t('setting.modelName'),
      type: 'text',
      required: true,
      placeholder: t('setting.modelNameMessage'),
      rules: z.string().nonempty(t('setting.SparkModelNameMessage')),
    },
    {
      name: 'spark_api_password',
      label: t('setting.addSparkAPIPassword'),
      type: 'text',
      required: true,
      placeholder: t('setting.SparkAPIPasswordMessage'),
      rules: z.string().nonempty(t('setting.SparkAPIPasswordMessage')),
    },
    {
      name: 'spark_app_id',
      label: t('setting.addSparkAPPID'),
      type: 'text',
      required: true,
      placeholder: t('setting.SparkAPPIDMessage'),
      rules: z.string().nonempty(t('setting.SparkAPPIDMessage')),
      shouldRender: (values: FieldValues) => values.model_type === 'tts',
    },
    {
      name: 'spark_api_secret',
      label: t('setting.addSparkAPISecret'),
      type: 'text',
      required: true,
      placeholder: t('setting.SparkAPISecretMessage'),
      rules: z.string().nonempty(t('setting.SparkAPISecretMessage')),
      shouldRender: (values: FieldValues) => values.model_type === 'tts',
    },
    {
      name: 'spark_api_key',
      label: t('setting.addSparkAPIKey'),
      type: 'text',
      required: true,
      placeholder: t('setting.SparkAPIKeyMessage'),
      rules: z.string().nonempty(t('setting.SparkAPIKeyMessage')),
      shouldRender: (values: FieldValues) => values.model_type === 'tts',
    },
    {
      name: 'max_tokens',
      label: t('setting.maxTokens'),
      type: 'number',
      required: true,
      placeholder: t('setting.maxTokensTip'),
      rules: z.number().min(0, t('setting.maxTokensMinMessage')),
    },
  ],

  [LLMFactory.VolcEngine]: [
    {
      name: 'model_type',
      label: t('setting.modelType'),
      type: 'select',
      required: true,
      rules: z.enum(Models[LLMFactory.VolcEngine].modelTypes, {
        message: t('setting.modelTypeMessage'),
      }),
      options: Models[LLMFactory.VolcEngine].modelTypes,
      defaultValue: 'chat',
    },
    {
      name: 'llm_name',
      label: t('setting.modelName'),
      type: 'text',
      required: true,
      placeholder: t('setting.volcModelNameMessage'),
      rules: z.string().nonempty(t('setting.volcModelNameMessage')),
    },
    {
      name: 'endpoint_id',
      label: t('setting.addEndpointID'),
      type: 'text',
      required: true,
      placeholder: t('setting.endpointIDMessage'),
      rules: z.string().nonempty(t('setting.endpointIDMessage')),
    },
    {
      name: 'ark_api_key',
      label: t('setting.addArkApiKey'),
      type: 'text',
      required: true,
      placeholder: t('setting.ArkApiKeyMessage'),
      rules: z.string().nonempty(t('setting.ArkApiKeyMessage')),
    },
    {
      name: 'max_tokens',
      label: t('setting.maxTokens'),
      type: 'number',
      required: true,
      placeholder: t('setting.maxTokensTip'),
      rules: z.number().min(0, t('setting.maxTokensMinMessage')),
    },
  ],

  [LLMFactory.BaiduYiYan]: [
    {
      name: 'model_type',
      label: t('setting.modelType'),
      type: 'select',
      required: true,
      rules: z.enum(Models[LLMFactory.BaiduYiYan].modelTypes, {
        message: t('setting.modelTypeMessage'),
      }),
      options: Models[LLMFactory.BaiduYiYan].modelTypes,
      defaultValue: 'chat',
    },
    {
      name: 'llm_name',
      label: t('setting.modelName'),
      type: 'text',
      required: true,
      placeholder: t('setting.yiyanModelNameMessage'),
      rules: z.string().nonempty(t('setting.yiyanModelNameMessage')),
    },
    {
      name: 'yiyan_ak',
      label: t('setting.addyiyanAK'),
      type: 'text',
      required: true,
      placeholder: t('setting.yiyanAKMessage'),
      rules: z.string().nonempty(t('setting.yiyanAKMessage')),
    },
    {
      name: 'yiyan_sk',
      label: t('setting.addyiyanSK'),
      type: 'text',
      required: true,
      placeholder: t('setting.yiyanSKMessage'),
      rules: z.string().nonempty(t('setting.yiyanSKMessage')),
    },
    {
      name: 'max_tokens',
      label: t('setting.maxTokens'),
      type: 'number',
      required: true,
      placeholder: t('setting.maxTokensTip'),
      rules: z.number().min(0, t('setting.maxTokensMinMessage')),
    },
  ],

  // Local LLMs
  [LLMFactory.Ollama]: [...defaultLocalLlmModelFields],

  [LLMFactory.HuggingFace]: [
    {
      name: 'model_type',
      label: t('setting.modelType'),
      type: 'select',
      required: true,
      rules: z.enum(Models[LLMFactory.HuggingFace].modelTypes, {
        message: t('setting.modelTypeMessage'),
      }),
      options: Models[LLMFactory.HuggingFace].modelTypes,
      defaultValue: 'embedding',
    },
    {
      name: 'llm_name',
      label: t('setting.modelName'),
      type: 'text',
      required: true,
      placeholder: t('setting.modelNameMessage'),
      rules: z.string().nonempty(t('setting.modelNameMessage')),
    },
    {
      name: 'api_base',
      label: t('setting.addLlmBaseUrl'),
      type: 'text',
      required: true,
      placeholder: t('setting.baseUrlNameMessage'),
      rules: z.string().nonempty(t('setting.baseUrlNameMessage')),
    },
    {
      name: 'api_key',
      label: t('setting.apiKey'),
      type: 'text',
      placeholder: t('setting.apiKeyMessage'),
      rules: z.string().optional(),
    },
    {
      name: 'max_tokens',
      label: t('setting.maxTokens'),
      type: 'number',
      required: true,
      placeholder: t('setting.maxTokensTip'),
      rules: z.number().min(0, t('setting.maxTokensMinMessage')),
    },
    {
      name: 'vision',
      label: t('setting.vision'),
      type: 'switch',
      defaultValue: false,
      rules: z.boolean().optional(),
      shouldRender: (values: FieldValues) => values.model_type === 'chat',
    },
  ],

  [LLMFactory.LMStudio]: [
    {
      name: 'model_type',
      label: t('setting.modelType'),
      type: 'select',
      required: true,
      rules: z.enum(Models[LLMFactory.LMStudio].modelTypes, {
        message: t('setting.modelTypeMessage'),
      }),
      options: Models[LLMFactory.LMStudio].modelTypes,
      defaultValue: 'embedding',
    },
    {
      name: 'llm_name',
      label: t('setting.modelName'),
      type: 'text',
      required: true,
      placeholder: t('setting.modelNameMessage'),
      rules: z.string().nonempty(t('setting.modelNameMessage')),
    },
    {
      name: 'api_base',
      label: t('setting.addLlmBaseUrl'),
      type: 'text',
      required: true,
      placeholder: t('setting.baseUrlNameMessage'),
      rules: z.string().nonempty(t('setting.baseUrlNameMessage')),
    },
    {
      name: 'api_key',
      label: t('setting.apiKey'),
      type: 'text',
      placeholder: t('setting.apiKeyMessage'),
      rules: z.string().optional(),
    },
    {
      name: 'max_tokens',
      label: t('setting.maxTokens'),
      type: 'number',
      required: true,
      placeholder: t('setting.maxTokensTip'),
      rules: z.number().min(0, t('setting.maxTokensMinMessage')),
    },
    {
      name: 'vision',
      label: t('setting.vision'),
      type: 'switch',
      defaultValue: false,
      rules: z.boolean().optional(),
      shouldRender: (values: FieldValues) => values.model_type === 'chat',
    },
  ],

  [LLMFactory.Xinference]: [
    {
      name: 'model_type',
      label: t('setting.modelType'),
      type: 'select',
      required: true,
      rules: z.enum(Models[LLMFactory.Xinference].modelTypes, {
        message: t('setting.modelTypeMessage'),
      }),
      options: Models[LLMFactory.Xinference].modelTypes.map((x) => ({
        label: x === 'speech2text' ? 'sequence2text' : x,
        value: x,
      })),
      defaultValue: 'embedding',
    },
    {
      name: 'llm_name',
      label: t('setting.modelUid'),
      type: 'text',
      required: true,
      placeholder: t('setting.modelNameMessage'),
      rules: z.string().nonempty(t('setting.modelNameMessage')),
    },
    {
      name: 'api_base',
      label: t('setting.addLlmBaseUrl'),
      type: 'text',
      required: true,
      placeholder: t('setting.baseUrlNameMessage'),
      rules: z.string().nonempty(t('setting.baseUrlNameMessage')),
    },
    {
      name: 'api_key',
      label: t('setting.apiKey'),
      type: 'text',
      placeholder: t('setting.apiKeyMessage'),
      rules: z.string().optional(),
    },
    {
      name: 'max_tokens',
      label: t('setting.maxTokens'),
      type: 'number',
      required: true,
      placeholder: t('setting.maxTokensTip'),
      rules: z.number().min(0, t('setting.maxTokensMinMessage')),
    },
    {
      name: 'vision',
      label: t('setting.vision'),
      type: 'switch',
      defaultValue: false,
      rules: z.boolean().optional(),
      shouldRender: (values: FieldValues) => values.model_type === 'chat',
    },
  ],

  [LLMFactory.ModelScope]: [
    {
      name: 'model_type',
      label: t('setting.modelType'),
      type: 'select',
      required: true,
      rules: z.enum(Models[LLMFactory.ModelScope].modelTypes, {
        message: t('setting.modelTypeMessage'),
      }),
      options: Models[LLMFactory.ModelScope].modelTypes,
      defaultValue: 'chat',
    },
    {
      name: 'llm_name',
      label: t('setting.modelName'),
      type: 'text',
      required: true,
      placeholder: t('setting.modelNameMessage'),
      rules: z.string().nonempty(t('setting.modelNameMessage')),
    },
    {
      name: 'api_base',
      label: t('setting.addLlmBaseUrl'),
      type: 'text',
      required: true,
      placeholder: t('setting.baseUrlNameMessage'),
      rules: z.string().nonempty(t('setting.baseUrlNameMessage')),
    },
    {
      name: 'api_key',
      label: t('setting.apiKey'),
      type: 'text',
      placeholder: t('setting.apiKeyMessage'),
      rules: z.string().optional(),
    },
    {
      name: 'max_tokens',
      label: t('setting.maxTokens'),
      type: 'number',
      required: true,
      placeholder: t('setting.maxTokensTip'),
      rules: z.number().min(0, t('setting.maxTokensMinMessage')),
    },
    {
      name: 'vision',
      label: t('setting.vision'),
      type: 'switch',
      defaultValue: false,
      rules: z.boolean().optional(),
      shouldRender: (values: FieldValues) => values.model_type === 'chat',
    },
  ],

  [LLMFactory.GPUStack]: [
    {
      name: 'model_type',
      label: t('setting.modelType'),
      type: 'select',
      required: true,
      rules: z.enum(Models[LLMFactory.GPUStack].modelTypes, {
        message: t('setting.modelTypeMessage'),
      }),
      options: Models[LLMFactory.GPUStack].modelTypes.map((x) => ({
        label: x === 'speech2text' ? 'sequence2text' : x,
        value: x,
      })),
      defaultValue: 'embedding',
    },
    {
      name: 'llm_name',
      label: t('setting.modelName'),
      type: 'text',
      required: true,
      placeholder: t('setting.modelNameMessage'),
      rules: z.string().nonempty(t('setting.modelNameMessage')),
    },
    {
      name: 'api_base',
      label: t('setting.addLlmBaseUrl'),
      type: 'text',
      required: true,
      placeholder: t('setting.baseUrlNameMessage'),
      rules: z.string().nonempty(t('setting.baseUrlNameMessage')),
    },
    {
      name: 'api_key',
      label: t('setting.apiKey'),
      type: 'text',
      placeholder: t('setting.apiKeyMessage'),
      rules: z.string().optional(),
    },
    {
      name: 'max_tokens',
      label: t('setting.maxTokens'),
      type: 'number',
      required: true,
      placeholder: t('setting.maxTokensTip'),
      rules: z.number().min(0, t('setting.maxTokensMinMessage')),
    },
    {
      name: 'vision',
      label: t('setting.vision'),
      type: 'switch',
      defaultValue: false,
      rules: z.boolean().optional(),
      shouldRender: (values: FieldValues) => values.model_type === 'chat',
    },
  ],

  [LLMFactory.OpenRouter]: [
    {
      name: 'model_type',
      label: t('setting.modelType'),
      type: 'select',
      required: true,
      rules: z.enum(Models[LLMFactory.OpenRouter].modelTypes, {
        message: t('setting.modelTypeMessage'),
      }),
      options: Models[LLMFactory.OpenRouter].modelTypes,
      defaultValue: 'chat',
    },
    {
      name: 'llm_name',
      label: t('setting.modelName'),
      type: 'text',
      required: true,
      placeholder: t('setting.modelNameMessage'),
      rules: z.string().nonempty(t('setting.modelNameMessage')),
    },
    {
      name: 'api_base',
      label: t('setting.addLlmBaseUrl'),
      type: 'text',
      required: true,
      placeholder: t('setting.baseUrlNameMessage'),
      rules: z.string().nonempty(t('setting.baseUrlNameMessage')),
    },
    {
      name: 'api_key',
      label: t('setting.apiKey'),
      type: 'text',
      placeholder: t('setting.apiKeyMessage'),
      rules: z.string().optional(),
    },
    {
      name: 'max_tokens',
      label: t('setting.maxTokens'),
      type: 'number',
      required: true,
      placeholder: t('setting.maxTokensTip'),
      rules: z.number().min(0, t('setting.maxTokensMinMessage')),
    },
    {
      name: 'provider_order',
      label: 'Provider Order',
      type: 'text',
      labelTooltip: t('setting.openRouterProviderOrderTip'),
      placeholder: 'Groq,Fireworks',
      rules: z.string().optional(),
      defaultValue: 'Groq,Fireworks',
    },
    {
      name: 'vision',
      label: t('setting.vision'),
      type: 'switch',
      defaultValue: false,
      rules: z.boolean().optional(),
      shouldRender: (values: FieldValues) => values.model_type === 'chat',
    },
  ],
};

function _getModelFields(llmFactory: string): FieldConfig[] {
  let thisModelFields =
    modelFields[llmFactory as LLMFactory] ??
    (isLocalLlmFactory(llmFactory)
      ? [...defaultLocalLlmModelFields]
      : [...defaultModelFields]);

  if (typeof thisModelFields[0] === 'string') {
    const [alias, manipulators] = thisModelFields as ModelFieldConfigAlias;
    const fields = _getModelFields(alias);

    thisModelFields = manipulators
      ? fields.map((field) => {
          if (field.type === 'display') {
            return field;
          }

          if (manipulators.override?.[field.name]) {
            return {
              ...field,
              ...manipulators.override?.[field.name],
              name: field.name,
            } as FieldConfig;
          }

          return field;
        })
      : fields;

    if (
      Array.isArray(manipulators?.prepend) ||
      Array.isArray(manipulators?.append)
    ) {
      thisModelFields = [
        ...(manipulators?.prepend ?? []),
        ...thisModelFields,
        ...(manipulators?.append ?? []),
      ];
    }
  }

  return (modelFields[llmFactory as LLMFactory] =
    thisModelFields) as FieldConfig[];
}

export function getModelFields(llmFactory: string): {
  fields: FieldConfig[];
  defaultValues: DefaultValues<FieldValues>;
} {
  const fields = _getModelFields(llmFactory);
  const defaultValues = fields.reduce((acc, field) => {
    if (field.type !== 'display' && field.defaultValue != null) {
      acc[field.name] = field.defaultValue;
    }

    return acc;
  }, {} as DefaultValues<FieldValues>);

  return { fields, defaultValues };
}
