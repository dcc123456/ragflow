type DirectionHorizontal = 'left' | 'right';
type DirectionVertical = 'top' | 'bottom';
type Direction =
  | DirectionHorizontal
  | DirectionVertical
  | `${DirectionHorizontal} ${DirectionVertical}`
  | `${DirectionVertical} ${DirectionHorizontal}`;

const DIRECTIONS: Direction[] = [
  'left',
  'right',
  'top',
  'bottom',
  'left top',
  'left bottom',
  'right top',
  'right bottom',
  'top left',
  'top right',
  'bottom left',
  'bottom right',
];

const theme = {
  primary: '0 190 180',
  secondary: '117 120 122',
  success: '59 160 92',
  info: '92 150 200',
  warning: '250 173 20',
  danger: '216 73 75',
  black: '0 0 0',
  white: '255 255 255',
};

const PRESET_GRADIENT_MAP = {
  primary: [`rgb(${theme.primary})`, null, '#42ffa4'],
  'primary info': [`rgb(${theme.primary})`, null, `rgb(${theme.info})`],
  text: [`${theme.black})`, null, '#9dbab8'],
} as const;

type Props = React.HTMLAttributes<HTMLSpanElement> & {
  /** Starting color */
  from?: string;

  /** Via color  */
  via?: string;

  /** Ending color */
  to?: string;

  /**
   * Direction of the gradient, can be a string represents CSS `<side-or-corner>` value or a number for degrees
   *
   * @default 'bottom'
   */
  direction?: Direction | number;

  /**
   * Use a predefined gradient, could be partially overridden by `from`, `via`, `to` props
   *
   * @default 'primary'
   */
  preset?: keyof typeof PRESET_GRADIENT_MAP;
};

function FxGradientText(props: React.PropsWithChildren<Props>) {
  const {
    children,
    from: _from,
    via: _via,
    to: _to,
    direction: _direction = 'bottom',
    preset = 'primary',
    style,
    ...restProps
  } = props;

  const direction =
    typeof _direction === 'number'
      ? `${_direction}deg`
      : DIRECTIONS.includes(_direction as Direction)
        ? `to ${_direction}`
        : _direction;

  const [from, via, to] = PRESET_GRADIENT_MAP[preset] ?? [];

  const stops = [
    _from ?? from,
    _via ?? via,
    _to ?? to,
    _via ?? via,
    _to ?? to,
  ].filter(Boolean);

  return (
    <span
      {...restProps}
      style={{
        ...(style ?? {}),
        color: 'transparent',
        backgroundClip: 'text',
        backgroundSize: '200% 200%',
        backgroundImage: `linear-gradient(${direction}, ${stops.join(', ')})`,
      }}
    >
      {children}
    </span>
  );
}

export default FxGradientText;
