import { IAddedModel } from '@/interfaces/database/llm';
import {
  buildCompatibleModelGroupIds,
  buildModelGroupRef,
  isModelGroupRef,
  normalizeModelTypes,
} from '../model-group-util';

const group = {
  id: '0123456789abcdef0123456789abcdef',
  tenant_id: 'tenant-1',
  nickname: 'Owner',
  group_name: 'chat group',
  status: 'active',
};
const member = {
  id: 'map-1',
  model_id: 'model-1',
  position: 0,
  provider: 'OpenAI',
  instance: 'default',
  model: 'gpt-4',
  model_type: ['chat'],
  status: 'active',
  group_name: group.group_name,
  group_id: group.id,
};
const directModel: IAddedModel = {
  model_id: 'model-1',
  status: 'active',
  instance_status: 'active',
  model_type: ['chat'],
  name: 'gpt-4',
  provider_id: 'provider-1',
  provider_name: 'OpenAI',
  instance_id: 'instance-1',
  instance_name: 'default',
};
const unknownTypeMember = { ...member, id: 'map-unknown', model_type: null };

describe('model group references', () => {
  it('accepts only gp- plus a 32-character UUID', () => {
    expect(isModelGroupRef(buildModelGroupRef(group.id))).toBe(true);
    expect(isModelGroupRef('gp-short')).toBe(false);
    expect(isModelGroupRef('model-1')).toBe(false);
  });
  it('normalizes model type keys deterministically', () => {
    expect(normalizeModelTypes(['vision', 'chat', 'chat'])).toEqual([
      'chat',
      'vision',
    ]);
  });
});

describe('buildCompatibleModelGroupIds', () => {
  it('requires active group, active member, matching type and usable direct model', () => {
    const ids = buildCompatibleModelGroupIds(
      [{ group, members: [member], isError: false }],
      [directModel],
      ['chat'],
    );
    expect(ids).toEqual(new Set([group.id]));
  });
  it('fails closed for an active member with an unknown model type', () => {
    expect(
      buildCompatibleModelGroupIds(
        [{ group, members: [unknownTypeMember], isError: false }],
        [directModel],
        ['chat'],
      ),
    ).toEqual(new Set());
  });
  it('does not let an unknown model type mask a later compatible member', () => {
    expect(
      buildCompatibleModelGroupIds(
        [{ group, members: [unknownTypeMember, member], isError: false }],
        [directModel],
        ['chat'],
      ),
    ).toEqual(new Set([group.id]));
  });
  it.each([
    [{ ...group, status: 'inactive' }, member, directModel, ['chat']],
    [group, { ...member, status: 'inactive' }, directModel, ['chat']],
    [group, member, directModel, ['embedding']],
    [group, member, { ...directModel, status: 'inactive' }, ['chat']],
    [group, member, { ...directModel, instance_status: 'inactive' }, ['chat']],
  ])(
    'rejects an incompatible tuple',
    (testGroup, testMember, testModel, types) => {
      expect(
        buildCompatibleModelGroupIds(
          [{ group: testGroup, members: [testMember], isError: false }],
          [testModel],
          types,
        ),
      ).toEqual(new Set());
    },
  );
  it('fails closed when a detail query failed', () => {
    expect(
      buildCompatibleModelGroupIds(
        [{ group, members: [member], isError: true }],
        [directModel],
        ['chat'],
      ),
    ).toEqual(new Set());
  });
  it('applies an explicit provider restriction to group options', () => {
    expect(
      buildCompatibleModelGroupIds(
        [{ group, members: [member], isError: false }],
        [directModel],
        ['chat'],
        ['SystemOne'],
      ),
    ).toEqual(new Set());
    expect(
      buildCompatibleModelGroupIds(
        [{ group, members: [member], isError: false }],
        [directModel],
        ['chat'],
        ['OpenAI'],
      ),
    ).toEqual(new Set([group.id]));
  });
});
