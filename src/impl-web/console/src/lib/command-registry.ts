/**
 * Command Registry — metadata for all dashctl commands.
 *
 * Used by the Command View to provide an interactive command builder.
 * Each entry describes a dashctl verb+kind combination with its flags,
 * types, examples, and category.
 */

/* ── Types ─────────────────────────────────────────────────── */
export type CommandCategory = 'resources' | 'operations' | 'diagnostics' | 'admin';

export interface CommandFlag {
  /** Flag name (without --) */
  name: string;
  /** Short alias (without -) */
  short?: string;
  /** Human-readable description */
  description: string;
  /** Flag value type */
  type: 'string' | 'number' | 'boolean' | 'string[]';
  /** Whether this flag is required */
  required?: boolean;
  /** Default value (if any) */
  default?: string | number | boolean;
  /** Example values for UI hints */
  examples?: string[];
}

export interface CommandEntry {
  /** Unique key: "verb-kind" (e.g. "get-vnets") */
  id: string;
  /** HTTP verb (for API mapping) */
  verb: 'GET' | 'PUT' | 'DELETE' | 'POST';
  /** dashctl verb */
  action: 'get' | 'put' | 'delete' | 'apply' | 'reconcile' | 'simulate' | 'drain' | 'cordon' | 'uncordon';
  /** Resource kind */
  kind: string;
  /** Human-readable title */
  title: string;
  /** Short description */
  description: string;
  /** Category for grouping in sidebar */
  category: CommandCategory;
  /** API path template (with placeholders) */
  apiPath: string;
  /** Available flags */
  flags: CommandFlag[];
  /** Example CLI invocation */
  examples: string[];
  /** Whether this is a write operation */
  mutating: boolean;
  /** Whether body is required */
  requiresBody?: boolean;
}

/* ── Flag templates ────────────────────────────────────────── */
const namespaceFlag: CommandFlag = {
  name: 'namespace',
  short: 'n',
  description: 'Target namespace',
  type: 'string',
  default: 'default',
  examples: ['default', 'prod', 'staging'],
};

const outputFlag: CommandFlag = {
  name: 'output',
  short: 'o',
  description: 'Output format',
  type: 'string',
  default: 'table',
  examples: ['table', 'json', 'yaml', 'wide'],
};

const nameFlag: CommandFlag = {
  name: 'name',
  description: 'Resource name',
  type: 'string',
  required: true,
  examples: ['my-vnet', 'eni-01', 'acl-prod'],
};

const fileFlag: CommandFlag = {
  name: 'file',
  short: 'f',
  description: 'YAML/JSON file path or stdin',
  type: 'string',
  required: true,
  examples: ['vnet.yaml', 'eni.json', '-'],
};

const dpuIdFlag: CommandFlag = {
  name: 'dpu-id',
  description: 'DPU identifier',
  type: 'string',
  required: true,
  examples: ['dpu-sim-01', 'dpu-sim-05'],
};

/* ── Registry ──────────────────────────────────────────────── */
export const COMMAND_REGISTRY: CommandEntry[] = [
  // ── Resources: GET ──────────────────────────────────────
  {
    id: 'get-vnets',
    verb: 'GET',
    action: 'get',
    kind: 'vnets',
    title: 'List Vnets',
    description: 'List all virtual networks in a namespace',
    category: 'resources',
    apiPath: '/api/v1/{namespace}/vnets',
    flags: [namespaceFlag, outputFlag],
    examples: ['dashctl get vnets', 'dashctl get vnets -n prod -o json'],
    mutating: false,
  },
  {
    id: 'get-vnet',
    verb: 'GET',
    action: 'get',
    kind: 'vnet',
    title: 'Get Vnet',
    description: 'Get a specific virtual network by name',
    category: 'resources',
    apiPath: '/api/v1/{namespace}/vnets/{name}',
    flags: [namespaceFlag, nameFlag, outputFlag],
    examples: ['dashctl get vnet my-vnet', 'dashctl get vnet my-vnet -o yaml'],
    mutating: false,
  },
  {
    id: 'get-enis',
    verb: 'GET',
    action: 'get',
    kind: 'enis',
    title: 'List ENIs',
    description: 'List all elastic network interfaces in a namespace',
    category: 'resources',
    apiPath: '/api/v1/{namespace}/enis',
    flags: [namespaceFlag, outputFlag],
    examples: ['dashctl get enis', 'dashctl get enis -o wide'],
    mutating: false,
  },
  {
    id: 'get-eni',
    verb: 'GET',
    action: 'get',
    kind: 'eni',
    title: 'Get ENI',
    description: 'Get a specific ENI by name',
    category: 'resources',
    apiPath: '/api/v1/{namespace}/enis/{name}',
    flags: [namespaceFlag, nameFlag, outputFlag],
    examples: ['dashctl get eni eni-01'],
    mutating: false,
  },
  {
    id: 'get-acl-policies',
    verb: 'GET',
    action: 'get',
    kind: 'acl-policies',
    title: 'List ACL Policies',
    description: 'List all ACL policies in a namespace',
    category: 'resources',
    apiPath: '/api/v1/{namespace}/acl-policies',
    flags: [namespaceFlag, outputFlag],
    examples: ['dashctl get acl-policies'],
    mutating: false,
  },
  {
    id: 'get-route-policies',
    verb: 'GET',
    action: 'get',
    kind: 'route-policies',
    title: 'List Route Policies',
    description: 'List all route policies in a namespace',
    category: 'resources',
    apiPath: '/api/v1/{namespace}/route-policies',
    flags: [namespaceFlag, outputFlag],
    examples: ['dashctl get route-policies'],
    mutating: false,
  },
  {
    id: 'get-vnet-mappings',
    verb: 'GET',
    action: 'get',
    kind: 'vnet-mappings',
    title: 'List Vnet Mappings',
    description: 'List all Vnet-to-Vnet mappings in a namespace',
    category: 'resources',
    apiPath: '/api/v1/{namespace}/vnet-mappings',
    flags: [namespaceFlag, outputFlag],
    examples: ['dashctl get vnet-mappings'],
    mutating: false,
  },
  {
    id: 'get-service-tunnels',
    verb: 'GET',
    action: 'get',
    kind: 'service-tunnels',
    title: 'List Service Tunnels',
    description: 'List all service tunnels in a namespace',
    category: 'resources',
    apiPath: '/api/v1/{namespace}/service-tunnels',
    flags: [namespaceFlag, outputFlag],
    examples: ['dashctl get service-tunnels'],
    mutating: false,
  },
  {
    id: 'get-ha-sets',
    verb: 'GET',
    action: 'get',
    kind: 'ha-sets',
    title: 'List HA Sets',
    description: 'List all HA sets in a namespace',
    category: 'resources',
    apiPath: '/api/v1/{namespace}/ha',
    flags: [namespaceFlag, outputFlag],
    examples: ['dashctl get ha-sets'],
    mutating: false,
  },

  // ── Resources: PUT (create/update) ──────────────────────
  {
    id: 'put-vnet',
    verb: 'PUT',
    action: 'apply',
    kind: 'vnet',
    title: 'Apply Vnet',
    description: 'Create or update a virtual network from file',
    category: 'resources',
    apiPath: '/api/v1/{namespace}/vnets/{name}',
    flags: [namespaceFlag, fileFlag],
    examples: ['dashctl apply -f vnet.yaml', 'cat vnet.json | dashctl apply -f -'],
    mutating: true,
    requiresBody: true,
  },
  {
    id: 'put-eni',
    verb: 'PUT',
    action: 'apply',
    kind: 'eni',
    title: 'Apply ENI',
    description: 'Create or update an ENI from file',
    category: 'resources',
    apiPath: '/api/v1/{namespace}/enis/{name}',
    flags: [namespaceFlag, fileFlag],
    examples: ['dashctl apply -f eni.yaml'],
    mutating: true,
    requiresBody: true,
  },
  {
    id: 'put-acl-policy',
    verb: 'PUT',
    action: 'apply',
    kind: 'acl-policy',
    title: 'Apply ACL Policy',
    description: 'Create or update an ACL policy from file',
    category: 'resources',
    apiPath: '/api/v1/{namespace}/acl-policies/{name}',
    flags: [namespaceFlag, fileFlag],
    examples: ['dashctl apply -f acl-policy.yaml'],
    mutating: true,
    requiresBody: true,
  },
  {
    id: 'put-route-policy',
    verb: 'PUT',
    action: 'apply',
    kind: 'route-policy',
    title: 'Apply Route Policy',
    description: 'Create or update a route policy from file',
    category: 'resources',
    apiPath: '/api/v1/{namespace}/route-policies/{name}',
    flags: [namespaceFlag, fileFlag],
    examples: ['dashctl apply -f route-policy.yaml'],
    mutating: true,
    requiresBody: true,
  },
  {
    id: 'put-service-tunnel',
    verb: 'PUT',
    action: 'apply',
    kind: 'service-tunnel',
    title: 'Apply Service Tunnel',
    description: 'Create or update a service tunnel from file',
    category: 'resources',
    apiPath: '/api/v1/{namespace}/service-tunnels/{name}',
    flags: [namespaceFlag, fileFlag],
    examples: ['dashctl apply -f tunnel.yaml'],
    mutating: true,
    requiresBody: true,
  },
  {
    id: 'put-ha-set',
    verb: 'PUT',
    action: 'apply',
    kind: 'ha-set',
    title: 'Apply HA Set',
    description: 'Create or update an HA set from file',
    category: 'resources',
    apiPath: '/api/v1/{namespace}/ha/{name}',
    flags: [namespaceFlag, fileFlag],
    examples: ['dashctl apply -f ha-set.yaml'],
    mutating: true,
    requiresBody: true,
  },

  // ── Resources: DELETE ───────────────────────────────────
  {
    id: 'delete-vnet',
    verb: 'DELETE',
    action: 'delete',
    kind: 'vnet',
    title: 'Delete Vnet',
    description: 'Delete a virtual network by name',
    category: 'resources',
    apiPath: '/api/v1/{namespace}/vnets/{name}',
    flags: [namespaceFlag, nameFlag],
    examples: ['dashctl delete vnet my-vnet'],
    mutating: true,
  },
  {
    id: 'delete-eni',
    verb: 'DELETE',
    action: 'delete',
    kind: 'eni',
    title: 'Delete ENI',
    description: 'Delete an ENI by name',
    category: 'resources',
    apiPath: '/api/v1/{namespace}/enis/{name}',
    flags: [namespaceFlag, nameFlag],
    examples: ['dashctl delete eni eni-01'],
    mutating: true,
  },
  {
    id: 'delete-acl-policy',
    verb: 'DELETE',
    action: 'delete',
    kind: 'acl-policy',
    title: 'Delete ACL Policy',
    description: 'Delete an ACL policy by name',
    category: 'resources',
    apiPath: '/api/v1/{namespace}/acl-policies/{name}',
    flags: [namespaceFlag, nameFlag],
    examples: ['dashctl delete acl-policy acl-prod'],
    mutating: true,
  },

  // ── Operations ──────────────────────────────────────────
  {
    id: 'reconcile',
    verb: 'POST',
    action: 'reconcile',
    kind: 'cluster',
    title: 'Reconcile',
    description: 'Trigger full reconciliation of declared → DPU state',
    category: 'operations',
    apiPath: '/api/v1/reconcile',
    flags: [
      {
        name: 'dpu-ids',
        description: 'Limit reconciliation to specific DPUs (comma-separated)',
        type: 'string[]',
        examples: ['dpu-sim-01,dpu-sim-02'],
      },
    ],
    examples: ['dashctl reconcile', 'dashctl reconcile --dpu-ids dpu-sim-01'],
    mutating: true,
  },
  {
    id: 'drain',
    verb: 'POST',
    action: 'drain',
    kind: 'dpu',
    title: 'Drain DPU',
    description: 'Drain all ENIs from a DPU (evacuate workloads)',
    category: 'operations',
    apiPath: '/api/v1/operations/drain/{dpu-id}',
    flags: [dpuIdFlag],
    examples: ['dashctl drain dpu-sim-03'],
    mutating: true,
  },
  {
    id: 'cordon',
    verb: 'POST',
    action: 'cordon',
    kind: 'dpu',
    title: 'Cordon DPU',
    description: 'Mark a DPU as unschedulable (no new ENI placements)',
    category: 'operations',
    apiPath: '/api/v1/operations/cordon/{dpu-id}',
    flags: [dpuIdFlag],
    examples: ['dashctl cordon dpu-sim-03'],
    mutating: true,
  },
  {
    id: 'uncordon',
    verb: 'POST',
    action: 'uncordon',
    kind: 'dpu',
    title: 'Uncordon DPU',
    description: 'Mark a DPU as schedulable again',
    category: 'operations',
    apiPath: '/api/v1/operations/uncordon/{dpu-id}',
    flags: [dpuIdFlag],
    examples: ['dashctl uncordon dpu-sim-03'],
    mutating: true,
  },

  // ── Diagnostics ─────────────────────────────────────────
  {
    id: 'simulate-flow',
    verb: 'POST',
    action: 'simulate',
    kind: 'flow',
    title: 'Simulate Flow',
    description: 'Simulate packet flow through the pipeline and see matched rules',
    category: 'diagnostics',
    apiPath: '/api/v1/simulate',
    flags: [
      { name: 'src-ip', description: 'Source IP address', type: 'string', required: true, examples: ['10.0.1.10'] },
      { name: 'dst-ip', description: 'Destination IP address', type: 'string', required: true, examples: ['10.0.2.20'] },
      { name: 'protocol', description: 'Protocol (TCP/UDP/ICMP)', type: 'string', required: true, examples: ['TCP', 'UDP', 'ICMP'] },
      { name: 'src-port', description: 'Source port', type: 'number', examples: ['12345'] },
      { name: 'dst-port', description: 'Destination port', type: 'number', examples: ['443', '80'] },
      { name: 'eni-name', description: 'Target ENI', type: 'string', required: true, examples: ['eni-01'] },
    ],
    examples: [
      'dashctl simulate flow --src-ip 10.0.1.10 --dst-ip 10.0.2.20 --protocol TCP --dst-port 443 --eni-name eni-01',
    ],
    mutating: false,
    requiresBody: true,
  },

  // ── Admin ───────────────────────────────────────────────
  {
    id: 'admin-health',
    verb: 'GET',
    action: 'get',
    kind: 'health',
    title: 'dashd Health',
    description: 'Check dashd controller health status',
    category: 'admin',
    apiPath: '/api/admin/health',
    flags: [outputFlag],
    examples: ['dashctl admin health'],
    mutating: false,
  },
  {
    id: 'admin-leader',
    verb: 'GET',
    action: 'get',
    kind: 'leader',
    title: 'Leader Status',
    description: 'Check which dashd instance is the current leader',
    category: 'admin',
    apiPath: '/api/admin/leader',
    flags: [outputFlag],
    examples: ['dashctl admin leader'],
    mutating: false,
  },
  {
    id: 'admin-inventory',
    verb: 'GET',
    action: 'get',
    kind: 'inventory',
    title: 'DPU Inventory',
    description: 'List all DPUs in the inventory with connection state',
    category: 'admin',
    apiPath: '/api/admin/inventory',
    flags: [outputFlag],
    examples: ['dashctl admin inventory'],
    mutating: false,
  },
  {
    id: 'admin-drift',
    verb: 'GET',
    action: 'get',
    kind: 'drift',
    title: 'Drift Report',
    description: 'Show configuration drift between declared and observed state',
    category: 'admin',
    apiPath: '/api/admin/drift',
    flags: [
      { name: 'dpu', description: 'Filter by DPU ID', type: 'string', examples: ['dpu-sim-01'] },
      outputFlag,
    ],
    examples: ['dashctl admin drift', 'dashctl admin drift --dpu dpu-sim-01'],
    mutating: false,
  },
  {
    id: 'admin-eni-placement',
    verb: 'GET',
    action: 'get',
    kind: 'eni-placement',
    title: 'ENI Placement',
    description: 'Show current ENI-to-DPU placement map',
    category: 'admin',
    apiPath: '/api/admin/eni-placement',
    flags: [outputFlag],
    examples: ['dashctl admin eni-placement'],
    mutating: false,
  },
  {
    id: 'admin-observed',
    verb: 'GET',
    action: 'get',
    kind: 'observed',
    title: 'Observed State',
    description: 'Dump the observed (DPU-reported) state for a DPU',
    category: 'admin',
    apiPath: '/api/admin/observed',
    flags: [
      { name: 'dpu', description: 'DPU ID', type: 'string', required: true, examples: ['dpu-sim-01'] },
      outputFlag,
    ],
    examples: ['dashctl admin observed --dpu dpu-sim-01'],
    mutating: false,
  },
  {
    id: 'admin-audit',
    verb: 'GET',
    action: 'get',
    kind: 'audit',
    title: 'Audit Log',
    description: 'View the recent audit log entries',
    category: 'admin',
    apiPath: '/api/admin/audit',
    flags: [
      { name: 'limit', description: 'Number of entries to return', type: 'number', default: 100, examples: ['50', '200'] },
      outputFlag,
    ],
    examples: ['dashctl admin audit', 'dashctl admin audit --limit 50'],
    mutating: false,
  },
];

/* ── Helpers ───────────────────────────────────────────────── */

/** Group commands by category */
export function getCommandsByCategory(): Record<CommandCategory, CommandEntry[]> {
  const grouped: Record<CommandCategory, CommandEntry[]> = {
    resources: [],
    operations: [],
    diagnostics: [],
    admin: [],
  };
  for (const cmd of COMMAND_REGISTRY) {
    grouped[cmd.category].push(cmd);
  }
  return grouped;
}

/** Find a command by its ID */
export function getCommandById(id: string): CommandEntry | undefined {
  return COMMAND_REGISTRY.find((c) => c.id === id);
}

/** Build an API URL from a command and flag values */
export function buildApiUrl(
  command: CommandEntry,
  flagValues: Record<string, string | number | boolean | string[]>,
): string {
  let path = command.apiPath;

  // Replace placeholders
  const namespace = (flagValues['namespace'] as string) || 'default';
  path = path.replace('{namespace}', namespace);

  if (flagValues['name']) {
    path = path.replace('{name}', String(flagValues['name']));
  }
  if (flagValues['dpu-id']) {
    path = path.replace('{dpu-id}', String(flagValues['dpu-id']));
  }

  // Add query parameters for non-path flags
  const queryParams: string[] = [];
  for (const [key, value] of Object.entries(flagValues)) {
    if (['namespace', 'name', 'dpu-id', 'file', 'output'].includes(key)) continue;
    if (value === undefined || value === '' || value === false) continue;
    if (Array.isArray(value)) {
      queryParams.push(`${key}=${value.join(',')}`);
    } else {
      queryParams.push(`${key}=${encodeURIComponent(String(value))}`);
    }
  }

  if (queryParams.length > 0) {
    path += '?' + queryParams.join('&');
  }

  return path;
}

/** Category labels for UI */
export const CATEGORY_LABELS: Record<CommandCategory, string> = {
  resources: 'Resources',
  operations: 'Operations',
  diagnostics: 'Diagnostics',
  admin: 'Admin',
};

/** Category icons (lucide icon names) */
export const CATEGORY_ICONS: Record<CommandCategory, string> = {
  resources: 'Database',
  operations: 'Wrench',
  diagnostics: 'Search',
  admin: 'Shield',
};