import type { ChatCompletionChunk } from './types';

/**
 * Parse an SSE stream into typed chunks.
 * Yields parsed JSON objects from "data: ..." lines.
 * Handles "data: [DONE]" as the stream terminator.
 */
export async function* parseSSEResponse(
  body: ReadableStream<Uint8Array>,
): AsyncGenerator<ChatCompletionChunk> {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';

  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;

      buffer += decoder.decode(value, { stream: true });
      const lines = buffer.split('\n');
      buffer = lines.pop() ?? '';

      for (const line of lines) {
        const trimmed = line.trim();
        if (!trimmed || trimmed.startsWith(':')) continue;
        if (trimmed === 'data: [DONE]') return;
        if (!trimmed.startsWith('data: ')) continue;

        const json = trimmed.slice(6);
        try {
          yield JSON.parse(json) as ChatCompletionChunk;
        } catch {
          // Skip malformed JSON chunks
        }
      }
    }
  } finally {
    reader.releaseLock();
  }
}
