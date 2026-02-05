import { FieldValues } from 'react-hook-form';
import z from 'zod';

type BaseFieldProps = {
  name: string;
  label?: string;
  labelTooltip?: string;
  placeholder?: string;
  required?: boolean;
  rules: z.Schema<any, any>;
  shouldRender?: <T extends FieldValues>(values: T) => boolean;
  clearOnHide?: boolean;
};

type OptionDefs =
  | readonly string[]
  | readonly {
      value: string;
      label?: string;
    }[];

type GetOptionDefsValue<Def extends OptionDefs> = Def extends unknown[]
  ? Def[number]
  : T[number]['value'];

export type FieldConfigTypeDisplay = Pick<BaseFieldProps, 'shouldRender'> & {
  type: 'display';
  element?: React.ReactElement;
  Component?: React.ComponentType;
};

export type FieldConfigTypeText = BaseFieldProps & {
  type: 'text' | 'email' | 'password';
  defaultValue?: string;
};

export type FieldConfigTypeNumber = BaseFieldProps & {
  type: 'number';
  defaultValue?: number;
};

export type FieldConfigTypeSwitch = Omit<BaseFieldProps, 'placeholder'> & {
  type: 'switch';
  defaultValue?: boolean;
};

export type FieldConfigTypeCheckbox = Omit<BaseFieldProps, 'placeholder'> & {
  type: 'checkbox';
  3;
  defaultValue?: boolean;
};

export type FieldConfigTypeRadioGroup = Omit<BaseFieldProps, 'placeholder'> & {
  type: 'radio-group';
  defaultValue?: string;
  options: OptionDefs;
};

export type FieldConfigTypeSelect = BaseFieldProps & {
  type: 'select';
  searchable?: boolean;
  defaultValue?: string;
  options: OptionDefs;
};

export type FieldConfigTypeSegmented = Omit<BaseFieldProps, 'placeholder'> & {
  type: 'segmented';
  defaultValue?: string;
  options: OptionDefs;
};

export type InteractiveFieldConfig =
  | FieldConfigTypeText
  | FieldConfigTypeNumber
  | FieldConfigTypeSelect
  | FieldConfigTypeSwitch
  | FieldConfigTypeCheckbox
  | FieldConfigTypeRadioGroup
  | FieldConfigTypeSegmented;

export type FieldConfig = FieldConfigTypeDisplay | InteractiveFieldConfig;
