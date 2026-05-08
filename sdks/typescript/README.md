# @openlimit/sdk

TypeScript SDK for the [OpenLimit](https://github.com/nicklausw/openlimit) AI API Gateway.

Zero runtime dependencies. Works with Node.js 18+ (uses native `fetch`).

## Installation

```bash
npm install @openlimit/sdk
```

## Quick Start

```typescript
import { OpenLimitClient } from '@openlimit/sdk';

const client = new OpenLimitClient({
  baseURL: 'http://localhost:8080',
  apiKey: 'your-api-key',
});
```

## Chat Completions

### Non-Streaming

```typescript
const response = await client.chatCompletion({
  model: 'gpt-4',
  messages: [
    { role: 'system', content: 'You are a helpful assistant.' },
    { role: 'user', content: 'What is OpenLimit?' },
  ],
  temperature: 0.7,
});

console.log(response.choices[0].message.content);
```

### Streaming

```typescript
for await (const chunk of client.chatCompletionStream({
  model: 'gpt-4',
  messages: [{ role: 'user', content: 'Tell me a story' }],
})) {
  process.stdout.write(chunk.choices[0].delta.content ?? '');
}
```

## Embeddings

```typescript
const result = await client.embeddings({
  model: 'text-embedding-3-small',
  input: 'Hello world',
});

console.log(result.data[0].embedding); // [0.1, 0.2, ...]
```

## Models

```typescript
const models = await client.models();
for (const model of models.data) {
  console.log(model.id, model.owned_by);
}
```

## Health Check

```typescript
const health = await client.health();
console.log(health.status); // "ok"
```

## Error Handling

```typescript
import { APIError, TimeoutError } from '@openlimit/sdk';

try {
  await client.chatCompletion({ model: 'gpt-4', messages: [...] });
} catch (err) {
  if (err instanceof APIError) {
    console.error(`API Error ${err.status}: ${err.message}`);
    console.error(`Type: ${err.type}, Request ID: ${err.requestId}`);
  } else if (err instanceof TimeoutError) {
    console.error('Request timed out');
  }
}
```

## Configuration

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `baseURL` | `string` | *required* | Gateway URL |
| `apiKey` | `string` | *required* | API key |
| `timeout` | `number` | `30000` | Request timeout (ms) |
| `defaultHeaders` | `Record<string, string>` | `{}` | Custom headers |

## Data Residency (OpenLimit Extension)

```typescript
const response = await client.chatCompletion({
  model: 'gpt-4',
  messages: [{ role: 'user', content: 'Hello' }],
  data_residency: 'eu', // Routes to EU providers only
});
```

## License

MIT
