import ApiKeyFormGroup from './api-key';

describe('API key settings', () => {
  it('defaults to 16 and maps persisted limits to the settings form', () => {
    expect(
      ApiKeyFormGroup.mapValuesFromData({} as AdminServiceEe.SystemVariables),
    ).toEqual({ maxCount: 16 });
    const variables = {
      'api_key.max_count': {
        source: 'variable',
        name: 'api_key.max_count',
        data_type: 'integer',
        value: 32,
      },
    } as AdminServiceEe.SystemVariables;
    expect(ApiKeyFormGroup.mapValuesFromData(variables)).toEqual({
      maxCount: 32,
    });
    expect(ApiKeyFormGroup.mapValuesToData({ maxCount: 32 })).toEqual({
      'api_key.max_count': 32,
    });
  });

  it.each([undefined, '', 0, -1, 1.5, NaN, Infinity])(
    'rejects invalid maximum %s',
    (maxCount) => {
      expect(ApiKeyFormGroup.schema.safeParse({ maxCount }).success).toBe(
        false,
      );
    },
  );

  it.each([1, 16, 32])('accepts positive integer %s', (maxCount) => {
    expect(ApiKeyFormGroup.schema.safeParse({ maxCount }).success).toBe(true);
  });
});
