package requestid

import "context"

type contextKey string

const key contextKey = "request_id"

func WithContext(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, key, requestID)
}

func FromContext(ctx context.Context) string {
	requestID, _ := ctx.Value(key).(string)
	return requestID
}
