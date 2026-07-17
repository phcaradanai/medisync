import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type FormEvent,
} from "react";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError, createClient } from "@connectrpc/connect";
import {
  CatalogService,
  GetByBarcodeRequestSchema,
  type Drug,
} from "@medisync/proto/medisync/catalog/v1/catalog_pb";
import { transport } from "../../transport.ts";

export interface BarcodeScanDialogProps {
  open: boolean;
  onClose: () => void;
  onConfirm: (drug: Drug, barcode: string) => void;
  lookupDrug?: (barcode: string) => Promise<Drug | null>;
}

type ScanState = "idle" | "matched" | "not-found";

const catalogClient = createClient(CatalogService, transport);

async function lookupDrugByBarcode(barcode: string): Promise<Drug | null> {
  const response = await catalogClient.getByBarcode(
    create(GetByBarcodeRequestSchema, { barcode }),
  );
  return response.drug ?? null;
}

function BarcodeGlyph() {
  return (
    <svg
      className="barcode-dialog__glyph"
      viewBox="0 0 64 48"
      aria-hidden="true"
    >
      <path
        fill="currentColor"
        d="M4 4h3v40H4V4Zm6 0h2v40h-2V4Zm5 0h5v40h-5V4Zm8 0h2v40h-2V4Zm6 0h4v40h-4V4Zm7 0h2v40h-2V4Zm6 0h6v40h-6V4Zm10 0h2v40h-2V4Zm6 0h3v40h-3V4Z"
      />
    </svg>
  );
}

export default function BarcodeScanDialog({
  open,
  onClose,
  onConfirm,
  lookupDrug = lookupDrugByBarcode,
}: BarcodeScanDialogProps) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const scannerBufferRef = useRef("");
  const scannerTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const lookupSequenceRef = useRef(0);
  const [scanState, setScanState] = useState<ScanState>("idle");
  const [matchedDrug, setMatchedDrug] = useState<Drug | null>(null);
  const [barcode, setBarcode] = useState("");
  const [manualBarcode, setManualBarcode] = useState("");
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);

  const resetScannerBuffer = useCallback(() => {
    scannerBufferRef.current = "";
    if (scannerTimerRef.current) {
      clearTimeout(scannerTimerRef.current);
      scannerTimerRef.current = null;
    }
  }, []);

  const resetDialog = useCallback(() => {
    lookupSequenceRef.current += 1;
    resetScannerBuffer();
    setScanState("idle");
    setMatchedDrug(null);
    setBarcode("");
    setManualBarcode("");
    setMessage("");
    setBusy(false);
  }, [resetScannerBuffer]);

  const submitBarcode = useCallback(
    async (rawBarcode: string) => {
      const normalizedBarcode = rawBarcode.trim();
      if (!normalizedBarcode || busy) return;

      const sequence = ++lookupSequenceRef.current;
      resetScannerBuffer();
      setBarcode(normalizedBarcode);
      setManualBarcode(normalizedBarcode);
      setMessage("");
      setBusy(true);

      try {
        const drug = await lookupDrug(normalizedBarcode);
        if (sequence !== lookupSequenceRef.current) return;

        if (!drug) {
          setMatchedDrug(null);
          setScanState("not-found");
          setMessage("ไม่พบบาร์โค้ดนี้ในรายการยา");
          return;
        }

        setMatchedDrug(drug);
        setScanState("matched");
      } catch (error: unknown) {
        if (sequence !== lookupSequenceRef.current) return;

        const connectError = ConnectError.from(error);
        setMatchedDrug(null);
        setScanState("not-found");
        setMessage(
          connectError.code === Code.NotFound
            ? "ไม่พบบาร์โค้ดนี้ในรายการยา"
            : "ไม่สามารถตรวจสอบบาร์โค้ดได้ กรุณาลองอีกครั้ง",
        );
      } finally {
        if (sequence === lookupSequenceRef.current) setBusy(false);
      }
    },
    [busy, lookupDrug, resetScannerBuffer],
  );

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;

    if (open) {
      resetDialog();
      if (!dialog.open) {
        if (typeof dialog.showModal === "function") dialog.showModal();
        else dialog.setAttribute("open", "");
      }
    } else if (dialog.open) {
      if (typeof dialog.close === "function") dialog.close();
      else dialog.removeAttribute("open");
    }
  }, [open, resetDialog]);

  useEffect(() => {
    if (!open) return;

    const handleScannerKey = (event: KeyboardEvent) => {
      const target = event.target;
      if (
        target instanceof HTMLInputElement ||
        target instanceof HTMLTextAreaElement ||
        (target instanceof HTMLElement && target.isContentEditable)
      ) {
        return;
      }

      if (event.key === "Enter") {
        const scannedBarcode = scannerBufferRef.current;
        resetScannerBuffer();
        if (scannedBarcode) {
          event.preventDefault();
          void submitBarcode(scannedBarcode);
        }
        return;
      }

      if (
        event.key.length !== 1 ||
        event.ctrlKey ||
        event.altKey ||
        event.metaKey
      ) {
        return;
      }

      event.preventDefault();
      scannerBufferRef.current += event.key;
      if (scannerTimerRef.current) clearTimeout(scannerTimerRef.current);
      scannerTimerRef.current = setTimeout(resetScannerBuffer, 150);
    };

    window.addEventListener("keydown", handleScannerKey);
    return () => {
      window.removeEventListener("keydown", handleScannerKey);
      resetScannerBuffer();
    };
  }, [open, resetScannerBuffer, submitBarcode]);

  const handleCancel = (event: React.SyntheticEvent<HTMLDialogElement>) => {
    event.preventDefault();
    onClose();
  };

  const handleManualSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    void submitBarcode(manualBarcode);
  };

  const handleRescan = () => {
    resetDialog();
    dialogRef.current?.focus();
  };

  const handleConfirm = () => {
    if (!matchedDrug) return;
    onConfirm(matchedDrug, barcode);
    onClose();
  };

  return (
    <dialog
      ref={dialogRef}
      className="barcode-dialog"
      aria-labelledby="barcode-dialog-title"
      onCancel={handleCancel}
      tabIndex={-1}
    >
      <div className="barcode-dialog__panel">
        {scanState === "idle" && (
          <>
            <h2 id="barcode-dialog-title" className="barcode-dialog__title">
              สแกนบาร์โค้ดยา
            </h2>
            <div className="card-scan-area" aria-live="polite">
              {busy ? <div className="spinner" /> : <BarcodeGlyph />}
              <p>
                {busy
                  ? "กำลังตรวจสอบบาร์โค้ด..."
                  : "นำเครื่องสแกนไปที่บาร์โค้ด แล้วกดไกสแกน"}
              </p>
            </div>
          </>
        )}

        {scanState === "matched" && matchedDrug && (
          <>
            <div className="barcode-dialog__matched">
              <span className="status-badge status-badge--info">✓ พบยา</span>
              <h2 id="barcode-dialog-title" className="barcode-dialog__title">
                {matchedDrug.displayName || matchedDrug.name}
              </h2>
              <span className="barcode-dialog__mono">{barcode}</span>
              {matchedDrug.code && (
                <span className="barcode-dialog__drug-code">
                  {matchedDrug.code}
                </span>
              )}
            </div>
            <div className="barcode-dialog__actions">
              <button
                type="button"
                className="kiosk-btn kiosk-btn-outline"
                onClick={onClose}
              >
                ยกเลิก
              </button>
              <button
                type="button"
                className="kiosk-btn kiosk-btn-primary"
                onClick={handleConfirm}
              >
                ยืนยันยานี้
              </button>
            </div>
          </>
        )}

        {scanState === "not-found" && (
          <>
            <h2 id="barcode-dialog-title" className="barcode-dialog__title">
              ไม่พบบาร์โค้ด
            </h2>
            <div className="barcode-dialog__error" role="alert">
              <span aria-hidden="true">⊘</span>
              <span>{message}</span>
              {barcode && <span className="barcode-dialog__mono">{barcode}</span>}
            </div>
            <form
              className="barcode-dialog__manual"
              onSubmit={handleManualSubmit}
            >
              <label className="kiosk-label" htmlFor="manual-barcode">
                กรอกรหัสบาร์โค้ดเอง
              </label>
              <div className="barcode-dialog__manual-row">
                <input
                  id="manual-barcode"
                  className="kiosk-input barcode-dialog__manual-input"
                  value={manualBarcode}
                  onChange={(event) => setManualBarcode(event.target.value)}
                  inputMode="text"
                  autoComplete="off"
                  spellCheck={false}
                />
                <button
                  type="submit"
                  className="kiosk-btn kiosk-btn-outline"
                  disabled={!manualBarcode.trim() || busy}
                >
                  {busy ? "กำลังค้นหา..." : "ค้นหา"}
                </button>
              </div>
            </form>
            <div className="barcode-dialog__actions">
              <button
                type="button"
                className="kiosk-btn kiosk-btn-outline"
                onClick={onClose}
              >
                ปิด
              </button>
              <button
                type="button"
                className="kiosk-btn kiosk-btn-primary"
                onClick={handleRescan}
              >
                สแกนใหม่
              </button>
            </div>
          </>
        )}

        {scanState === "idle" && (
          <button
            type="button"
            className="kiosk-btn kiosk-btn-outline barcode-dialog__dismiss"
            onClick={onClose}
          >
            ปิด
          </button>
        )}
      </div>
    </dialog>
  );
}
