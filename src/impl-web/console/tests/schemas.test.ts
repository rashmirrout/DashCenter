import { describe, it, expect } from 'vitest';
import {
  vnetSchema, eniSchema, aclPolicySchema, routePolicySchema,
  serviceTunnelSchema, haSetSchema, simulateRequestSchema, RESOURCE_SCHEMAS,
} from '../src/lib/schemas';

describe('vnetSchema', () => {
  const valid = {
    metadata: { namespace: 'default', name: 'vnet-prod' },
    vni: 1000,
    address_space: ['10.0.0.0/16'],
  };
  it('accepts valid vnet', () => {
    expect(vnetSchema.safeParse(valid).success).toBe(true);
  });
  it('rejects missing vni', () => {
    expect(vnetSchema.safeParse({ ...valid, vni: undefined }).success).toBe(false);
  });
  it('rejects vni out of range', () => {
    expect(vnetSchema.safeParse({ ...valid, vni: 0 }).success).toBe(false);
    expect(vnetSchema.safeParse({ ...valid, vni: 16777216 }).success).toBe(false);
  });
  it('rejects invalid CIDR', () => {
    expect(vnetSchema.safeParse({ ...valid, address_space: ['not-cidr'] }).success).toBe(false);
  });
  it('rejects empty address_space', () => {
    expect(vnetSchema.safeParse({ ...valid, address_space: [] }).success).toBe(false);
  });
});

describe('eniSchema', () => {
  const valid = {
    metadata: { namespace: 'default', name: 'eni-1' },
    vnet_name: 'vnet-prod',
    mac_address: 'aa:bb:cc:dd:ee:ff',
    underlay_ip: '10.0.0.1',
  };
  it('accepts valid eni', () => {
    expect(eniSchema.safeParse(valid).success).toBe(true);
  });
  it('rejects invalid mac', () => {
    expect(eniSchema.safeParse({ ...valid, mac_address: 'bad' }).success).toBe(false);
  });
  it('rejects invalid ip', () => {
    expect(eniSchema.safeParse({ ...valid, underlay_ip: 'not-ip' }).success).toBe(false);
  });
});

describe('aclPolicySchema', () => {
  const valid = {
    metadata: { namespace: 'default', name: 'acl-1' },
    eni_names: ['eni-1'],
    default_action: 'DENY' as const,
    rules: [{ priority: 100, action: 'ALLOW' as const, direction: 'IN' as const }],
  };
  it('accepts valid policy', () => {
    expect(aclPolicySchema.safeParse(valid).success).toBe(true);
  });
  it('rejects empty eni_names', () => {
    expect(aclPolicySchema.safeParse({ ...valid, eni_names: [] }).success).toBe(false);
  });
  it('rejects empty rules', () => {
    expect(aclPolicySchema.safeParse({ ...valid, rules: [] }).success).toBe(false);
  });
  it('validates port range', () => {
    const withPort = { ...valid, rules: [{ ...valid.rules[0], src_port_range: { start: 0, end: 65535 } }] };
    expect(aclPolicySchema.safeParse(withPort).success).toBe(true);
    const badPort = { ...valid, rules: [{ ...valid.rules[0], src_port_range: { start: -1, end: 70000 } }] };
    expect(aclPolicySchema.safeParse(badPort).success).toBe(false);
  });
});

describe('routePolicySchema', () => {
  it('accepts valid policy', () => {
    const valid = {
      metadata: { namespace: 'default', name: 'rp-1' },
      eni_names: ['eni-1'],
      direction: 'IN' as const,
      rules: [{ priority: 10, action: 'PERMIT' as const }],
    };
    expect(routePolicySchema.safeParse(valid).success).toBe(true);
  });
});

describe('serviceTunnelSchema', () => {
  it('accepts valid tunnel', () => {
    const valid = {
      metadata: { namespace: 'default', name: 'tun-1' },
      source_vnet: 'vnet-a',
      destination_vnet: 'vnet-b',
      tunnel_type: 'vxlan',
    };
    expect(serviceTunnelSchema.safeParse(valid).success).toBe(true);
  });
});

describe('haSetSchema', () => {
  it('rejects less than 2 members', () => {
    const invalid = {
      metadata: { namespace: 'default', name: 'ha-1' },
      scope: 'eni',
      members: [{ dpu_id: 'dpu-1', role: 'active' }],
    };
    expect(haSetSchema.safeParse(invalid).success).toBe(false);
  });
  it('accepts 2 members', () => {
    const valid = {
      metadata: { namespace: 'default', name: 'ha-1' },
      scope: 'eni',
      members: [
        { dpu_id: 'dpu-1', role: 'active' },
        { dpu_id: 'dpu-2', role: 'standby' },
      ],
    };
    expect(haSetSchema.safeParse(valid).success).toBe(true);
  });
});

describe('simulateRequestSchema', () => {
  it('accepts valid request', () => {
    const valid = {
      vnet_name: 'vnet-prod',
      src_ip: '10.0.0.1',
      dst_ip: '10.0.0.2',
      protocol: 6,
      direction: 'IN' as const,
    };
    expect(simulateRequestSchema.safeParse(valid).success).toBe(true);
  });
  it('rejects invalid IPs', () => {
    const invalid = {
      vnet_name: 'vnet-prod',
      src_ip: 'bad',
      dst_ip: '10.0.0.2',
      protocol: 6,
      direction: 'IN' as const,
    };
    expect(simulateRequestSchema.safeParse(invalid).success).toBe(false);
  });
});

describe('RESOURCE_SCHEMAS registry', () => {
  it('has all 7 kinds', () => {
    expect(Object.keys(RESOURCE_SCHEMAS)).toEqual(
      expect.arrayContaining(['vnets', 'enis', 'acl-policies', 'route-policies', 'vnet-mappings', 'service-tunnels', 'ha']),
    );
  });
});