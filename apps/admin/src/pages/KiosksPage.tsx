/**
 * Kiosks page: admin CRUD for provisioned kiosk terminals.
 * Thai-first, accessible. PIN is revealed only once per create/reset.
 */
import { useState, useEffect, useCallback, type FormEvent } from "react";
import { kioskClient } from "../api/client";
import type { Kiosk } from "@medisync/proto/medisync/kiosk/v1/kiosk_pb";

export function KiosksPage() {
  const [kiosks, setKiosks] = useState<Kiosk[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Create form
  const [showCreate, setShowCreate] = useState(false);
  const [createCode, setCreateCode] = useState("");
  const [createName, setCreateName] = useState("");
  const [createPin, setCreatePin] = useState("");
  const [createBusy, setCreateBusy] = useState(false);
  const [createError, setCreateError] = useState<string | null>(null);

  // One-time PIN reveal after create/reset
  const [revealedPin, setRevealedPin] = useState<string | null>(null);
  const [revealedFor, setRevealedFor] = useState<string>("");

  const fetchKiosks = useCallback(async () => {
    try {
      const res = await kioskClient.listKiosks({});
      setKiosks(res.kiosks ?? []);
      setError(null);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "โหลดข้อมูลไม่สำเร็จ");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchKiosks();
  }, [fetchKiosks]);

  const handleCreate = async (e: FormEvent) => {
    e.preventDefault();
    if (!createCode.trim() || !createPin.trim()) {
      setCreateError("กรุณากรอกรหัสเครื่องและ PIN");
      return;
    }
    setCreateError(null);
    setCreateBusy(true);
    try {
      const res = await kioskClient.createKiosk({
        code: createCode.trim(),
        displayName: createName.trim(),
        pin: createPin,
      });
      if (res.kiosk?.pin) {
        setRevealedPin(res.kiosk.pin);
        setRevealedFor(res.kiosk.displayName || res.kiosk.code);
      }
      setShowCreate(false);
      setCreateCode("");
      setCreateName("");
      setCreatePin("");
      fetchKiosks();
    } catch (e: unknown) {
      setCreateError(
        e instanceof Error ? e.message : "สร้างเครื่องไม่สำเร็จ",
      );
    } finally {
      setCreateBusy(false);
    }
  };

  const handleToggleActive = async (k: Kiosk) => {
    try {
      await kioskClient.updateKiosk({ id: k.id, active: !k.active });
      fetchKiosks();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "อัปเดตสถานะไม่สำเร็จ");
    }
  };

  const handleResetPin = async (k: Kiosk) => {
    const newPin = prompt(
      `กรุณากรอก PIN ใหม่สำหรับเครื่อง "${k.displayName || k.code}":`,
    );
    if (!newPin || !newPin.trim()) return;
    try {
      const res = await kioskClient.resetKioskPin({
        id: k.id,
        newPin: newPin.trim(),
      });
      if (res.kiosk?.pin) {
        setRevealedPin(res.kiosk.pin);
        setRevealedFor(k.displayName || k.code);
      }
      fetchKiosks();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "รีเซ็ต PIN ไม่สำเร็จ");
    }
  };

  if (loading) {
    return (
      <div>
        <div className="page-header">
          <h1>เครื่องจ่ายยา (Kiosk)</h1>
        </div>
        <div className="table-wrap">
          <div className="empty-state">กำลังโหลด...</div>
        </div>
      </div>
    );
  }

  return (
    <div>
      <div className="page-header">
        <h1>เครื่องจ่ายยา (Kiosk)</h1>
        <button
          type="button"
          className="btn btn-primary"
          onClick={() => setShowCreate((v) => !v)}
        >
          {showCreate ? "ยกเลิก" : "+ เพิ่มเครื่อง"}
        </button>
      </div>

      {/* One-time PIN reveal banner */}
      {revealedPin && (
        <div
          className="status-bar status-bar--info"
          role="alert"
          style={{
            padding: "var(--space-lg)",
            marginBottom: "var(--space-lg)",
            background: "var(--color-neutral-subtle, #f0f0f0)",
            borderRadius: "var(--rounded-md, 6px)",
            border: "1px solid var(--color-semantic-info, #89b4fa)",
            display: "flex",
            justifyContent: "space-between",
            alignItems: "center",
            flexWrap: "wrap",
            gap: "var(--space-md)",
          }}
        >
          <span>
            <strong>PIN สำหรับ {revealedFor}:</strong>{" "}
            <code style={{ fontSize: "1.2rem", fontWeight: 700 }}>
              {revealedPin}
            </code>{" "}
            — ข้อมูลนี้จะไม่ถูกแสดงซ้ำอีก กรุณาบันทึกไว้
          </span>
          <button
            type="button"
            className="btn btn-outline"
            onClick={() => {
              setRevealedPin(null);
              setRevealedFor("");
            }}
          >
            ปิด
          </button>
        </div>
      )}

      {/* Create form */}
      {showCreate && (
        <form
          onSubmit={handleCreate}
          style={{
            background: "var(--color-neutral-subtle, #f5f5f5)",
            padding: "var(--space-xl)",
            borderRadius: "var(--rounded-md, 6px)",
            marginBottom: "var(--space-xl)",
            display: "flex",
            flexDirection: "column",
            gap: "var(--space-md)",
          }}
        >
          <h2 style={{ margin: 0, fontSize: "1.1rem" }}>เพิ่มเครื่องใหม่</h2>
          {createError && (
            <div className="form-error" role="alert">
              {createError}
            </div>
          )}
          <div style={{ display: "flex", gap: "var(--space-md)", flexWrap: "wrap" }}>
            <label style={{ flex: 1, minWidth: "200px" }}>
              <span className="label">รหัสเครื่อง (Code)</span>
              <input
                className="input"
                type="text"
                value={createCode}
                onChange={(e) => setCreateCode(e.target.value)}
                placeholder="เช่น KIOSK-WARD3A"
                disabled={createBusy}
                autoFocus
              />
            </label>
            <label style={{ flex: 1, minWidth: "200px" }}>
              <span className="label">ชื่อเครื่อง</span>
              <input
                className="input"
                type="text"
                value={createName}
                onChange={(e) => setCreateName(e.target.value)}
                placeholder="เช่น ตู้จ่ายยาวอร์ด 3A"
                disabled={createBusy}
              />
            </label>
            <label style={{ flex: 1, minWidth: "200px" }}>
              <span className="label">PIN</span>
              <input
                className="input"
                type="password"
                value={createPin}
                onChange={(e) => setCreatePin(e.target.value)}
                placeholder="PIN 4-6 หลัก"
                disabled={createBusy}
              />
            </label>
          </div>
          <button
            type="submit"
            className="btn btn-primary"
            disabled={createBusy}
            style={{ alignSelf: "flex-start" }}
          >
            {createBusy ? "กำลังสร้าง..." : "สร้างเครื่อง"}
          </button>
        </form>
      )}

      {/* Error banner */}
      {error && (
        <div className="form-error" role="alert">
          {error}
        </div>
      )}

      {/* Kiosks table */}
      <div className="table-wrap">
        {kiosks.length === 0 ? (
          <div className="empty-state">ยังไม่มีเครื่องจ่ายยา</div>
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th>รหัสเครื่อง</th>
                <th>ชื่อเครื่อง</th>
                <th>สถานะ</th>
                <th>สร้างเมื่อ</th>
                <th>จัดการ</th>
              </tr>
            </thead>
            <tbody>
              {kiosks.map((k) => (
                <tr key={k.id}>
                  <td>
                    <code>{k.code}</code>
                  </td>
                  <td>{k.displayName}</td>
                  <td>
                    <span
                      className={`status-badge ${k.active ? "status-badge--active" : "status-badge--inactive"}`}
                    >
                      {k.active ? "พร้อมใช้งาน" : "ปิดใช้งาน"}
                    </span>
                  </td>
                  <td>
                    {k.createdAt
                      ? new Date(
                          Number(k.createdAt.seconds) * 1000,
                        ).toLocaleDateString("th-TH")
                      : "—"}
                  </td>
                  <td>
                    <div
                      style={{
                        display: "flex",
                        gap: "var(--space-sm)",
                        flexWrap: "wrap",
                      }}
                    >
                      <button
                        type="button"
                        className="btn btn-outline btn-sm"
                        onClick={() => handleToggleActive(k)}
                      >
                        {k.active ? "ปิดใช้งาน" : "เปิดใช้งาน"}
                      </button>
                      <button
                        type="button"
                        className="btn btn-outline btn-sm"
                        onClick={() => handleResetPin(k)}
                      >
                        รีเซ็ต PIN
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
