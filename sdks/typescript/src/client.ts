import type {
  OpenLimitConfig,
  ChatCompletionRequest,
  ChatCompletionResponse,
  ChatCompletionChunk,
  EmbeddingsRequest,
  EmbeddingsResponse,
  ModelsResponse,
  HealthResponse,
  ErrorResponse,
  ResponseHeaders,
} from './types';
import { APIError, TimeoutError, NetworkError } from './errors';
import { parseSSEResponse } from './streaming';

const DEFAULT_TIMEOUT = 30_000;

export class OpenLimitClient {
  private readonly baseURL: string;
  private readonly apiKey: string;
  private readonly timeout: number;
  private readonly headers: Record<string, string>;

  constructor(config: OpenLimitConfig) {
    this.baseURL = config.baseURL.replace(/\/+$/, '');
    this.apiKey = config.apiKey;
    this.timeout = config.timeout ?? DEFAULT_TIMEOUT;
    this.headers = {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${this.apiKey}`,
      ...config.defaultHeaders,
    };
  }

  // ── Chat Completions ──────────────────────────────────

  /**
   * Create a non-streaming chat completion.
   */
  async chatCompletion(
    req: ChatCompletionRequest,
  ): Promise<ChatCompletionResponse> {
    const body = { ...req, stream: false };
    const response = await this.fetchRaw('POST', '/v1/chat/completions', body);

    if (!response.ok) {
      await this.handleError(response);
    }

    const data = (await response.json()) as ChatCompletionResponse;
    data.headers = this.extractHeaders(response);
    return data;
  }

  /**
   * Create a streaming chat completion.
   * Returns an async iterator of ChatCompletionChunk.
   */
  async *chatCompletionStream(
    req: ChatCompletionRequest,
  ): AsyncGenerator<ChatCompletionChunk> {
    const body = { ...req, stream: true };
    const response = await this.fetchRaw('POST', '/v1/chat/completions', body);

    if (!response.ok) {
      await this.handleError(response);
    }

    if (!response.body) {
      throw new Error('Response body is null for streaming request');
    }

    yield* parseSSEResponse(response.body);
  }

  // ── Embeddings ────────────────────────────────────────

  /**
   * Create embeddings for the given input.
   */
  async embeddings(req: EmbeddingsRequest): Promise<EmbeddingsResponse> {
    const response = await this.fetchRaw('POST', '/v1/embeddings', req);

    if (!response.ok) {
      await this.handleError(response);
    }

    const data = (await response.json()) as EmbeddingsResponse;
    data.headers = this.extractHeaders(response);
    return data;
  }

  // ── Models ────────────────────────────────────────────

  /**
   * List available models.
   */
  async models(): Promise<ModelsResponse> {
    return this.request<ModelsResponse>('GET', '/v1/models');
  }

  // ── Health ────────────────────────────────────────────

  /**
   * Check gateway health (no auth required).
   */
  async health(): Promise<HealthResponse> {
    return this.request<HealthResponse>('GET', '/health', undefined, false);
  }

  // ── Internal ──────────────────────────────────────────

  private async request<T>(
    method: string,
    path: string,
    body?: unknown,
    auth = true,
  ): Promise<T> {
    const response = await this.fetchRaw(method, path, body, auth);

    if (!response.ok) {
      await this.handleError(response);
    }

    return response.json() as Promise<T>;
  }

  private async fetchRaw(
    method: string,
    path: string,
    body?: unknown,
    auth = true,
  ): Promise<Response> {
    const headers: Record<string, string> = { ...this.headers };
    if (!auth) {
      delete headers['Authorization'];
    }

    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.timeout);

    try {
      const response = await fetch(`${this.baseURL}${path}`, {
        method,
        headers,
        body: body ? JSON.stringify(body) : undefined,
        signal: controller.signal,
      });
      return response;
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') {
        throw new TimeoutError(this.timeout);
      }
      throw new NetworkError(err instanceof Error ? err : new Error(String(err)));
    } finally {
      clearTimeout(timer);
    }
  }

  /**
   * Extract operational X-* headers from a fetch Response.
   */
  private extractHeaders(response: Response): ResponseHeaders {
    const get = (name: string): string | undefined =>
      response.headers.get(name) ?? undefined;

    return {
      'X-Provider': get('X-Provider'),
      'X-Cache': get('X-Cache'),
      'X-Cost-USD': get('X-Cost-USD'),
      'X-RateLimit-Limit': get('X-RateLimit-Limit'),
      'X-RateLimit-Remaining': get('X-RateLimit-Remaining'),
      'X-RateLimit-Reset': get('X-RateLimit-Reset'),
      'X-Request-ID': get('X-Request-ID'),
    };
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
