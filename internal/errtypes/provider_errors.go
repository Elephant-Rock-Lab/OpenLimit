package errtypes

import "fmt"

// EnrichProviderError maps an HTTP status code and provider name to an
// actionable, human-readable error message. It returns a string (HB-01).
//
// The caller wraps the result into whatever error type is appropriate
// (AUTH-01).
func EnrichProviderError(provider string, statusCode int, rawBody string) string {
	switch {
	case statusCode == 401:
		return fmt.Sprintf("Authentication failed with %s. Check your API key.", provider)
	case statusCode == 402:
		return fmt.Sprintf("%s account requires payment or has an overdue balance.", provider)
	case statusCode == 403:
		return fmt.Sprintf("%s denied the request. Check your account permissions or model access.", provider)
	case statusCode == 429:
		return fmt.Sprintf("%s is rate-limiting requests. The gateway will retry automatically. Consider adding a fallback model.", provider)
	case statusCode >= 500:
		return fmt.Sprintf("%s experienced an internal error (HTTP %d). The gateway will retry if possible.", provider, statusCode)
	case statusCode > 0:
		return fmt.Sprintf("%s returned an unexpected error (HTTP %d). Check the provider status page.", provider, statusCode)
	default:
		if rawBody != "" {
			return fmt.Sprintf("%s request failed: %s", provider, truncate(rawBody, 200))
		}
		return fmt.Sprintf("%s request failed. Check provider connectivity.", provider)
	}
}

// truncate shortens a string to maxLen bytes, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
