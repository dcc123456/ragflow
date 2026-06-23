import { useSetModalState } from '@/hooks/common-hooks';

import { useFetchKnowledgeBaseConfiguration } from '@/hooks/use-knowledge-request';
import { useSelectParserList } from '@/hooks/use-user-setting-request';
import kbService, {
  checkEmbedding,
  traceEmbedding,
} from '@/services/knowledge-service';
import {
  useIsFetching,
  useMutation,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query';
import { pick } from 'lodash';
import { useCallback, useEffect, useState } from 'react';
import { UseFormReturn } from 'react-hook-form';
import { useParams, useSearchParams } from 'react-router';
import { z } from 'zod';
import { formSchema } from './form-schema';

// The value that does not need to be displayed in the analysis method Select
const HiddenFields = ['email', 'picture', 'audio'];

export function useSelectChunkMethodList() {
  const parserList = useSelectParserList();

  return parserList.filter((x) => !HiddenFields.some((y) => y === x.value));
}

export function useHasParsedDocument(isEdit?: boolean) {
  const { data: knowledgeDetails } = useFetchKnowledgeBaseConfiguration({
    isEdit,
  });
  return knowledgeDetails.chunk_count > 0;
}

export const useFetchKnowledgeConfigurationOnMount = (
  form: UseFormReturn<z.infer<typeof formSchema>>,
) => {
  const { data: knowledgeDetails, loading } =
    useFetchKnowledgeBaseConfiguration();

  useEffect(() => {
    const parser_config = {
      ...form.formState?.defaultValues?.parser_config,
      ...knowledgeDetails.parser_config,
      raptor: {
        ...form.formState?.defaultValues?.parser_config?.raptor,
        ...knowledgeDetails.parser_config?.raptor,
        clustering_method:
          knowledgeDetails.parser_config?.raptor?.ext?.clustering_method,
        use_raptor: true,
      },
      graphrag: {
        ...form.formState?.defaultValues?.parser_config?.graphrag,
        ...knowledgeDetails.parser_config?.graphrag,
        use_graphrag: true,
      },
    };
    const formValues = {
      ...pick({ ...knowledgeDetails, parser_config: parser_config }, [
        'description',
        'name',
        // 'permission',
        'language',
        'parser_config',
        'connectors',
        'pagerank',
        'avatar',
      ]),
      embedding_model: knowledgeDetails.embedding_model,
      chunk_method: knowledgeDetails.chunk_method,
    } as unknown as z.infer<typeof formSchema>;
    form.reset(formValues);
  }, [form, knowledgeDetails]);

  return { knowledgeDetails, loading };
};

export const useSelectKnowledgeDetailsLoading = () =>
  useIsFetching({ queryKey: ['fetchKnowledgeDetail'] }) > 0;

export const useRenameKnowledgeTag = () => {
  const [tag, setTag] = useState<string>('');
  const {
    visible: tagRenameVisible,
    hideModal: hideTagRenameModal,
    showModal: showFileRenameModal,
  } = useSetModalState();

  const handleShowTagRenameModal = useCallback(
    (record: string) => {
      setTag(record);
      showFileRenameModal();
    },
    [showFileRenameModal],
  );

  return {
    initialName: tag,
    tagRenameVisible,
    hideTagRenameModal,
    showTagRenameModal: handleShowTagRenameModal,
  };
};

export const useHandleKbEmbedding = () => {
  const { id } = useParams();
  const [searchParams] = useSearchParams();
  const knowledgeBaseId = searchParams.get('id') || id;

  const handleChange = useCallback(
    async ({ embed_id }: { embed_id: string }) => {
      const res = await checkEmbedding(knowledgeBaseId || '', {
        embd_id: embed_id,
      });
      return res.data;
    },
    [knowledgeBaseId],
  );
  return {
    handleChange,
  };
};

export const useKbSwitchEmbeddingModel = () => {
  const { id } = useParams();
  const [searchParams] = useSearchParams();
  const queryClient = useQueryClient();
  const knowledgeBaseId = searchParams.get('id') || id;

  const { mutateAsync, isPending } = useMutation({
    mutationKey: ['kbSwitchEmbeddingModel'],
    mutationFn: async (embd_id: string) => {
      const { data = {} } = await kbService.switchEmbeddingModel({
        kb_id: knowledgeBaseId,
        embd_id,
      });

      if (data.code !== 0) {
        throw new Error(data.message);
      }

      queryClient.invalidateQueries({
        queryKey: ['traceEmbedding', knowledgeBaseId],
      });

      return data;
    },
  });

  return {
    switchEmbeddingModel: mutateAsync,
    isLoading: isPending,
  };
};

export const useTraceEmbedding = () => {
  const { id } = useParams();
  const [searchParams] = useSearchParams();
  const knowledgeBaseId = searchParams.get('id') || id;

  const { data, refetch } = useQuery({
    queryKey: ['traceEmbedding', knowledgeBaseId],
    queryFn: async () => {
      const { data } = await traceEmbedding(knowledgeBaseId!);
      return data?.data || {};
    },
    enabled: !!id,
  });

  const hasProgress =
    data?.progress != null && data.progress >= 0 && data?.progress < 1;

  useEffect(() => {
    if (hasProgress) {
      const interval = window.setInterval(() => {
        refetch();
      }, 2000);

      return () => {
        window.clearInterval(interval);
      };
    }
  }, [hasProgress, refetch]);

  return {
    data,
    hasProgress,
    progress: data?.progress,
    finished: data?.progress >= 1,
    errored: data?.progress === -1,
  };
};
