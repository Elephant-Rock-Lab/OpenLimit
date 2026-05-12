package providers

import (
	"context"
	"errors"
	"net/http"
	"os"
	"sync"

	"openlimit/internal/config"
	"openlimit/internal/kms"
	"openlimit/internal/schema/openai"
)

var ErrRetryable = errors.New("retryable provider error")

// MaxProviderResponseSize is the maximum bytes read from any external provider
// response body. Set to 50MB to prevent OOM from malicious or buggy providers.
const MaxProviderResponseSize int64 = 50 << 20 // 50MB

type Target struct {
	Provider      string
	Model         string
	Region        string // resolved region name (empty = no regions configured)
	BaseURL       string // override base URL for this target (empty = use adapter default)
	DataResidency string // residency tag for filtering (e.g., "eu", "us")
}

type ProviderKey struct {
	ID    string
	Value string
}

type StreamResult struct {
	Chunks <-chan openai.ChatCompletionStreamChunk
	Errors <-chan error
}

type Adapter interface {
	Name() string
	CompleteChat(ctx context.Context, req openai.ChatCompletionRequest, target Target, key ProviderKey) (*openai.ChatCompletionResponse, error)
	StreamChat(ctx context.Context, req openai.ChatCompletionRequest, target Target, key ProviderKey) (*StreamResult, error)
}

type MissingEnvKey struct {
	KeyID string
	Env   string
}

type KeyRing struct {
	mu         sync.Mutex
	next       uint64
	keys       []ProviderKey
	configured int
	missingEnv []MissingEnvKey
}

// NewKeyRing builds a KeyRing from provider config.
// kmsFetcher is optional; when non-nil, encrypted_value fields are decrypted.
func NewKeyRing(cfg config.ProviderConfig, kmsFetcher kms.KeyFetcher) *KeyRing {
	keys := make([]ProviderKey, 0, len(cfg.Keys))
	missingEnv := []MissingEnvKey{}
	for _, item := range cfg.Keys {
		value := item.Value
		if value == "" && item.EncryptedValue != "" {
			if kmsFetcher == nil {
				// encrypted_value present but no KMS — skip and report
				missingEnv = append(missingEnv, MissingEnvKey{KeyID: item.ID, Env: "encrypted_value (no KMS configured)"})
				continue
			}
			decrypted, err := kms.DecryptProviderKey(item.EncryptedValue, kmsFetcher)
			if err != nil {
				missingEnv = append(missingEnv, MissingEnvKey{KeyID: item.ID, Env: "encrypted_value (decryption failed)"})
				continue
			}
			value = decrypted
		}
		if value == "" && item.Env != "" {
			value = os.Getenv(item.Env)
			if value == "" {
				missingEnv = append(missingEnv, MissingEnvKey{KeyID: item.ID, Env: item.Env})
			}
		}
		if value == "" {
			continue
		}
		keys = append(keys, ProviderKey{ID: item.ID, Value: value})
	}
	return &KeyRing{keys: keys, configured: len(cfg.Keys), missingEnv: missingEnv}
}

func (r *KeyRing) Next() (ProviderKey, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.keys) == 0 {
		return ProviderKey{}, false
	}
	key := r.keys[int(r.next%uint64(len(r.keys)))]
	r.next++
	return key, true
}

func (r *KeyRing) ActiveCount() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.keys)
}

func (r *KeyRing) ConfiguredCount() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.configured
}

func (r *KeyRing) MissingEnv() []MissingEnvKey {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]MissingEnvKey, len(r.missingEnv))
	copy(out, r.missingEnv)
	return out
}

type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return http.StatusText(e.StatusCode) + ": " + e.Body
}

func IsRetryable(err error) bool {
	if errors.Is(err, ErrRetryable) {
		return true
	}
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode == http.StatusTooManyRequests || httpErr.StatusCode >= 500
	}
	return false
}
