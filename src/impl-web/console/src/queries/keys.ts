/** Query key factory — ensures consistent cache key structure */
export const queryKeys = {
  fleet: {
    all: ['fleet'] as const,
    summary: () => [...queryKeys.fleet.all, 'summary'] as const,
    topology: () => [...queryKeys.fleet.all, 'topology'] as const,
  },
  dpu: {
    all: ['dpu'] as const,
    detail: (dpuId: string) => [...queryKeys.dpu.all, 'detail', dpuId] as const,
    health: () => [...queryKeys.dpu.all, 'health'] as const,
  },
  vnet: {
    all: ['vnet'] as const,
    list: (ns?: string) => [...queryKeys.vnet.all, 'list', ns ?? 'default'] as const,
    detail: (name: string) => [...queryKeys.vnet.all, 'detail', name] as const,
    canvas: (name: string) => [...queryKeys.vnet.all, 'canvas', name] as const,
  },
  eni: {
    all: ['eni'] as const,
    list: (ns?: string) => [...queryKeys.eni.all, 'list', ns ?? 'default'] as const,
  },
  policy: {
    all: ['policy'] as const,
    acl: (ns?: string) => [...queryKeys.policy.all, 'acl', ns ?? 'default'] as const,
    route: (ns?: string) => [...queryKeys.policy.all, 'route', ns ?? 'default'] as const,
  },
  mapping: {
    all: ['mapping'] as const,
    list: (ns?: string) => [...queryKeys.mapping.all, 'list', ns ?? 'default'] as const,
  },
  tunnel: {
    all: ['tunnel'] as const,
    list: (ns?: string) => [...queryKeys.tunnel.all, 'list', ns ?? 'default'] as const,
  },
  ha: {
    all: ['ha'] as const,
    list: (ns?: string) => [...queryKeys.ha.all, 'list', ns ?? 'default'] as const,
  },
  health: {
    all: ['health'] as const,
    dashd: () => [...queryKeys.health.all, 'dashd'] as const,
    leader: () => [...queryKeys.health.all, 'leader'] as const,
  },
  capacity: {
    all: ['capacity'] as const,
    stats: () => [...queryKeys.capacity.all, 'stats'] as const,
  },
  inventory: {
    all: ['inventory'] as const,
    list: () => [...queryKeys.inventory.all, 'list'] as const,
  },
  drift: {
    all: ['drift'] as const,
    list: (dpuId?: string) => [...queryKeys.drift.all, 'list', dpuId] as const,
  },
  audit: {
    all: ['audit'] as const,
    list: () => [...queryKeys.audit.all, 'list'] as const,
  },
  eniPlacement: {
    all: ['eni-placement'] as const,
    list: () => [...queryKeys.eniPlacement.all, 'list'] as const,
  },
};