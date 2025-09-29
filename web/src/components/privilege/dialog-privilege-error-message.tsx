import { TeamRole } from '@/constants/team';
import { Failed } from '@/interfaces/database/team';
import { isEmpty } from 'lodash';
import { useTranslation } from 'react-i18next';
import { Card, CardContent } from '../ui/card';

type DialogPrivilegeErrorMessageProps = {
  data: (Failed & { type: TeamRole })[];
};

export function DialogPrivilegeErrorMessage({
  data,
}: DialogPrivilegeErrorMessageProps) {
  const { t } = useTranslation();

  return (
    <section className="w-full ">
      <h2 className="pb-3">{t('permission.pleaseShareTheModelFirst')}</h2>
      <ul className="text-colors-text-neutral-strong space-y-4">
        {data.map((x) => (
          <Card key={x.id}>
            <CardContent className="p-1">
              <li>
                <div className="flex items-center gap-2 font-semibold">
                  {x.type}:<h4 className="pb-1  text-lg border-b">{x.name}</h4>
                </div>
                {x.llm_factory && (
                  <div>
                    <span className="font-semibold">Model:</span>
                    <p className=" pl-2">{x.llm_factory}</p>
                  </div>
                )}
                {!isEmpty(x.kbs) && (
                  <div>
                    <span className="font-semibold">Knowledge base:</span>
                    <ul className="pl-2 ">
                      {x.kbs.map((y) => (
                        <li key={y.id}>{y.name}</li>
                      ))}
                    </ul>
                  </div>
                )}
              </li>
            </CardContent>
          </Card>
        ))}
      </ul>
    </section>
  );
}
