package audit

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/types/known/timestamppb"

	auditv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/audit/v1"
	auditv1connect "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/audit/v1/auditv1connect"
)

// Compile-time check: AuditServer implements the generated handler interface.
var _ auditv1connect.AuditServiceHandler = (*AuditServer)(nil)

// AuditServer is the Connect-RPC handler for AuditService.
type AuditServer struct {
	pool *pgxpool.Pool
}

// NewAuditServer creates a new AuditServer.
func NewAuditServer(pool *pgxpool.Pool) *AuditServer {
	return &AuditServer{pool: pool}
}

// ListAuditLogs returns paginated audit log entries.
func (s *AuditServer) ListAuditLogs(ctx context.Context, req *connect.Request[auditv1.ListAuditLogsRequest]) (*connect.Response[auditv1.ListAuditLogsResponse], error) {
	msg := req.Msg

	projectID := msg.GetProjectId()

	pageSize := int32(50)
	if msg.GetPagination() != nil {
		pageSize = msg.GetPagination().GetPageSize()
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 50
	}
	pageToken := ""
	if msg.GetPagination() != nil {
		pageToken = msg.GetPagination().GetPageToken()
	}

	entries, total, nextToken, err := List(ctx, s.pool, projectID, pageSize, pageToken)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list audit logs: %w", err))
	}

	pbEntries := make([]*auditv1.AuditEntry, len(entries))
	for i, e := range entries {
		pbEntries[i] = toProtoEntry(e)
	}

	return connect.NewResponse(&auditv1.ListAuditLogsResponse{
		Entries:       pbEntries,
		TotalCount:    total,
		NextPageToken: nextToken,
	}), nil
}

func toProtoEntry(e AuditEntry) *auditv1.AuditEntry {
	createdAt, err := time.Parse("2006-01-02T15:04:05Z07:00", e.CreatedAt)
	if err != nil {
		createdAt = time.Now()
	}
	return &auditv1.AuditEntry{
		Id:        e.ID,
		TraceId:   e.TraceID,
		Actor:     e.Actor,
		Action:    e.Action,
		Entity:    e.Entity,
		EntityId:  e.EntityID,
		ProjectId: e.ProjectID,
		Detail:    e.Detail,
		CreatedAt: timestamppb.New(createdAt),
	}
}
