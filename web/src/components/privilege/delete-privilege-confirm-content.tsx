import { PermissionResourceType } from '@/constants/team';
import { useFetchKnowledgeList } from '@/hooks/use-knowledge-request';
import { useFetchConfirmDeletePermission } from '@/hooks/use-team';
import { IConfirmDeletePermission } from '@/interfaces/request/team';
import { isEmpty } from 'lodash';
import { useTranslation } from 'react-i18next';
import { Card, CardContent, CardHeader, CardTitle } from '../ui/card';

export function DeletePrivilegeConfirmContent({
  params,
}: {
  params: IConfirmDeletePermission;
}) {
  const { t } = useTranslation();
  const isLlm = params.resource_type === PermissionResourceType.LLM;
  const { data } = useFetchConfirmDeletePermission(params);
  const { list: knowledgeList } = useFetchKnowledgeList(
    false,
    params.resource_type === PermissionResourceType.KnowledgeBase,
  );

  if (isEmpty(data)) {
    return <div></div>;
  }
  return (
    <div>
      {Object.entries(data).map(([key, value]) => (
        <div key={key}>
          {Object.entries(value).map(([type, list]) => (
            <Card key={type} className="p-2">
              <CardHeader className="p-0 pb-1 border-b">
                <CardTitle className="text-base">
                  {t('permission.deleteConfirmMessage', {
                    id: isLlm
                      ? key
                      : knowledgeList.find((x) => x.id === key)?.name,
                    type,
                  })}
                </CardTitle>
              </CardHeader>
              <CardContent className="p-0">
                <ul>
                  <li>
                    {list.map((x) => (
                      <li key={x.id}>{x.name}</li>
                    ))}
                  </li>
                </ul>
              </CardContent>
            </Card>
          ))}
        </div>
      ))}
    </div>
  );
}
