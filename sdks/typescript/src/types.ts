// ── Configuration ──────────────────────────────────────────

export interface OpenLimitConfig {
  /** Gateway base URL (e.g. "http://localhost:8080") */
  baseURL: string;
  /** API key for authentication */
  apiKey: string;
  /** Request timeout in milliseconds (default: 30000) */
  timeout?: number;
  /** Custom headers to include in every request */
  defaultHeaders?: Record<string, string>;
}

// ── Chat Completions ──────────────────────────────────────

export interface ChatCompletionRequest {
  model: string;
  messages: ChatMessage[];
  temperature?: number;
  top_p?: number;
  max_tokens?: number;
  stream?: boolean;
  stop?: string | string[];
  presence_penalty?: number;
  frequency_penalty?: number;
  user?: string;
  /** OpenLimit extension: data residency filter */
  data_residency?: string;
}

export interface ChatMessage {
  role: 'system' | 'user' | 'assistant' | 'tool';
  content: string;
  name?: string;
  tool_call_id?: string;
  tool_calls?: ToolCall[];
}

export interface ToolCall {
  id: string;
  type: 'function';
  function: { name: string; arguments: string };
}

export interface ChatCompletionResponse {
  id: string;
  object: 'chat.completion';
  created: number;
  model: string;
  choices: ChatChoice[];
  usage?: Usage;
  headers?: ResponseHeaders;
}

export interface ChatChoice {
  index: number;
  message: ChatMessage;
  finish_reason: string;
}

export interface Usage {
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
}

// ── Streaming ─────────────────────────────────────────────

export interface ChatCompletionChunk {
  id: string;
  object: 'chat.completion.chunk';
  created: number;
  model: string;
  choices: ChunkChoice[];
}

export interface ChunkChoice {
  index: number;
  delta: Partial<ChatMessage>;
  finish_reason: string | null;
}

// ── Embeddings ────────────────────────────────────────────

export interface EmbeddingsRequest {
  model: string;
  input: string | string[] | number[] | number[][];
  encoding_format?: 'float' | 'base64';
  dimensions?: number;
  user?: string;
}

export interface EmbeddingsResponse {
  object: 'list';
  data: EmbeddingData[];
  model: string;
  usage: Usage;
  headers?: ResponseHeaders;
}

export interface EmbeddingData {
  object: 'embedding';
  embedding: number[];
  index: number;
}

// ── Models ────────────────────────────────────────────────

export interface ModelsResponse {
  object: 'list';
  data: ModelInfo[];
}

export interface ModelInfo {
  id: string;
  object: 'model';
  created: number;
  owned_by: string;
}

// ── Health ────────────────────────────────────────────────

export interface HealthResponse {
  status: string;
  version?: string;
  uptime_seconds?: number;
}

// ── Response Headers ─────────────────────────────────────

export interface ResponseHeaders {
  'X-Provider'?: string;
  'X-Cache'?: string;
  'X-Cost-USD'?: string;
  'X-RateLimit-Limit'?: string;
  'X-RateLimit-Remaining'?: string;
  'X-RateLimit-Reset'?: string;
  'X-Request-ID'?: string;
}

// ── Admin Configuration ───────────────────────────────────

export interface AdminConfig {
  /** Gateway base URL (e.g. "http://localhost:8080") */
  baseURL: string;
  /** Admin bearer token for authentication */
  adminToken: string;
  /** Request timeout in milliseconds (default: 30000) */
  timeout?: number;
  /** Custom headers to include in every request */
  defaultHeaders?: Record<string, string>;
}

// ── Admin: Projects ───────────────────────────────────────

export interface Project {
  id: string;
  name: string;
  created_at: string;
}

// ── Admin: Keys ───────────────────────────────────────────

export interface VirtualKey {
  id: string;
  project_id: string;
  key_prefix: string;
  name: string;
  allowed_models: string[];
  allowed_providers: string[];
  allowed_tools: string[];
  rpm_limit: number;
  tpm_limit: number;
  budget_limit_usd: number;
  budget_period: string;
  expires_at: string | null;
  revoked_at: string | null;
  created_at: string;
  allow_mcp_server: boolean;
  mcp_tool_name: string;
}

export interface CreateKeyRequest {
  project_id: string;
  name: string;
  allowed_models?: string[];
  allowed_providers?: string[];
  allowed_tools?: string[];
  rpm_limit?: number;
  tpm_limit?: number;
  budget_limit_usd?: number;
  budget_period?: string;
  allow_mcp_server?: boolean;
  mcp_tool_name?: string;
}

export interface CreateKeyResponse {
  id: string;
  key: string;
  key_prefix: string;
  name: string;
  project_id: string;
}

// ── Admin: Usage ──────────────────────────────────────────

export interface UsageEntry {
  id: number;
  request_id: string;
  project_id: string | null;
  virtual_key_id: string | null;
  model: string;
  provider: string;
  provider_model: string;
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
  cost_usd: number;
  cache_hit: boolean;
  stream: boolean;
  attempts: number;
  duration_ms: number;
  error: string;
  created_at: string;
}

export interface UsageSummaryEntry {
  period: string;
  model: string;
  provider: string;
  request_count: number;
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
  cost_usd: number;
}

export interface UsageFilters {
  project_id?: string;
  key_id?: string;
  model?: string;
  from?: string;
  to?: string;
  limit?: number;
}

export interface UsageSummaryFilters {
  project_id?: string;
  period?: 'daily' | 'monthly';
}

// ── Admin: Quickstart ─────────────────────────────────────

export interface QuickstartOptions {
  name?: string;
  rpm_limit?: number;
  budget_limit_usd?: number;
}

export interface QuickstartResponse {
  project: Project;
  key: CreateKeyResponse;
}

// ── Errors ────────────────────────────────────────────────

export interface ErrorResponse {
  error: {
    message: string;
    type: string;
    code?: string;
    request_id?: string;
  };
}
