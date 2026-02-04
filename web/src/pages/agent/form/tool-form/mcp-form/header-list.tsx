import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Trash2 } from 'lucide-react';
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { PromptEditor } from '../../components/prompt-editor';

interface HeaderItemProps {
  keyName: string;
  keyValue: string;
  index: number;
  onUpdate: (index: number, newKey: string, newValue: string) => void;
  onDelete: (index: number) => void;
  placeholderKey?: string;
  placeholderValue?: string;
}

const HeaderItem = ({
  keyName,
  keyValue,
  index,
  onUpdate,
  onDelete,
  placeholderKey,
  placeholderValue,
}: HeaderItemProps) => {
  const [editingKey, setEditingKey] = useState(keyName);
  const [editingValue, setEditingValue] = useState(keyValue || '');
  const { t } = useTranslation();

  const handleKeyChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setEditingKey(e.target.value);
  };

  const handleValueChange = (newValue?: string) => {
    setEditingValue(newValue || '');
  };

  useEffect(() => {
    if (keyName !== editingKey || keyValue !== editingValue) {
      onUpdate(index, editingKey, editingValue);
    }
  }, [editingKey, editingValue, index, onUpdate, keyName, keyValue]);

  return (
    <div className="flex flex-row items-center space-x-2">
      <Input
        value={editingKey}
        onChange={handleKeyChange}
        placeholder={placeholderKey || t('flow.header_key')}
        className="w-40"
      />
      <div className="flex-1">
        <PromptEditor
          placeholder={placeholderValue || t('flow.header_value')}
          value={editingValue}
          onChange={(value) => {
            handleValueChange(value);
          }}
          multiLine={false}
          showToolbar={false}
        ></PromptEditor>
      </div>
      <Button type="button" variant={'delete'} onClick={() => onDelete(index)}>
        <Trash2 />
      </Button>
    </div>
  );
};

interface HeaderListProps {
  headers: Record<string, string>;
  onChange: (headers: Record<string, string>) => void;
}

export const HeaderList = ({ headers, onChange }: HeaderListProps) => {
  const { t } = useTranslation();

  const headerEntries = Object.entries(headers || {});

  const handleUpdate = (index: number, newKey: string, newValue: string) => {
    const updatedHeaders: Record<string, string> = {};

    headerEntries.forEach(([key, value], i) => {
      if (i === index) {
        updatedHeaders[newKey] = newValue;
      } else {
        updatedHeaders[key] = value;
      }
    });
    onChange(updatedHeaders);
  };

  const handleDelete = (index: number) => {
    const updatedHeaders: Record<string, string> = {};

    headerEntries.forEach(([key, value], i) => {
      if (i !== index) {
        updatedHeaders[key] = value;
      }
    });

    onChange(updatedHeaders);
  };

  return (
    <div className="space-y-4">
      {headerEntries.map(([key, value], index) => (
        <HeaderItem
          key={`${key}-${index}`}
          keyName={key}
          keyValue={value}
          index={index}
          onUpdate={handleUpdate}
          onDelete={handleDelete}
          placeholderKey={t('flow.headerKey')}
          placeholderValue={t('flow.headerValue')}
        />
      ))}
    </div>
  );
};
