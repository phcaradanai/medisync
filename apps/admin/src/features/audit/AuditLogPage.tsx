import { useCallback, useEffect, useMemo, useState } from "react";
import { create } from "@bufbuild/protobuf";
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { ListAuditLogsRequestSchema } from "@medisync/proto/medisync/audit/v1/audit_pb";
import type { AuditEntry } from "@medisync/proto/medisync/audit/v1/audit_pb";
import { CursorPaginationSchema } from "@medisync/proto/medisync/common/v1/pagination_pb";
import { auditClient } from "../../api/client";
import { Icon } from "../masterdata/icons";
import {
  MasterHeader, ListHeading, SearchInput, MasterTable, type Column,
} from "../masterdata/kit";

function formatDate(ts: Timestamp | undefined): string {
  if (!ts) return "-";
  const ms = Number(ts.seconds) * 1000 + (ts.nanos || 0) / 1_000_000;
  const date = new Date(ms);
  if (Number.isNaN(date.getTime())) return "-";
  return date.toLocaleString("th-TH", {
    dateStyle: "medium",
    timeStyle: "medium",
  });
}

export function AuditLogPage() {
  const [entries, setEntries] = useState<AuditEntry[]>([]);
  const [query, setQuery] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const req = create(ListAuditLogsRequestSchema, {
        projectId: "",
        pagination: create(CursorPaginationSchema, {
          pageSize: 500,
          pageToken: "",
        }),
      });
      const response = await auditClient.listAuditLogs(req);
      setEntries(response.entries);
    } catch (e) {
      setError(e instanceof Error ? e.message : "โหลด Audit Log ไม่สำเร็จ");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return entries;
    return entries.filter((e) =>
      e.actor.toLowerCase().includes(q) ||
      e.action.toLowerCase().includes(q) ||
      e.entity.toLowerCase().includes(q) ||
      e.entityId.toLowerCase().includes(q) ||
      e.projectId.toLowerCase().includes(q) ||
      e.detail.toLowerCase().includes(q)
    );
  }, [entries, query]);

  const columns: Column<AuditEntry>[] = [
    {
      key: "created_at",
      header: "เวลา",
      render: (e) => <span className='md-cell-muted'>{formatDate(e.createdAt)}</span>,
    },
    { key: "actor", header: "ผู้ทำรายการ", render: (e) => e.actor || "system" },
    {
      key: "action",
      header: "Action",
      render: (e) => <span className='md-badge md-badge-info'>{e.action}</span>,
    },
    { key: "entity", header: "Entity", render: (e) => e.entity },
    { key: "entity_id", header: "Entity ID", render: (e) => <span className='md-code'>{e.entityId}</span> },
    { key: "project", header: "Project", render: (e) => e.projectId || "SYSADMIN" },
    {
      key: "detail",
      header: "รายละเอียด",
      render: (e) => <code>{e.detail}</code>,
    },
  ];

  return (
    <div>
      <MasterHeader
        icon={Icon.database}
        title='Audit Log'
        subtitle='ประวัติการเปลี่ยนแปลงข้อมูล ระยะที่ 1'
      >
        <button
          className='md-btn md-btn-ghost'
          onClick={() => void load()}
          disabled={loading}
        >
          <Icon.undo size={18} /> รีเฟรช
        </button>
      </MasterHeader>
      {error && <div className='md-err'>{error}</div>}

      <div className='md-panel'>
        <ListHeading icon={Icon.database} title='รายการประวัติการใช้งาน' count={filtered.length} />
        <div className='md-toolbar'>
          <SearchInput
            value={query}
            onChange={setQuery}
            placeholder='ค้นหาผู้ทำรายการ Action Entity หรือรายละเอียด'
          />
        </div>
        <MasterTable
          rows={filtered}
          columns={columns}
          getId={(e) => e.id}
          loading={loading}
          emptyText='ยังไม่มีประวัติการใช้งาน'
        />
      </div>
    </div>
  );
}