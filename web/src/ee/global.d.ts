// eslint-disable @typescript-eslint/no-unused-vars
type ValueOf<T extends object> = T[keyof T];
type PrefixKey<K extends string, P extends string> = `${P}${K}`;
type PrefixKeys<
  T extends object,
  P extends string | number = '',
  S extends string = '.',
> = {
  [K in keyof T as `${K extends '' ? K : PrefixKey<K, `${P}${S}`>}`]: T[K];
};
type UnprefixKey<
  K extends string,
  S extends string,
> = K extends `${S}${infer Rest}` ? Rest : K;
type UnprefixKeys<T extends object, S extends string> = {
  [K in keyof T as UnprefixKey<K, S>]: T[K];
};
