'use client';
import { identity, noop, stubFalse, stubString } from 'lodash';

import React, {
  createContext,
  forwardRef,
  Fragment,
  useCallback,
  useContext,
  useLayoutEffect,
  useMemo,
  useState,
} from 'react';

import * as AccordionPrimitive from '@radix-ui/react-accordion';
import { CheckedState } from '@radix-ui/react-checkbox';

import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { cn } from '@/lib/utils';
import {
  ChevronRight,
  LucideFile,
  LucideFolder,
  LucideFolderOpen,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';

function _defaultIsLeafFn(item: TreeDataItem) {
  return !item.children;
}

type TreeSingleSelectProps = {
  multiple?: false;
  value?: string;
  onSelectChange?: (item: string) => void;
};

type TreeMultipleSelectProps = {
  multiple: true;
  value?: string[];
  onSelectChange?: (items: string[]) => void;
};

export type TreeDataItem<IdProp extends string = string> = {
  name: string;
  icon?: any;
  selectedIcon?: any;
  openIcon?: any;
  children?: TreeDataItem<IdProp>[];
  actions?: React.ReactNode;
  onClick?: () => void;
} &
  // When IdProp is a specific string literal (like "token"), require that property
  (string extends IdProp
    ? { [key: string]: any } // Default case (IdProp = string): be permissive
    : { [K in IdProp]: string } & { [key: string]: any }); // Specific literal: require IdProp property but allow others

type TreeBaseProps<IdProp extends string = string> = {
  className?: string;
  disabled?: boolean;
  data: TreeDataItem<IdProp>[] | TreeDataItem<IdProp>;
  initialSelectedItemId?: string;
  expandAll?: boolean;
};

type TreeProps<IdProp extends string = string> = TreeBaseProps<IdProp> &
  (TreeSingleSelectProps | TreeMultipleSelectProps);

type TreeRootProps<IdProp extends string = string> = TreeProps<IdProp> & {
  idProp?: IdProp;
  containerClassName?: string;
  showSelectAll?: boolean;
  defaultNodeIcon?: any;
  defaultNodeOpenIcon?: any;
  defaultLeafIcon?: any;

  isLeafNode?: (item: TreeDataItem<IdProp>) => boolean;
};

type TreeItemProps<IdProp extends string = string> = {
  data: TreeDataItem<IdProp>[] | TreeDataItem<IdProp>;
  expandedItemIds: string[];
};

type TreeUtilityContextValue = {
  getItemId<IdProp extends string = string>(item: TreeDataItem<IdProp>): string;
  isLeafNode<IdProp extends string = string>(
    item: TreeDataItem<IdProp>,
  ): boolean;
};

const TreeUtilityContext = createContext<TreeUtilityContextValue>({
  getItemId: stubString,
  isLeafNode: _defaultIsLeafFn,
});

const TreeSelectionContext = createContext<{
  multiple: boolean;
  onSelect<IdProp extends string = string>(item: TreeDataItem<IdProp>): void;
  isItemSelected<IdProp extends string = string>(
    item: TreeDataItem<IdProp>,
  ): CheckedState;
  getProcessedItem<IdProp extends string = string>(
    item: TreeDataItem<IdProp>,
  ): TreeDataItemWithPath<IdProp>;
}>({
  multiple: false,
  onSelect: noop,
  isItemSelected: stubFalse,
  getProcessedItem: identity,
});

const TreeDefaultIconContext = createContext({
  node: LucideFolder,
  nodeOpen: LucideFolderOpen,
  leaf: LucideFile,
});

const TreeIcon = ({
  item,
  isOpen,
  isSelected,
  default: defaultIcon,
}: {
  item: TreeDataItem;
  isOpen?: boolean;
  isSelected?: CheckedState;
  default?: any;
}) => {
  const { isLeafNode } = useContext(TreeUtilityContext);
  const ctxIcons = useContext(TreeDefaultIconContext);
  const isNode = !isLeafNode(item);

  let Icon = defaultIcon;

  if (isSelected === true && item.selectedIcon) {
    Icon = item.selectedIcon;
  } else if (isNode) {
    Icon = isOpen
      ? (item.openIcon ?? ctxIcons.nodeOpen)
      : (item.icon ?? ctxIcons.node);
  } else {
    Icon = item.icon ?? ctxIcons.leaf;
  }

  return Icon ? <Icon className="flex-none size-[1em] mr-1" /> : null;
};

const TreeActions = ({
  children,
  isSelected,
}: {
  children: React.ReactNode;
  isSelected: boolean;
}) => {
  return (
    <div
      className={cn(
        isSelected ? 'block' : 'hidden',
        'absolute right-3 group-hover:block',
      )}
    >
      {children}
    </div>
  );
};

// eslint-disable-next-line @typescript-eslint/no-use-before-define
function TreeNode({
  item,
  expandedItemIds,
}: {
  item: TreeDataItem;
  expandedItemIds: string[];
}) {
  const { getItemId } = useContext(TreeUtilityContext);
  const itemId = getItemId(item);

  const [value, setValue] = useState(
    expandedItemIds.includes(itemId) ? [itemId] : [],
  );

  const {
    multiple: multipleSelect,
    isItemSelected,
    onSelect,
  } = useContext(TreeSelectionContext);

  const isOpen = value.includes(itemId);
  const isSelected = isItemSelected(item);

  return (
    <AccordionPrimitive.Root
      asChild
      type="multiple"
      value={value}
      onValueChange={(s) => setValue(s)}
    >
      <AccordionPrimitive.Item asChild value={itemId}>
        <li
          role="treeitem"
          aria-expanded={isOpen}
          aria-label={item.name}
          {...(multipleSelect
            ? {
                'aria-checked':
                  isSelected === 'indeterminate' ? 'mixed' : isSelected,
              }
            : { 'aria-selected': !!isSelected })}
        >
          <div className="flex items-center">
            {multipleSelect ? (
              <Checkbox
                className="ml-2 mr-1"
                checked={isSelected}
                onCheckedChange={() => {
                  onSelect(item);
                }}
              />
            ) : null}

            <AccordionPrimitive.Trigger asChild>
              <Button
                variant="transparent"
                className={cn(
                  'gap-0 border-none flex-auto flex flex-row justify-start px-2 py-2',
                  isSelected && 'text-text-primary',
                )}
                onClick={() => item.onClick?.()}
              >
                <ChevronRight
                  className={cn(
                    'size-[1em] flex-none transition-transform duration-200 text-accent-foreground/50 mr-2',
                    isOpen && 'rotate-90',
                  )}
                />

                <TreeIcon item={item} isSelected={isSelected} isOpen={isOpen} />

                <span className="text-sm truncate">{item.name}</span>

                <TreeActions isSelected={!!isSelected}>
                  {item.actions}
                </TreeActions>
              </Button>
            </AccordionPrimitive.Trigger>
          </div>

          <AccordionPrimitive.Content asChild>
            {/* eslint-disable-next-line @typescript-eslint/no-use-before-define */}
            <TreeItem
              data={item.children ? item.children : item}
              expandedItemIds={expandedItemIds}
              className="border-l pl-2 ml-3.5 overflow-hidden text-sm transition-all data-[state=closed]:animate-accordion-up data-[state=open]:animate-accordion-down"
              role="group"
            />
          </AccordionPrimitive.Content>
        </li>
      </AccordionPrimitive.Item>
    </AccordionPrimitive.Root>
  );
}

const TreeLeaf = forwardRef<
  HTMLLIElement,
  React.HTMLAttributes<HTMLLIElement> & {
    item: TreeDataItem;
  }
>(function TreeLeaf({ item, className, ...props }, ref) {
  const {
    multiple: multipleSelect,
    onSelect,
    isItemSelected,
  } = useContext(TreeSelectionContext);

  const isSelected = isItemSelected(item);

  const content = (
    <>
      <TreeIcon item={item} isSelected={isSelected} />
      <span className="flex-grow text-sm truncate">{item.name}</span>
      <TreeActions isSelected={!!isSelected}>{item.actions}</TreeActions>
    </>
  );

  return (
    <li
      ref={ref}
      className={cn(
        'flex flex-row items-center text-text-secondary',
        isSelected && 'text-text-primary',
        className,
      )}
      role="treeitem"
      aria-label={item.name}
      {...(multipleSelect
        ? {
            'aria-checked':
              isSelected === 'indeterminate' ? 'mixed' : isSelected,
          }
        : { 'aria-selected': !!isSelected })}
      {...props}
    >
      {multipleSelect ? (
        <>
          <Checkbox
            className="ml-2 mr-1"
            checked={isSelected}
            onCheckedChange={() => {
              onSelect(item);
            }}
          />

          <div
            className="flex-auto flex flex-row items-center px-2 py-2"
            onClick={() => item.onClick?.()}
          >
            {content}
          </div>
        </>
      ) : (
        <Button
          variant={isSelected ? 'default' : 'transparent'}
          className={cn(
            'gap-0 border-none flex-auto flex flex-row justify-start items-center px-2 py-2 text-left',
          )}
          onClick={() => {
            item.onClick?.();
            onSelect(item);
          }}
        >
          {content}
        </Button>
      )}
    </li>
  );
});

const TreeItem = forwardRef<
  HTMLUListElement,
  TreeItemProps & React.HTMLAttributes<HTMLUListElement>
>(function TreeItem({ className, data, expandedItemIds, ...props }, ref) {
  if (!(data instanceof Array)) {
    data = [data];
  }

  const { getItemId, isLeafNode } = useContext(TreeUtilityContext);

  return (
    <ul ref={ref} className={className} {...props}>
      {data.map((item, i) => (
        <Fragment key={getItemId(item) || i}>
          {isLeafNode(item) ? (
            <TreeLeaf item={item} />
          ) : (
            <TreeNode item={item} expandedItemIds={expandedItemIds} />
          )}
        </Fragment>
      ))}
    </ul>
  );
});

const TreeViewInner = forwardRef<HTMLUListElement, Omit<TreeProps, 'value'>>(
  function TreeViewInner(
    { data, initialSelectedItemId, expandAll, multiple = false, ...props },
    ref,
  ) {
    const expandedItemIds = useMemo(() => {
      if (!initialSelectedItemId) {
        return [] as string[];
      }

      const ids: string[] = [];

      function walkTreeItems(
        items: TreeDataItem[] | TreeDataItem,
        targetId: string,
      ) {
        if (items instanceof Array) {
          for (let i = 0; i < items.length; i++) {
            ids.push(items[i]!.id);
            if (walkTreeItems(items[i]!, targetId) && !expandAll) {
              return true;
            }
            if (!expandAll) ids.pop();
          }
        } else if (!expandAll && items.id === targetId) {
          return true;
        } else if (items.children) {
          return walkTreeItems(items.children, targetId);
        }
      }

      walkTreeItems(data, initialSelectedItemId);
      return ids;
    }, [data, expandAll, initialSelectedItemId]);

    return (
      <TreeItem
        data={data}
        ref={ref}
        expandedItemIds={expandedItemIds}
        {...props}
        role="tree"
        aria-multiselectable={!!multiple}
      />
    );
  },
);

type TreeDataItemWithPath<IdProp extends string = 'id'> =
  TreeDataItem<IdProp> & {
    idPath: string[];
    namePath: string[];
    __original: TreeDataItem<IdProp>;
  };

function flattenTreeData<IdProp extends string = 'id'>(
  data: TreeDataItem<IdProp>[] | TreeDataItem<IdProp>,
  idProp: IdProp,
  parentIdPath: string[] = [],
  parentNamePath: string[] = [],
): TreeDataItemWithPath<IdProp>[] {
  const _data = Array.isArray(data) ? data : [data];

  return _data.flatMap((item) => {
    // Invalid item id, throw error
    if (item[idProp] == null) {
      throw new Error(`[TreeView] Item ID is not valid.`);
    }

    const thisItemIdPath = [...parentIdPath, item[idProp]];
    const thisItemNamePath = [...parentNamePath, item.name];
    return [
      {
        ...item,
        idPath: thisItemIdPath,
        namePath: thisItemNamePath,
        __original: item,
      },
      ...(Array.isArray(item.children) && item.children.length
        ? flattenTreeData(
            item.children,
            idProp,
            thisItemIdPath,
            thisItemNamePath,
          )
        : []),
    ];
  });
}

export const TreeView = forwardRef(function TreeView<
  IdProp extends string = 'id',
>(props: TreeRootProps<IdProp>, ref: React.ForwardedRef<HTMLUListElement>) {
  const {
    className,
    containerClassName,

    data,
    value,
    idProp = 'id',
    disabled = false,

    showSelectAll = false,
    defaultNodeIcon = LucideFolder,
    defaultNodeOpenIcon = LucideFolderOpen,
    defaultLeafIcon = LucideFile,

    multiple: multipleSelect = false,
    onSelectChange,

    isLeafNode = _defaultIsLeafFn,

    ...restProps
  } = props;

  const { t } = useTranslation();

  const flatData = useMemo(() => flattenTreeData(data, idProp), [data, idProp]);

  const isControlled = value != null;
  const [__internalSelectionIds, setSelectionIds] = useState<string[]>([]);
  const selectionIds = useMemo(
    () =>
      isControlled
        ? Array.isArray(value)
          ? value
          : value
            ? [value]
            : []
        : __internalSelectionIds,
    [isControlled, value, __internalSelectionIds],
  );

  const getItemId = useCallback(
    (item: TreeDataItem<IdProp>): string => item[idProp] || item.id,
    [idProp],
  );

  const isItemSelected = useCallback(
    (item: TreeDataItem<IdProp>): CheckedState => {
      const thisItemId = getItemId(item);

      // No valid item ID, always return `false`
      if (!thisItemId) {
        return false;
      }

      if (multipleSelect) {
        // Guaranteed `getItemId(item) === thisItemId`
        const _item = flatData.find((it) => getItemId(it) === thisItemId)!;

        if (isLeafNode(_item)) {
          return selectionIds.includes(thisItemId);
        } else {
          // Calculate the state by all leaf children
          const leafChildrenIds = flatData
            .filter(
              (d) =>
                isLeafNode(d) &&
                getItemId(d) !== thisItemId &&
                d.idPath.includes(thisItemId),
            )
            .map(getItemId);
          const isAllLeafChildrenSelected = leafChildrenIds.every((id) =>
            selectionIds.includes(id),
          );
          const isAnyLeafChildSelected = leafChildrenIds.some((id) =>
            selectionIds.includes(id),
          );

          return isAllLeafChildrenSelected
            ? true
            : isAnyLeafChildSelected
              ? 'indeterminate'
              : false;
        }
      } else {
        return selectionIds[0] === thisItemId;
      }
    },
    [flatData, selectionIds, multipleSelect, getItemId, isLeafNode],
  );

  const handleSelection = useCallback(
    (item: TreeDataItem<IdProp>) => {
      if (disabled) return;

      const thisItemId = getItemId(item);

      // 'indeterminate' is not considered as selected
      const isSelected = isItemSelected(item) === true;
      let nextValue = selectionIds;

      if (multipleSelect) {
        const isLeaf = isLeafNode(item);
        const idsToMutate = isLeaf
          ? [thisItemId]
          : flatData
              .filter(
                (d) =>
                  getItemId(d) !== thisItemId && d.idPath.includes(thisItemId),
              )
              .map(getItemId);

        nextValue = isSelected
          ? selectionIds.filter((id) => !idsToMutate.includes(id))
          : [...new Set([...selectionIds, ...idsToMutate])];
      } else {
        // Single select: toggle the item
        nextValue = isSelected ? [] : [thisItemId];
      }

      setSelectionIds(nextValue);
      // @ts-ignore
      onSelectChange?.(multipleSelect ? nextValue : nextValue[0]);
    },
    [
      selectionIds,
      flatData,
      multipleSelect,
      isItemSelected,
      onSelectChange,
      disabled,
      getItemId,
      isLeafNode,
    ],
  );

  const getProcessedItem = useCallback(
    (item: TreeDataItem<IdProp>) => {
      const thisItemId = getItemId(item);
      return flatData.find((d) => getItemId(d) === thisItemId)!;
    },
    [flatData, getItemId],
  );

  const isAllSelected = useMemo(() => {
    return (
      !!flatData.length &&
      flatData
        .filter(isLeafNode)
        .every((d) => selectionIds.includes(getItemId(d)))
    );
  }, [flatData, selectionIds, getItemId, isLeafNode]);

  const handleToggleSelectAll = useCallback(() => {
    if (disabled) return;

    const allLeafIds = flatData.filter(isLeafNode).map(getItemId);
    setSelectionIds(isAllSelected ? [] : allLeafIds);
    onSelectChange?.(
      // @ts-ignore
      multipleSelect
        ? isAllSelected
          ? []
          : allLeafIds
        : isAllSelected
          ? ''
          : allLeafIds[0],
    );
  }, [
    flatData,
    isAllSelected,
    multipleSelect,
    onSelectChange,
    getItemId,
    disabled,
    isLeafNode,
  ]);

  useLayoutEffect(() => {
    if (isControlled) {
      setSelectionIds(Array.isArray(value) ? value : value ? [value] : []);
    }
  }, [isControlled, value]);

  return (
    <TreeUtilityContext.Provider
      value={{
        getItemId,
        isLeafNode,
      }}
    >
      <TreeSelectionContext.Provider
        value={{
          multiple: multipleSelect,
          isItemSelected,
          getProcessedItem,
          onSelect: handleSelection,
        }}
      >
        <TreeDefaultIconContext.Provider
          value={{
            node: defaultNodeIcon,
            nodeOpen: defaultNodeOpenIcon,
            leaf: defaultLeafIcon,
          }}
        >
          <div className={cn('size-full', containerClassName, 'flex flex-col')}>
            {showSelectAll ? (
              <div className="flex-none p-2 border-b border-border-default">
                {showSelectAll && (
                  <div className="flex items-center">
                    <label className="flex items-center">
                      <Checkbox
                        className="ml-2 mr-2"
                        checked={isAllSelected}
                        onCheckedChange={() => handleToggleSelectAll()}
                      />

                      <span>{t('common.selectAll')}</span>
                    </label>
                  </div>
                )}
              </div>
            ) : null}

            <div className={cn('overflow-auto relative p-2 flex-auto')}>
              <TreeViewInner
                ref={ref}
                data={data}
                className={className}
                multiple={multipleSelect}
                {...restProps}
              />
            </div>
          </div>
        </TreeDefaultIconContext.Provider>
      </TreeSelectionContext.Provider>
    </TreeUtilityContext.Provider>
  );
}) as <IdProp extends string = 'id'>(
  props: TreeRootProps<IdProp> & { ref?: React.Ref<HTMLUListElement> },
) => React.ReactElement;
// Return type of the `forwardRef()` is a plain function, so we need to use type assertion ("cast") it to the generic type
