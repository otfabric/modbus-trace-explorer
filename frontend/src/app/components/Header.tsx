import logoDark from '../../assets/logo-dark.svg';
import logoLight from '../../assets/logo-light.svg';
import modbusIcon from '../../assets/modbus.svg';
import { MoonIcon, SunIcon } from '../icons';
import type { Theme } from '../platform/platform';
import { IconButton } from './ui';

interface HeaderProps {
  theme: Theme;
  onToggleTheme: () => void;
}

/** BRANDING.md section 4: protocol icon, product name, "powered by OT Fabric". No mode badge: there is one mode. */
export function Header({ theme, onToggleTheme }: HeaderProps) {
  return (
    <header className="flex h-14 shrink-0 items-center gap-4 border-b border-line bg-header px-4">
      <img src={modbusIcon} alt="" width={32} height={32} />
      <span className="font-display text-[1.45rem] font-semibold tracking-[0.01em] text-fg-strong">Modbus Trace Explorer</span>
      <span className="flex items-center gap-1.5 text-[12px] text-muted">
        powered by
        <img src={theme === 'dark' ? logoDark : logoLight} alt="" width={16} height={16} />
        <span className="font-display text-[14px] font-medium tracking-[0.01em] text-fg">
          <b className="font-bold">OT</b> Fabric
        </span>
      </span>
      <span className="ml-auto flex items-center gap-3">
        <IconButton label={theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'} onClick={onToggleTheme}>
          {theme === 'dark' ? <SunIcon /> : <MoonIcon />}
        </IconButton>
      </span>
    </header>
  );
}
