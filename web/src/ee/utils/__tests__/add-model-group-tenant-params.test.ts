import { addTenantParams } from '@/utils/llm-util';
import { addModelGroupTenantParams } from '../add-model-group-tenant-params';

const groupRef = 'gp-0123456789abcdef0123456789abcdef';
const cases = [
  ['llm_id', 'tenant_llm_id'],
  ['embd_id', 'tenant_embd_id'],
  ['asr_id', 'tenant_asr_id'],
  ['tts_id', 'tenant_tts_id'],
  ['img2txt_id', 'tenant_img2txt_id'],
  ['rerank_id', 'tenant_rerank_id'],
  ['decision_id', 'tenant_decision_id'],
] as const;

it.each(cases)('maps %s without a URL whitelist', (field, tenantField) => {
  expect(addModelGroupTenantParams({ [field]: groupRef })).toEqual({
    [field]: groupRef,
    [tenantField]: groupRef,
  });
});

it('maps every item in nested arrays', () => {
  expect(
    addModelGroupTenantParams([{ nodes: [{ embd_id: groupRef }] }]),
  ).toEqual([{ nodes: [{ embd_id: groupRef, tenant_embd_id: groupRef }] }]);
});

it('clears stale group ids when the selection is cleared or replaced', () => {
  expect(
    addModelGroupTenantParams({ llm_id: null, tenant_llm_id: groupRef }),
  ).toEqual({
    llm_id: null,
    tenant_llm_id: null,
  });
  expect(
    addModelGroupTenantParams({
      llm_id: 'direct-model',
      tenant_llm_id: groupRef,
    }),
  ).toEqual({
    llm_id: 'direct-model',
    tenant_llm_id: null,
  });
});

it('preserves a malformed gp- value and its existing tenant field', () => {
  expect(
    addModelGroupTenantParams({
      llm_id: 'gp-short',
      tenant_llm_id: 'existing',
    }),
  ).toEqual({
    llm_id: 'gp-short',
    tenant_llm_id: 'existing',
  });
});

describe('addTenantParams — model group references', () => {
  it('maps chat llm, rerank and decision references outside the direct-model whitelist', () => {
    expect(
      addTenantParams(
        {
          llm_id: groupRef,
          rerank_id: groupRef,
          decision_id: groupRef,
          prompt: { llm_id: groupRef },
        },
        '/api/v1/chats/chat-id',
      ),
    ).toEqual({
      llm_id: groupRef,
      tenant_llm_id: groupRef,
      rerank_id: groupRef,
      tenant_rerank_id: groupRef,
      decision_id: groupRef,
      tenant_decision_id: groupRef,
      prompt: { llm_id: groupRef, tenant_llm_id: groupRef },
    });
  });

  it('clears a stale group tenant id when the selection is cleared or replaced', () => {
    expect(
      addTenantParams(
        { llm_id: '', tenant_llm_id: groupRef },
        '/api/v1/chats/chat-id',
      ),
    ).toEqual({ llm_id: '', tenant_llm_id: null });
    expect(
      addTenantParams(
        { llm_id: 'direct-model', tenant_llm_id: groupRef },
        '/api/v1/chats/chat-id',
      ),
    ).toEqual({ llm_id: 'direct-model', tenant_llm_id: null });
  });

  it('does not turn a malformed gp- value into a tenant reference', () => {
    expect(
      addTenantParams(
        { llm_id: 'gp-short', tenant_llm_id: 'existing' },
        '/api/v1/chats/chat-id',
      ),
    ).toEqual({ llm_id: 'gp-short', tenant_llm_id: 'existing' });
  });

  it('preserves direct tenant ids in nested non-whitelisted chat payloads', () => {
    expect(
      addTenantParams(
        { prompt: { llm_id: '', tenant_llm_id: 'direct-tenant' } },
        '/api/v1/chats/chat-id',
      ),
    ).toEqual({
      prompt: { llm_id: '', tenant_llm_id: 'direct-tenant' },
    });
    expect(
      addTenantParams(
        { prompt: { embd_id: null, tenant_embd_id: 'direct-tenant' } },
        '/api/v1/chats/chat-id',
      ),
    ).toEqual({
      prompt: { embd_id: null, tenant_embd_id: 'direct-tenant' },
    });
  });

  it('preserves direct tenant ids in array non-whitelisted chat payloads', () => {
    expect(
      addTenantParams(
        [{ llm_id: '', tenant_llm_id: 'direct-tenant' }],
        '/api/v1/chats/chat-id',
      ),
    ).toEqual([{ llm_id: '', tenant_llm_id: 'direct-tenant' }]);
  });

  it('preserves special object identities in non-whitelisted chat payloads', () => {
    class ChatMetadata {
      constructor(readonly source: string) {}
    }

    const date = new Date('2026-01-02T03:04:05.000Z');
    const map = new Map([['source', 'upload']]);
    const blob = new Blob(['chat payload'], { type: 'text/plain' });
    const file = new File(['chat payload'], 'chat.txt', {
      type: 'text/plain',
    });
    const metadata = new ChatMetadata('upload');
    const result = addTenantParams(
      { prompt: { date, map, blob, file, metadata } },
      '/api/v1/chats/chat-id',
    );

    expect(result.prompt.date).toBe(date);
    expect(result.prompt.map).toBe(map);
    expect(result.prompt.blob).toBe(blob);
    expect(result.prompt.file).toBe(file);
    expect(result.prompt.metadata).toBe(metadata);
  });
});
