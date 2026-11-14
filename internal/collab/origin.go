package collab

import "context"

type originKey struct{}

// WithOrigin marks a context with the connection that sends an update, so that the update is not echoed to it.
func WithOrigin(ctx context.Context, connection uint64) context.Context {
	return context.WithValue(ctx, originKey{}, connection)
}

// OriginFrom returns the connection set by WithOrigin, or zero for requests that do not come from a connection.
func OriginFrom(ctx context.Context) uint64 {
	connection, _ := ctx.Value(originKey{}).(uint64)

	return connection
}
