package db

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "synapse/db"

// TraceDBQuery wraps a database operation in an OTel span. The span is created
// with the given operation name and linked to the parent request trace via ctx.
// If ctx does not contain an active span, a new root span is created.
//
// Usage:
//
//	err := TraceDBQuery(ctx, "SELECT users", func(ctx context.Context) error {
//	    return db.QueryRowContext(ctx, ...)
//	})
func TraceDBQuery(ctx context.Context, operation string, dbFunc func(context.Context) error) error {
	tracer := otel.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, operation,
		trace.WithAttributes(
			attribute.String("db.operation", operation),
			attribute.String("db.system", "sqlite"),
		),
	)
	defer span.End()

	if err := dbFunc(ctx); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

// CountAdminUsersCtx wraps CountAdminUsers with an OTel tracing span linked
// to the parent request context.
func (db *DB) CountAdminUsersCtx(ctx context.Context) (int, error) {
	var count int
	err := TraceDBQuery(ctx, "CountAdminUsers", func(ctx context.Context) error {
		var err error
		count, err = db.CountAdminUsers()
		return err
	})
	return count, err
}

// GetAdminUserCtx wraps GetAdminUser with an OTel tracing span linked to the
// parent request context.
func (db *DB) GetAdminUserCtx(ctx context.Context, username string) (*AdminUser, error) {
	var user *AdminUser
	err := TraceDBQuery(ctx, "GetAdminUser", func(ctx context.Context) error {
		var err error
		user, err = db.GetAdminUser(username)
		return err
	})
	return user, err
}
