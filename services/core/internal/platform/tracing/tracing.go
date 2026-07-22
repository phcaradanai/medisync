// Package tracing correlates Connect RPC responses, error bodies, and logs.
package tracing

import (
	"context"
	"errors"
	"log/slog"

	"connectrpc.com/connect"
	commonv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/common/v1"
	"github.com/google/uuid"
)

const HeaderRequestID = "X-Request-Id"

// NewInterceptor returns a unary interceptor that adds request IDs to all
// responses and trace IDs to all RPC errors.
func NewInterceptor(log *slog.Logger) connect.Interceptor {
	if log == nil {
		log = slog.Default()
	}
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			requestID := uuid.NewString()
			response, err := next(ctx, req)
			if err == nil {
				response.Header().Set(HeaderRequestID, requestID)
				return response, nil
			}

			traceID := uuid.NewString()
			var connectErr *connect.Error
			if !errors.As(err, &connectErr) {
				connectErr = connect.NewError(connect.CodeInternal, err)
			}
			connectErr.Meta().Set(HeaderRequestID, requestID)

			if detail, detailErr := connect.NewErrorDetail(&commonv1.ErrorTrace{TraceId: traceID}); detailErr == nil {
				connectErr.AddDetail(detail)
			}

			log.ErrorContext(ctx, "rpc error",
				slog.String("request_id", requestID),
				slog.String("trace_id", traceID),
				slog.String("endpoint", req.Spec().Procedure),
				slog.String("error_code", connectErr.Code().String()),
				slog.String("error_message", connectErr.Message()),
			)
			return nil, connectErr
		}
	})
}
