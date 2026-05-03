# Security

**What you'll learn:** OpenLimit's security model, KMS encryption options (static, AWS KMS, HashiCorp Vault), data residency enforcement, and how virtual keys are protected at rest.

---

## Security Model

OpenLimit operates as a gateway between consumers and AI providers. The security model has three layers:

### Layer 1: Network

- Provider API keys never leave the gateway — they are stored in the config (or environment variables) and used only for outbound calls to providers
- Consumers authenticate with virtual keys (`gw-` prefix) that are scoped to specific permissions
- Admin API is protected by a bearer token or OIDC JWT
- Run the gateway in a private VPC/subnet. Use Ingress or a load balancer for external access

### Layer 2: Governance

- Virtual keys enforce rate limits, budgets, model restrictions, and tool permissions
- RBAC controls admin API access (admin, editor, viewer roles)
- Guardrails inspect and filter content at input and output stages
- All admin mutations are recorded in immutable audit logs

### Layer 3: Encryption

- Provider API keys can be encrypted at rest using KMS
- Virtual keys are bcrypt-hashed at rest (never stored in plaintext)
- KMS uses AES-256-GCM for envelope encryption
- Fail-closed: encrypted keys without a working KMS are skipped, not used as plaintext

---

## KMS Encryption

Enable KMS to encrypt provider API keys in your config file. No plaintext keys in YAML, environment variables, or persistent storage.

### Static KMS (no external dependencies)

Generate a 32-byte DEK:

```bash
openssl rand -base64 32
```

Configure:

```yaml
kms:
  enabled: true
  type: static
  key_id: "v1"    # optional label for key rotation
```

Set the environment variable:

```bash
export KMS_STATIC_KEY="<base64-encoded 32-byte key>"
```

This is the simplest option — no external services required. The DEK is read once at startup.

### AWS KMS

```yaml
kms:
  enabled: true
  type: aws-kms
  key_id: "arn:aws:kms:us-east-1:123456789:key/your-key-id"
```

Uses the standard AWS SDK credential chain (environment variables, instance metadata, IAM roles, etc.).

### HashiCorp Vault KMS

```yaml
kms:
  enabled: true
  type: vault
  key_id: "secret/data/openlimit-dek"    # KV v2 secret path
  vault:
    addr: "https://vault.example.com:8200"
    # token: ""             # or set VAULT_TOKEN env var
    # namespace: ""         # Vault Enterprise namespaces
    # tls_skip_verify: false
```

The secret at `key_id` must contain:

```json
{
  "key": "<base64-encoded 32-byte DEK>",
  "key_id": "v1"
}
```

Token resolution order: `vault.token` config → `VAULT_TOKEN` env var → error if both empty.

### Encrypt a provider key

```yaml
providers:
  openai:
    type: openai
    keys:
      - id: main
        encrypted_value: "dek-v1:djE=:6gHx9...base64..."
        weight: 1
```

Use the OpenLimit CLI helper or any AES-256-GCM tool to encrypt values.

### Ciphertext format

```
dek-v1:base64(keyID):base64(nonce+ciphertext)
```

The key ID is embedded in the ciphertext itself, enabling key rotation without re-encrypting all values.

### Key rotation

1. Generate a new DEK with a new `key_id`
2. Re-encrypt provider keys with the new DEK
3. Update `kms.key_id` in config
4. Restart pods (rolling update)

Old ciphertext still decrypts because the key ID is embedded in the ciphertext — the gateway uses the appropriate DEK based on the embedded ID.

### Fail-closed behavior

If `encrypted_value` is present but KMS is not configured (or fails to initialize), the provider key is **skipped** with an error logged. The gateway does not fall back to using the value as plaintext. This ensures that encryption is never silently bypassed.

The `/ready` health endpoint includes `kms.ready` status.

---

## Data Residency

Enforce that requests are only routed to providers in specific geographic regions.

### Configure region residency

```yaml
providers:
  openai:
    type: openai
    regions:
      - name: eu-west
        base_url: https://eu.api.openai.com/v1
        priority: 1
        data_residency: eu       # explicit tag
```

### Enforce via request header

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "X-Data-Residency: eu" \
  -H "Content-Type: application/json" \
  -d '{"model": "fast", "messages": [{"role": "user", "content": "Hello"}]}'
```

### Matching rules

1. Explicit `data_residency` tag on the region config is checked first
2. Falls back to region name prefix: `eu` matches `eu-west`, `eu-central`, etc.
3. Requests with no matching providers receive HTTP 403 (`residency_denied`)
4. Requests without the `X-Data-Residency` header are unaffected

### Monitoring

```bash
curl -s http://localhost:8080/metrics | grep gateway_residency_filter_total
```

The `gateway_residency_filter_total{result="allowed|denied"}` counter tracks filter decisions.

---

## Virtual Key Protection

### bcrypt hashing

Virtual API keys are bcrypt-hashed before storage. The full key value is returned only once during creation and cannot be recovered from the database.

### Key lookup caching

Key lookups use an in-memory LRU cache with configurable TTL. This reduces database load but means that revoked keys may continue to work for up to the TTL duration. Set a shorter TTL for stricter revocation.

### Key prefix

All virtual keys start with `gw-` for easy identification in logs. The first 6 characters (`key_prefix`) are stored in plaintext for filtering in admin queries.

---

## Redis Security

- Cache values are **not encrypted** in Redis — run Redis in a private VPC
- Use Redis AUTH (password) for basic access control
- Redis should not be exposed to the internet
- Standard practice: ElastiCache (AWS), Redis on GKE, or Azure Cache for Redis with private endpoints

---

## OIDC Security Considerations

- The gateway validates JWT access tokens — it does not implement the OAuth2 authorization code flow
- JWT validation uses the IdP's JWKS endpoint for signature verification
- Token expiry is checked on every request
- Role changes take effect on the next request (not cached in the JWT)
- Only one OIDC issuer per gateway instance

---

## KMS Limitations

- **No memory zeroing** — Standard Go limitation. Plaintext keys live in memory for the process lifetime.
- **Static KMS key rotation requires restart** — The DEK is read once at startup.
- **Vault token lifecycle** — Short-lived tokens expire without renewal. Use periodic or long-lived tokens.

---

## Security checklist

- [ ] Enable `auth.enabled: true` to require virtual keys
- [ ] Set a strong `admin.bearer_token` (or use OIDC)
- [ ] Encrypt provider API keys with KMS
- [ ] Run the gateway in a private VPC
- [ ] Use TLS for all external connections (Ingress, database)
- [ ] Enable RBAC for multi-team environments
- [ ] Configure guardrails for content safety
- [ ] Review audit logs regularly
- [ ] Enable data residency for compliance requirements
- [ ] Run Redis with AUTH in a private network

---

## Next steps

- **[Configuration](configuration.md)** — KMS, Redis, and auth YAML reference
- **[Governance](governance.md)** — Virtual keys, RBAC, and audit logs
- **[Deployment](deployment.md)** — Production deployment with security defaults
- **[Migration to v1.0](migration-v1.0.md)** — Upgrading security features
