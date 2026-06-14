import { describe, it, expect } from 'vitest';
import {
  COMMAND_REGISTRY,
  getCommandsByCategory,
  getCommandById,
  buildApiUrl,
  CATEGORY_LABELS,
  type CommandEntry,
} from '../src/lib/command-registry';

describe('COMMAND_REGISTRY', () => {
  it('has entries for all expected categories', () => {
    const categories = new Set(COMMAND_REGISTRY.map((c) => c.category));
    expect(categories).toContain('resources');
    expect(categories).toContain('operations');
    expect(categories).toContain('diagnostics');
    expect(categories).toContain('admin');
  });

  it('every entry has a unique id', () => {
    const ids = COMMAND_REGISTRY.map((c) => c.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  it('every entry has non-empty title, description, and examples', () => {
    for (const cmd of COMMAND_REGISTRY) {
      expect(cmd.title.length).toBeGreaterThan(0);
      expect(cmd.description.length).toBeGreaterThan(0);
      expect(cmd.examples.length).toBeGreaterThan(0);
    }
  });

  it('every entry has a valid HTTP verb', () => {
    const validVerbs = ['GET', 'PUT', 'DELETE', 'POST'];
    for (const cmd of COMMAND_REGISTRY) {
      expect(validVerbs).toContain(cmd.verb);
    }
  });

  it('mutating commands use PUT/POST/DELETE', () => {
    for (const cmd of COMMAND_REGISTRY) {
      if (cmd.mutating) {
        expect(['PUT', 'POST', 'DELETE']).toContain(cmd.verb);
      }
    }
  });

  it('has at least 30 commands', () => {
    expect(COMMAND_REGISTRY.length).toBeGreaterThanOrEqual(30);
  });
});

describe('getCommandsByCategory', () => {
  it('groups all commands', () => {
    const grouped = getCommandsByCategory();
    const totalGrouped = Object.values(grouped).reduce((sum, arr) => sum + arr.length, 0);
    expect(totalGrouped).toBe(COMMAND_REGISTRY.length);
  });

  it('resources category has GET/PUT/DELETE commands', () => {
    const grouped = getCommandsByCategory();
    const verbs = new Set(grouped.resources.map((c) => c.verb));
    expect(verbs).toContain('GET');
    expect(verbs).toContain('PUT');
    expect(verbs).toContain('DELETE');
  });

  it('operations category has POST commands', () => {
    const grouped = getCommandsByCategory();
    expect(grouped.operations.every((c) => c.verb === 'POST')).toBe(true);
  });
});

describe('getCommandById', () => {
  it('finds existing command', () => {
    const cmd = getCommandById('get-vnets');
    expect(cmd).toBeDefined();
    expect(cmd!.title).toBe('List Vnets');
  });

  it('returns undefined for non-existent command', () => {
    expect(getCommandById('non-existent')).toBeUndefined();
  });
});

describe('buildApiUrl', () => {
  it('builds simple GET path with default namespace', () => {
    const cmd = getCommandById('get-vnets')!;
    const url = buildApiUrl(cmd, {});
    expect(url).toBe('/api/v1/default/vnets');
  });

  it('replaces namespace placeholder', () => {
    const cmd = getCommandById('get-vnets')!;
    const url = buildApiUrl(cmd, { namespace: 'prod' });
    expect(url).toBe('/api/v1/prod/vnets');
  });

  it('replaces name placeholder', () => {
    const cmd = getCommandById('get-vnet')!;
    const url = buildApiUrl(cmd, { name: 'my-vnet', namespace: 'default' });
    expect(url).toBe('/api/v1/default/vnets/my-vnet');
  });

  it('replaces dpu-id placeholder', () => {
    const cmd = getCommandById('drain')!;
    const url = buildApiUrl(cmd, { 'dpu-id': 'dpu-sim-03' });
    expect(url).toBe('/api/v1/operations/drain/dpu-sim-03');
  });

  it('adds query parameters for non-path flags', () => {
    const cmd = getCommandById('admin-drift')!;
    const url = buildApiUrl(cmd, { dpu: 'dpu-sim-01' });
    expect(url).toBe('/api/admin/drift?dpu=dpu-sim-01');
  });

  it('handles admin paths without namespace', () => {
    const cmd = getCommandById('admin-health')!;
    const url = buildApiUrl(cmd, {});
    expect(url).toBe('/api/admin/health');
  });

  it('handles array query parameters', () => {
    const cmd = getCommandById('reconcile')!;
    const url = buildApiUrl(cmd, { 'dpu-ids': ['dpu-sim-01', 'dpu-sim-02'] });
    expect(url).toBe('/api/v1/reconcile?dpu-ids=dpu-sim-01,dpu-sim-02');
  });
});

describe('CATEGORY_LABELS', () => {
  it('has labels for all categories', () => {
    expect(Object.keys(CATEGORY_LABELS)).toEqual(['resources', 'operations', 'diagnostics', 'admin']);
  });

  it('labels are non-empty strings', () => {
    for (const label of Object.values(CATEGORY_LABELS)) {
      expect(label.length).toBeGreaterThan(0);
    }
  });
});