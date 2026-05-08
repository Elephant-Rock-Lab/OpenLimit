import { describe, it, expect, vi, beforeEach } from 'vitest';
import { OpenLimitClient, APIError, TimeoutError } from '../src/index';

// Mock global fetch
const mockFetch = vi.fn();
vi.stubGlobal('fetch', mockFetch);

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function makeClient(): OpenLimitClient {
  return new OpenLimitClient({
    baseURL: 'http://localhost:8080',
    apiKey: 'test-key-123',
  });
}

beforeEach(() => {
  mockFetch.mockReset();
});

// ── TEST-14-01-01: Non-streaming chat completion ────────

describe('chatCompletion', () => {
  it('returns a chat completion response', async () => {
    const mockResponse = {
      id: 'chatcmpl-123',
      object: 'chat.completion',
      created: 1234567890,
      model: 'gpt-4',
      choices: [
        {
          index: 0,
          message: { role: 'assistant', content: 'Hello!' },
          finish_reason: 'stop',
        },
      ],
      usage: { prompt_tokens: 10, completion_tokens: 5, total_tokens: 15 },
    };
    mockFetch.mockResolvedValue(jsonResponse(mockResponse));

    const client = makeClient();
    const result = await client.chatCompletion({
      model: 'gpt-4',
      messages: [{ role: 'user', content: 'Hi' }],
    });

    expect(result.id).toBe('chatcmpl-123');
    expect(result.choices[0].message.content).toBe('Hello!');
    expect(mockFetch).toHaveBeenCalledWith(
      'http://localhost:8080/v1/chat/completions',
      expect.objectContaining({ method: 'POST' }),
    );
  });
});

// ── TEST-14-01-02: Streaming chat completion ────────────

describe('chatCompletionStream', () => {
  it('yields SSE chunks', async () => {
    const sseBody = [
      'data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"gpt-4","choices":[{"index":0,"delta":{"role":"assistant","content":"Hi"},"finish_reason":null}]}',
      '',
      'data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":2,"model":"gpt-4","choices":[{"index":0,"delta":{"content":"!"},"finish_reason":"stop"}]}',
      '',
      'data: [DONE]',
      '',
    ].join('\n');

    const stream = new ReadableStream({
      start(controller) {
        controller.enqueue(new TextEncoder().encode(sseBody));
        controller.close();
      },
    });

    mockFetch.mockResolvedValue(
      new Response(stream, {
        status: 200,
        headers: { 'Content-Type': 'text/event-stream' },
      }),
    );

    const client = makeClient();
    const chunks = [];
    for await (const chunk of client.chatCompletionStream({
      model: 'gpt-4',
      messages: [{ role: 'user', content: 'Hi' }],
    })) {
      chunks.push(chunk);
    }

    expect(chunks).toHaveLength(2);
    expect(chunks[0].choices[0].delta.content).toBe('Hi');
    expect(chunks[1].choices[0].delta.content).toBe('!');
    expect(chunks[1].choices[0].finish_reason).toBe('stop');
  });
});

// ── TEST-14-01-03: Embeddings ───────────────────────────

describe('embeddings', () => {
  it('returns embedding vectors', async () => {
    const mockResponse = {
      object: 'list',
      data: [
        { object: 'embedding', embedding: [0.1, 0.2, 0.3], index: 0 },
      ],
      model: 'text-embedding-3-small',
      usage: { prompt_tokens: 5, completion_tokens: 0, total_tokens: 5 },
    };
    mockFetch.mockResolvedValue(jsonResponse(mockResponse));

    const client = makeClient();
    const result = await client.embeddings({
      model: 'text-embedding-3-small',
      input: 'Hello world',
    });

    expect(result.data).toHaveLength(1);
    expect(result.data[0].embedding).toEqual([0.1, 0.2, 0.3]);
  });
});

// ── TEST-14-01-04: Models listing ───────────────────────

describe('models', () => {
  it('returns list of models', async () => {
    const mockResponse = {
      object: 'list',
      data: [
        { id: 'gpt-4', object: 'model', created: 1, owned_by: 'openai' },
        { id: 'claude-3', object: 'model', created: 2, owned_by: 'anthropic' },
      ],
    };
    mockFetch.mockResolvedValue(jsonResponse(mockResponse));

    const client = makeClient();
    const result = await client.models();

    expect(result.data).toHaveLength(2);
    expect(result.data[0].id).toBe('gpt-4');
    expect(result.data[1].id).toBe('claude-3');
  });
});

// ── TEST-14-01-05: Health check ─────────────────────────

describe('health', () => {
  it('returns health status without auth', async () => {
    const mockResponse = { status: 'ok', version: 'v1.2.0' };
    mockFetch.mockResolvedValue(jsonResponse(mockResponse));

    const client = makeClient();
    const result = await client.health();

    expect(result.status).toBe('ok');
    // Verify Authorization header was NOT sent
    const callArgs = mockFetch.mock.calls[0][1];
    expect(callArgs.headers.Authorization).toBeUndefined();
  });
});

// ── TEST-14-01-06: Error handling ───────────────────────

describe('error handling', () => {
  it('throws APIError for non-2xx responses', async () => {
    const errorResponse = {
      error: {
        message: 'Invalid API key',
        type: 'authentication_error',
        code: 'invalid_api_key',
        request_id: 'req-123',
      },
    };
    mockFetch.mockResolvedValue(jsonResponse(errorResponse, 401));

    const client = makeClient();
    try {
      await client.chatCompletion({
        model: 'gpt-4',
        messages: [{ role: 'user', content: 'Hi' }],
      });
      expect.unreachable('should have thrown');
    } catch (err) {
      expect(err).toBeInstanceOf(APIError);
      const apiErr = err as APIError;
      expect(apiErr.status).toBe(401);
      expect(apiErr.type).toBe('authentication_error');
      expect(apiErr.requestId).toBe('req-123');
    }
  });

  it('throws TimeoutError when request exceeds timeout', async () => {
    mockFetch.mockImplementation(
      () =>
        new Promise((_, reject) => {
          const error = new DOMException('The operation was aborted', 'AbortError');
          setTimeout(() => reject(error), 10);
        }),
    );

    const client = new OpenLimitClient({
      baseURL: 'http://localhost:8080',
      apiKey: 'test-key',
      timeout: 1, // 1ms — will always timeout
    });

    await expect(
      client.chatCompletion({
        model: 'gpt-4',
        messages: [{ role: 'user', content: 'Hi' }],
      }),
    ).rejects.toThrow(TimeoutError);
  });
});
