import {
  AlertCircle,
  Archive,
  CircleDot,
  CircleSlash,
  Clock,
  Loader2,
  PlugZap,
  type LucideIcon,
} from 'lucide-react';
import type { AgentAvailability, Workload } from '@goosar/core/agents';

export interface AvailabilityVisual {
  label: string;
  dotClass: string;
  textClass: string;
  icon: LucideIcon;
}

export const availabilityConfig: Record<AgentAvailability, AvailabilityVisual> = {
  online: {
    label: 'Online',
    dotClass: 'bg-success',
    textClass: 'text-success',
    icon: CircleDot,
  },
  unstable: {
    label: 'Unstable',
    dotClass: 'bg-warning',
    textClass: 'text-warning',
    icon: PlugZap,
  },
  offline: {
    label: 'Offline',
    dotClass: 'bg-muted-foreground/40',
    textClass: 'text-muted-foreground',
    icon: CircleSlash,
  },
  archived: {
    label: 'Archived',
    dotClass: 'bg-muted-foreground/40',
    textClass: 'text-muted-foreground',
    icon: Archive,
  },
};

export const availabilityOrder: AgentAvailability[] = ['online', 'unstable', 'offline'];

export interface WorkloadVisual {
  label: string;
  textClass: string;
  icon: LucideIcon;
}

export const workloadConfig: Record<Workload, WorkloadVisual> = {
  working: {
    label: 'Working',
    textClass: 'text-brand',
    icon: Loader2,
  },
  queued: {
    label: 'Queued',
    textClass: 'text-warning',
    icon: Clock,
  },
  idle: {
    label: 'Idle',
    textClass: 'text-muted-foreground',
    icon: AlertCircle,
  },
};

export const workloadOrder: Workload[] = ['working', 'queued', 'idle'];
