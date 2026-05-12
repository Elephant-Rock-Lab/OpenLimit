import type {
  AdminConfig,
  Project,
  VirtualKey,
  CreateKeyRequest,
  CreateKeyResponse,
  UsageEntry,
  UsageSummaryEntry,
  UsageFilters,
  UsageSummaryFilters,
  QuickstartOptions,
  QuickstartResponse,
  ErrorResponse,
} from './types';
import { APIError, TimeoutError } from './errors';

const DEFAULT_TIMEOUT = 30_000;

/**
 * Admin client for the OpenLimit gateway.
 *
 * Uses admin bearer token auth (separate from OpenLimitClient's API key).
 * Provides methods for project CRUD, key CRUD, usage queries, and quickstart.
 */
export class OpenLimitAdmin {
  private readonly baseURL: string;
  private readonly adminToken: string;
  private readonly timeout: number;
  private readonly headers: Record<string, string>;

  constructor(config: AdminConfig) {
    this.baseURL = config.baseURL.replace(/\/+$/, '');
    this.adminToken = config.adminToken;
    this.timeout = config.timeout ?? DEFAULT_TIMEOUT;
    this.headers = {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${this.adminToken}`,
      ...config.defaultHeaders,
    };
  }

  // ── Projects ────────────────────────────────────────────

  /**
   * List all projects.
   */
  async listProjects(): Promise<Project[]> {
    return this.request<Project[]>('GET', '/admin/projects');
  }

  /**
   * Create a new project.
   */
  async createProject(name: string): Promise<Project> {
    return this.request<Project>('POST', '/admin/projects', { name });
  }

  /**
   * Delete a project by ID.
   */
  async deleteProject(id: string): Promise<void> {
    await this.requestVoid('DELETE', `/admin/projects/${id}`);
  }

  // ── Keys ────────────────────────────────────────────────

  /**
   * List virtual keys, optionally filtered by project.
   */
  async listKeys(projectId?: string): Promise<VirtualKey[]> {
    const params = new URLSearchParams();
    if (projectId) {
      params.set('project_id', projectId);
    }
    const qs = params.toString();
    const path = qs ? `/admin/keys?${qs}` : '/admin/keys';
    return this.request<VirtualKey[]>('GET', path);
  }

  /**
   * Create a new virtual key.
   */
  async createKey(req: CreateKeyRequest): Promise<CreateKeyResponse> {
    return this.request<CreateKeyResponse>('POST', '/admin/keys', req);
  }

  /**
   * Full update (PUT) of updatable key fields.
   */
  async updateKey(id: string, fields: Record<string, unknown>): Promise<VirtualKey> {
    return this.request<VirtualKey>('PUT', `/admin/keys/${id}`, fields);
  }

  /**
   * Partial update (PATCH) of updatable key fields.
   */
  async patchKey(id: string, fields: Record<string, unknown>): Promise<VirtualKey> {
    return this.request<VirtualKey>('PATCH', `/admin/keys/${id}`, fields);
  }

  /**
   * Revoke (soft-delete) a key.
   */
  async revokeKey(id: string): Promise<void> {
    await this.requestVoid('DELETE', `/admin/keys/${id}`);
  }

  // ── Usage ───────────────────────────────────────────────

  /**
   * Query raw usage log entries with optional filters.
   */
  async queryUsage(filters?: UsageFilters): Promise<UsageEntry[]> {
    const params = new URLSearchParams();
    if (filters) {
      if (filters.project_id) params.set('project_id', filters.project_id);
      if (filters.key_id) params.set('key_id', filters.key_id);
      if (filters.model) params.set('model', filters.model);
      if (filters.from) params.set('from', filters.from);
      if (filters.to) params.set('to', filters.to);
      if (filters.limit) params.set('limit', String(filters.limit));
    }
    const qs = params.toString();
    const path = qs ? `/admin/usage?${qs}` : '/admin/usage';
    return this.request<UsageEntry[]>('GET', path);
  }

  /**
   * Query aggregated usage summary.
   */
  async usageSummary(filters?: UsageSummaryFilters): Promise<UsageSummaryEntry[]> {
    const params = new URLSearchParams();
    if (filters) {
      if (filters.project_id) params.set('project_id', filters.project_id);
      if (filters.period) params.set('period', filters.period);
    }
    const qs = params.toString();
    const path = qs ? `/admin/usage/summary?${qs}` : '/admin/usage/summary';
    return this.request<UsageSummaryEntry[]>('GET', path);
  }

  // ── Quickstart ──────────────────────────────────────────

  /**
   * One-shot quickstart: creates a project and a key, returns both.
   */
  async quickstart(opts?: QuickstartOptions): Promise<QuickstartResponse> {
    return this.request<QuickstartResponse>('POST', '/admin/quickstart', opts ?? {});
  }

  // ── Internal ────────────────────────────────────────────

  private async request<T>(
    method: string,
    path: string,
    body?: unknown,
  ): Promise<T> {
    const response = await this.fetchRaw(method, path, body);

    if (!response.ok) {
      await this.handleError(response);
    }

    return response.json() as Promise<T>;
  }

  private async requestVoid(
    method: string,
    path: string,
  ): Promise<void> {
    const response = await this.fetchRaw(method, path);

    if (!response.ok) {
      await this.handleError(response);
    }
  }

  private async fetchRaw(
    method: string,
    path: string,
    body?: unknown,
  ): Promise<Response> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.timeout);

    try {
      const response = await fetch(`${this.baseURL}${path}`, {
        method,
        headers: { ...this.headers },
        body: body ? JSON.stringify(body) : undefined,
        signal: controller.signal,
      });
      return response;
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') {
        throw new TimeoutError(this.timeout);
      }
      throw err;
    } finally {
      clearTimeout(timer);
    }
  }

  private async handleError(response: Response): Promise<never> {
    let errorBody: ErrorResponse;
    try {
      errorBody = (await response.json()) as ErrorResponse;
    } catch {
      errorBody = {
        error: {
          message: `HTTP ${response.status}: ${response.statusText}`,
          type: 'http_error',
        },
      };
    }
    throw new APIError(response.status, errorBody);
  }
}
