package tracing

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"connectrpc.com/connect"
	commonv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/common/v1"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestInterceptorAddsRequestIDToSuccess(t *testing.T) {
	interceptor := NewInterceptor(slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
	wrapped := interceptor.WrapUnary(func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&emptypb.Empty{}), nil
	})

	response, err := wrapped(context.Background(), connect.NewRequest(&emptypb.Empty{}))
	if err != nil {
		t.Fatalf("wrapped: %v", err)
	}
	if _, err := uuid.Parse(response.Header().Get(HeaderRequestID)); err != nil {
		t.Fatalf("X-Request-Id is not a UUID: %v", err)
	}
}

func TestInterceptorTracesAndLogsErrors(t *testing.T) {
	var logs bytes.Buffer
	interceptor := NewInterceptor(slog.New(slog.NewJSONHandler(&logs, nil)))
	wrapped := interceptor.WrapUnary(func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("bad request"))
	})

	_, err := wrapped(context.Background(), connect.NewRequest(&emptypb.Empty{}))
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) {
		t.Fatalf("error type = %T, want *connect.Error", err)
	}
	requestID := connectErr.Meta().Get(HeaderRequestID)
	if _, err := uuid.Parse(requestID); err != nil {
		t.Fatalf("X-Request-Id is not a UUID: %v", err)
	}

	if len(connectErr.Details()) != 1 {
		t.Fatalf("details = %d, want 1", len(connectErr.Details()))
	}
	value, err := connectErr.Details()[0].Value()
	if err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	trace, ok := value.(*commonv1.ErrorTrace)
	if !ok {
		t.Fatalf("detail type = %T, want *commonv1.ErrorTrace", value)
	}
	if _, err := uuid.Parse(trace.TraceId); err != nil {
		t.Fatalf("trace_id is not a UUID: %v", err)
	}

	logText := logs.String()
	for _, want := range []string{
		"\"request_id\":\"" + requestID + "\"",
		"\"trace_id\":\"" + trace.TraceId + "\"",
		"\"endpoint\":\"\"",
		"\"error_code\":\"invalid_argument\"",
		"\"error_message\":\"bad request\"",
	} {
		if !strings.Contains(logText, want) {
			t.Errorf("log missing %s: %s", want, logText)
		}
	}
}

func TestInterceptorWrapsNonConnectErrors(t *testing.T) {
	interceptor := NewInterceptor(slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
	wrapped := interceptor.WrapUnary(func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
		return nil, errors.New("boom")
	})

	_, err := wrapped(context.Background(), connect.NewRequest(&emptypb.Empty{}))
	if got := connect.CodeOf(err); got != connect.CodeInternal {
		t.Fatalf("code = %v, want %v", got, connect.CodeInternal)
	}
}
