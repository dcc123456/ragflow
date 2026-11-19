import type { INextOperatorForm } from '@/pages/agent/interface';
import CadChunkerForm from './cad-chunker-form';

const CadChunkerOperator = 'CADChunker';

export function EnterpriseOperatorForm(props: INextOperatorForm) {
  if (props.node?.data?.label !== CadChunkerOperator) {
    return null;
  }
  return <CadChunkerForm {...props} />;
}
