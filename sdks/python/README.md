# openlimit

Python SDK for the [OpenLimit](https://github.com/nicklausw/openlimit) AI API Gateway.

Zero third-party dependencies. Works with Python 3.10+.

## Installation

```bash
pip install openlimit
```

## Quick Start

```python
from openlimit import OpenLimitClient

client = OpenLimitClient(
    base_url="http://localhost:8080",
    api_key="your-api-key",
)
```

## Chat Completions

### Non-Streaming

```python
from openlimit.types import ChatCompletionRequest, ChatMessage

response = client.chat_completion(ChatCompletionRequest(
    model="gpt-4",
    messages=[
        ChatMessage(role="system", content="You are a helpful assistant."),
        ChatMessage(role="user", content="What is OpenLimit?"),
    ],
    temperature=0.7,
))

print(response.choices[0].message.content)
```

### Streaming

```python
for chunk in client.chat_completion_stream(ChatCompletionRequest(
    model="gpt-4",
    messages=[ChatMessage(role="user", content="Tell me a story")],
)):
    content = chunk.choices[0].delta.content
    if content:
        print(content, end="", flush=True)
```

## Embeddings

```python
from openlimit.types import EmbeddingsRequest

result = client.embeddings(EmbeddingsRequest(
    model="text-embedding-3-small",
    input="Hello world",
))

print(result.data[0].embedding)  # [0.1, 0.2, ...]
```

## Models

```python
models = client.models()
for model in models.data:
    print(model.id, model.owned_by)
```

## Health Check

```python
health = client.health()
print(health.status)  # "ok"
```

## Error Handling

```python
from openlimit import APIError, TimeoutError

try:
    client.chat_completion(...)
except APIError as e:
    print(f"API Error {e.status}: {e}")
    print(f"Type: {e.error_type}, Request ID: {e.request_id}")
except TimeoutError as e:
    print("Request timed out")
```

## License

MIT
