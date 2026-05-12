import { describe, it, expect, vi, beforeEach } from 'vitest';
import { OpenLimitAdmin, APIError } from '../src/index';

// Mock global fetch
const mockFetch = vi.fn();
vi.stubGlobal('fetch', mockFetch);

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function makeAdmin(): OpenLimitAdmin {
  return new OpenLimitAdmin({
    baseURL: 'http://localhost:8080',
    adminToken: 'admin-secret-token',
  });
}

beforeEach(() => {
  mockFetch.mockReset();
});

// ── TEST-22-01-01: listProjects() returns Project[] ──────

describe('listProjects', () => {
  it('returns array of Project', async () => {
    const mockProjects = [
      { id: 'proj-1', name: 'My App', created_at: '2026-05-01T00:00:00Z' },
      { id: 'proj-2', name: 'Test Project', created_at: '2026-05-02T00:00:00Z' },
    ];
    mockFetch.mockResolvedValue(jsonResponse(mockProjects));

    const admin = makeAdmin();
    const result = await admin.listProjects();

    expect(result).toHaveLength(2);
    expect(result[0].id).toBe('proj-1');
    expect(result[0].name).toBe('My App');
    expect(mockFetch).toHaveBeenCalledWith(
      'http://localhost:8080/admin/projects',
      expect.objectContaining({ method: 'GET' }),
    );
  });
});

// ── TEST-22-01-02: createProject(name) sends POST, returns Project ──

describe('createProject', () => {
  it('sends POST and returns Project', async () => {
    const mockProject = { id: 'proj-new', name: 'New Project', created_at: '2026-05-09T00:00:00Z' };
    mockFetch.mockResolvedValue(jsonResponse(mockProject, 201));

    const admin = makeAdmin();
    const result = await admin.createProject('New Project');

    expect(result.id).toBe('proj-new');
    expect(result.name).toBe('New Project');
    expect(mockFetch).toHaveBeenCalledWith(
      'http://localhost:8080/admin/projects',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ name: 'New Project' }),
      }),
    );
  });
});

// ── TEST-22-01-03: createKey(req) sends POST, returns CreateKeyResponse ──

describe('createKey', () => {
  it('sends POST with project_id and name, returns CreateKeyResponse', async () => {
    const mockResponse = {
      id: 'key-1',
      key: 'gw-abcdef1234567890abcdef1234567890',
      key_prefix: 'gw-abcde',
      name: 'my-key',
      project_id: 'proj-1',
    };
    mockFetch.mockResolvedValue(jsonResponse(mockResponse, 201));

    const admin = makeAdmin();
    const result = await admin.createKey({
      project_id: 'proj-1',
      name: 'my-key',
    });

    expect(result.id).toBe('key-1');
    expect(result.key).toBe('gw-abcdef1234567890abcdef1234567890');
    expect(result.project_id).toBe('proj-1');
    expect(mockFetch).toHaveBeenCalledWith(
      'http://localhost:8080/admin/keys',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ project_id: 'proj-1', name: 'my-key' }),
      }),
    );
  });
});

// ── TEST-22-01-04: listKeys(project_id?) sends GET with optional filter ──

describe('listKeys', () => {
  it('sends GET and supports optional project_id filter', async () => {
    const mockKeys = [
      {
        id: 'key-1',
        project_id: 'proj-1',
        key_prefix: 'gw-abcde',
        name: 'key-alpha',
        allowed_models: [],
        allowed_providers: [],
        allowed_tools: [],
        rpm_limit: 0,
        tpm_limit: 0,
        budget_limit_usd: 0,
        budget_period: 'monthly',
        expires_at: null,
        revoked_at: null,
        created_at: '2026-05-01T00:00:00Z',
        allow_mcp_server: false,
        mcp_tool_name: '',
      },
    ];
    mockFetch.mockResolvedValue(jsonResponse(mockKeys));

    const admin = makeAdmin();
    const result = await admin.listKeys('proj-1');

    expect(result).toHaveLength(1);
    expect(result[0].id).toBe('key-1');
    expect(mockFetch).toHaveBeenCalledWith(
      'http://localhost:8080/admin/keys?project_id=proj-1',
      expect.objectContaining({ method: 'GET' }),
    );

    // Also test without filter
    mockFetch.mockReset();
    mockFetch.mockResolvedValue(jsonResponse([]));
    await admin.listKeys();
    expect(mockFetch).toHaveBeenCalledWith(
      'http://localhost:8080/admin/keys',
      expect.objectContaining({ method: 'GET' }),
    );
  });
});

// ── TEST-22-01-05: queryUsage(filters) sends GET with query params ──

describe('queryUsage', () => {
  it('sends GET with query params from filters', async () => {
    const mockUsage = [
      {
        id: 1,
        request_id: 'req-1',
        project_id: 'proj-1',
        virtual_key_id: 'key-1',
        model: 'gpt-4',
        provider: 'openai',
        provider_model: 'gpt-4',
        prompt_tokens: 100,
        completion_tokens: 50,
        total_tokens: 150,
        cost_usd: 0.003,
        cache_hit: false,
        stream: false,
        attempts: 1,
        duration_ms: 500,
        error: '',
        created_at: '2026-05-09T00:00:00Z',
      },
    ];
    mockFetch.mockResolvedValue(jsonResponse(mockUsage));

    const admin = makeAdmin();
    const result = await admin.queryUsage({
      project_id: 'proj-1',
      model: 'gpt-4',
      limit: 50,
    });

    expect(result).toHaveLength(1);
    expect(result[0].model).toBe('gpt-4');
    expect(mockFetch).toHaveBeenCalledWith(
      'http://localhost:8080/admin/usage?project_id=proj-1&model=gpt-4&limit=50',
      expect.objectContaining({ method: 'GET' }),
    );
  });
});

// ── TEST-22-01-06: usageSummary(filters) sends GET with period param ──

describe('usageSummary', () => {
  it('sends GET with period param', async () => {
    const mockSummary = [
      {
        period: '2026-05-09T00:00:00Z',
        model: 'gpt-4',
        provider: 'openai',
        request_count: 42,
        prompt_tokens: 10000,
        completion_tokens: 5000,
        total_tokens: 15000,
        cost_usd: 0.35,
      },
    ];
    mockFetch.mockResolvedValue(jsonResponse(mockSummary));

    const admin = makeAdmin();
    const result = await admin.usageSummary({ period: 'daily' });

    expect(result).toHaveLength(1);
    expect(result[0].request_count).toBe(42);
    expect(mockFetch).toHaveBeenCalledWith(
      'http://localhost:8080/admin/usage/summary?period=daily',
      expect.objectContaining({ method: 'GET' }),
    );
  });
});

// ── TEST-22-01-07: quickstart(opts?) sends POST, returns {project, key} ──

describe('quickstart', () => {
  it('sends POST and returns project and key', async () => {
    const mockResponse = {
      project: { id: 'proj-qs', name: 'quickstart-2026-05-09', created_at: '2026-05-09T00:00:00Z' },
      key: {
        id: 'key-qs',
        key: 'gw-quickstart1234567890abcdef12',
        key_prefix: 'gw-quick',
        name: 'quickstart',
        rpm_limit: 0,
        budget_limit_usd: 0,
      },
    };
    mockFetch.mockResolvedValue(jsonResponse(mockResponse, 201));

    const admin = makeAdmin();
    const result = await admin.quickstart({ name: 'my-quickstart', rpm_limit: 100 });

    expect(result.project.id).toBe('proj-qs');
    expect(result.key.id).toBe('key-qs');
    expect(result.key.key).toBe('gw-quickstart1234567890abcdef12');
    expect(mockFetch).toHaveBeenCalledWith(
      'http://localhost:8080/admin/quickstart',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ name: 'my-quickstart', rpm_limit: 100 }),
      }),
    );
  });
});

// ── TEST-22-01-08: deleteProject(id) sends DELETE and resolves void ──────

describe('deleteProject', () => {
  it('sends DELETE and resolves on success', async () => {
    mockFetch.mockResolvedValue(new Response(null, { status: 204 }));

    const admin = makeAdmin();
    await admin.deleteProject('proj-123');

    expect(mockFetch).toHaveBeenCalledWith(
      'http://localhost:8080/admin/projects/proj-123',
      expect.objectContaining({ method: 'DELETE' }),
    );
  });
});

// ── TEST-22-01-09: updateKey(id, fields) sends PUT and returns VirtualKey ──

describe('updateKey', () => {
  it('sends PUT with fields and returns updated VirtualKey', async () => {
    const mockKey = {
      id: 'key-1',
      project_id: 'proj-1',
      key_prefix: 'gw-abcd',
      name: 'updated-key',
      allowed_models: ['gpt-4'],
      allowed_providers: [],
      allowed_tools: [],
      rpm_limit: 200,
      tpm_limit: 0,
      budget_limit_usd: 100,
      budget_period: 'monthly',
      expires_at: null,
      revoked_at: null,
      created_at: '2026-05-09T00:00:00Z',
      allow_mcp_server: false,
      mcp_tool_name: '',
    };
    mockFetch.mockResolvedValue(jsonResponse(mockKey));

    const admin = makeAdmin();
    const result = await admin.updateKey('key-1', { rpm_limit: 200, name: 'updated-key' });

    expect(result.rpm_limit).toBe(200);
    expect(result.name).toBe('updated-key');
    expect(mockFetch).toHaveBeenCalledWith(
      'http://localhost:8080/admin/keys/key-1',
      expect.objectContaining({
        method: 'PUT',
        body: JSON.stringify({ rpm_limit: 200, name: 'updated-key' }),
      }),
    );
  });
});

// ── TEST-22-01-10: patchKey(id, fields) sends PATCH and returns VirtualKey ──

describe('patchKey', () => {
  it('sends PATCH with partial fields', async () => {
    const mockKey = {
      id: 'key-1',
      project_id: 'proj-1',
      key_prefix: 'gw-abcd',
      name: 'my-key',
      allowed_models: [],
      allowed_providers: [],
      allowed_tools: [],
      rpm_limit: 500,
      tpm_limit: 0,
      budget_limit_usd: 0,
      budget_period: 'monthly',
      expires_at: null,
      revoked_at: null,
      created_at: '2026-05-09T00:00:00Z',
      allow_mcp_server: false,
      mcp_tool_name: '',
    };
    mockFetch.mockResolvedValue(jsonResponse(mockKey));

    const admin = makeAdmin();
    const result = await admin.patchKey('key-1', { rpm_limit: 500 });

    expect(result.rpm_limit).toBe(500);
    expect(mockFetch).toHaveBeenCalledWith(
      'http://localhost:8080/admin/keys/key-1',
      expect.objectContaining({
        method: 'PATCH',
        body: JSON.stringify({ rpm_limit: 500 }),
      }),
    );
  });
});

// ── TEST-22-01-11: revokeKey(id) sends DELETE and resolves void ──────────

describe('revokeKey', () => {
  it('sends DELETE and resolves on success', async () => {
    mockFetch.mockResolvedValue(new Response(null, { status: 204 }));

    const admin = makeAdmin();
    await admin.revokeKey('key-1');

    expect(mockFetch).toHaveBeenCalledWith(
      'http://localhost:8080/admin/keys/key-1',
      expect.objectContaining({ method: 'DELETE' }),
    );
  });
});
