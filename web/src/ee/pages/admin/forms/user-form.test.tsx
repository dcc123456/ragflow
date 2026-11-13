import { render, screen } from '@testing-library/react';
import { useForm } from 'react-hook-form';

import { CreateUserForm } from './user-form';

jest.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

function TestForm() {
  const form = useForm({
    defaultValues: { email: '', password: '', confirmPassword: '' },
  });

  return <CreateUserForm id="create-user" form={form} />;
}

describe('CreateUserForm', () => {
  it('does not show the unsupported user role field', () => {
    render(<TestForm />);

    expect(screen.queryByText('admin.role')).not.toBeInTheDocument();
  });
});
