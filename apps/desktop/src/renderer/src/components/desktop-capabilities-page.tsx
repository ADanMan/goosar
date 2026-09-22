import { CapabilitiesPage } from '@goosar/views/capabilities';
import { useWorkToolsExtras } from '../platform/use-launcher-extras';

export function DesktopCapabilitiesPage() {
  return <CapabilitiesPage workToolsExtras={useWorkToolsExtras()} />;
}
