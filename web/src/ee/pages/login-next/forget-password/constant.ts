import { FormFieldConfig, FormFieldType } from '@/components/dynamic-form';
import { t } from 'i18next';

export const formFields = [
  {
    name: 'email',
    label: t('login.emailLabel'),
    type: FormFieldType.Email,
    placeholder: t('login.emailPlaceholder'),
    required: true,
  },
  {
    name: 'verifyCode',
    label: t('login.verificationCode'),
    type: FormFieldType.Text,
    placeholder: t('login.verifyCodePlaceholder'),
    required: true,
  },
  {
    name: 'password',
    label: t('login.passwordLabel'),
    type: FormFieldType.Password,
    placeholder: t('login.passwordPlaceholder'),
    required: true,
  },
  {
    name: 'confirmPassword',
    label: t('login.confirmPassword'),
    type: FormFieldType.Password,
    placeholder: t('login.confirmPasswordPlaceholder'),
    required: true,
    customValidate: (value, formValues) => {
      if (value !== formValues.password) {
        return t('login.confirmPasswordError');
      }
    },
  },
] as FormFieldConfig[];
