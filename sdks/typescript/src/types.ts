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

// ── Errors ────────────────────────────────────────────────

export interface ErrorResponse {
  error: {
    message: string;
    type: string;
    code?: string;
    request_id?: string;
  };
}
