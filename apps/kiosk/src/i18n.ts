// i18n — simple translation system for MediSync.
// Thai is the default; English available via language toggle.
// Add new keys to the TH dictionary, then mirror in EN.

export type Lang = "th" | "en";

const TH: Record<string, string> = {
  // ── Shared ──
  "app.name": "MediSync",
  "logout": "ออกจากระบบ",
  "cancel": "ยกเลิก",
  "save": "บันทึก",
  "confirm": "ยืนยัน",
  "dismiss": "ปิด",
  "loading": "กำลังโหลด…",
  "error.generic": "เกิดข้อผิดพลาด กรุณาลองใหม่",
  "refresh": "รีเฟรช",

  // ── Kiosk: Login ──
  "kiosk.login": "ระบบเบิกจ่ายยา",
  "kiosk.code": "รหัสเครื่อง (KIOSK CODE)",
  "kiosk.pin": "PIN",
  "kiosk.signin": "เข้าสู่ระบบ",
  "kiosk.loginError": "รหัสเครื่องหรือ PIN ไม่ถูกต้อง",

  // ── Kiosk: Withdraw ──
  "kiosk.withdraw": "💊 เบิกยา",
  "kiosk.withdrawTitle": "รายการยาที่รอเบิก",
  "kiosk.noPrescriptions": "ไม่มีใบสั่งยาที่รอเบิกอยู่ในขณะนี้",
  "kiosk.ready": "พร้อมเบิก",
  "kiosk.dispensing": "กำลังจ่าย",
  "kiosk.dispensed": "จ่ายแล้ว",
  "kiosk.confirmDispense": "ยืนยันการเบิกยา",
  "kiosk.dispenseSuccess": "เบิกยาสำเร็จ",
  "kiosk.stepPrefix": "ขั้นตอน",

  // ── Kiosk: Refill ──
  "kiosk.refill": "📦 เติมยา",
  "kiosk.refillTitle": "เติมยาเข้าตู้",
  "kiosk.refillMode": "🔄 โหมดเติมยา",
  "kiosk.refillLow": "🔴 เหลือน้อย",
  "kiosk.refillAll": "📋 ทั้งหมด",
  "kiosk.refillEmpty": "ช่องว่าง",
  "kiosk.refillQty": "กรอกจำนวนที่เติม",
  "kiosk.refillConfirm": "เติม {qty} หน่วย",
  "kiosk.refillSuccess": "เติมยาสำเร็จ",
  "kiosk.refillContinue": "เติมต่อ",
  "kiosk.refillBack": "กลับ",

  // ── Kiosk: ShelfGrid ──
  "kiosk.catalog": "📋 แคตตาล็อก",
  "kiosk.shelf": "ชั้น",
  "kiosk.scanBarcode": "สแกนบาร์โค้ดยา",
  "kiosk.barcodeNotFound": "ไม่พบยาจากรหัสบาร์โค้ดนี้",
  "kiosk.barcodeRescan": "สแกนใหม่",
  "kiosk.manualEntry": "กรอกรหัสเอง",

  // ── Kiosk: Expiry ──
  "kiosk.expiring": "ใกล้หมดอายุ",
  "kiosk.expired": "หมดอายุ",

  // ── Admin: Shared ──
  "admin.dashboard": "Drug Catalog",
  "admin.projects": "Projects",
  "admin.drugs": "Drugs",
  "admin.inventory": "Inventory",
  "admin.users": "Users",
  "admin.kiosks": "Kiosks",
  "admin.cabinets": "Cabinets",
  "admin.signOut": "Sign Out",
  "admin.addDrug": "+ Add Drug",
  "admin.addCabinet": "+ Add Cabinet",
  "admin.addUser": "+ Add User",
  "admin.addKiosk": "+ Add Kiosk",
  "admin.createSlot": "+ Create Slot",
  "admin.create": "Create",
  "admin.edit": "Edit",
  "admin.deactivate": "Deactivate",
  "admin.activate": "Activate",
  "admin.search": "Search…",
  "admin.empty": "No data yet",
};

const EN: Record<string, string> = {
  // ── Shared ──
  "app.name": "MediSync",
  "logout": "Sign Out",
  "cancel": "Cancel",
  "save": "Save",
  "confirm": "Confirm",
  "dismiss": "Dismiss",
  "loading": "Loading…",
  "error.generic": "An error occurred. Please try again.",
  "refresh": "Refresh",

  // ── Kiosk: Login ──
  "kiosk.login": "Medication Dispenser",
  "kiosk.code": "KIOSK CODE",
  "kiosk.pin": "PIN",
  "kiosk.signin": "Sign In",
  "kiosk.loginError": "Invalid kiosk code or PIN",

  // ── Kiosk: Withdraw ──
  "kiosk.withdraw": "💊 Dispense",
  "kiosk.withdrawTitle": "Pending Prescriptions",
  "kiosk.noPrescriptions": "No pending prescriptions",
  "kiosk.ready": "Ready",
  "kiosk.dispensing": "Dispensing",
  "kiosk.dispensed": "Dispensed",
  "kiosk.confirmDispense": "Confirm Dispense",
  "kiosk.dispenseSuccess": "Dispense Successful",
  "kiosk.stepPrefix": "Step",

  // ── Kiosk: Refill ──
  "kiosk.refill": "📦 Refill",
  "kiosk.refillTitle": "Refill Cabinet",
  "kiosk.refillMode": "🔄 Refill Mode",
  "kiosk.refillLow": "🔴 Low Stock",
  "kiosk.refillAll": "📋 All Slots",
  "kiosk.refillEmpty": "Empty",
  "kiosk.refillQty": "Enter Quantity",
  "kiosk.refillConfirm": "Add {qty} units",
  "kiosk.refillSuccess": "Refill Complete",
  "kiosk.refillContinue": "Refill More",
  "kiosk.refillBack": "Back",

  // ── Kiosk: ShelfGrid ──
  "kiosk.catalog": "📋 Catalog",
  "kiosk.shelf": "Shelf",
  "kiosk.scanBarcode": "Scan Barcode",
  "kiosk.barcodeNotFound": "Drug not found for this barcode",
  "kiosk.barcodeRescan": "Scan Again",
  "kiosk.manualEntry": "Enter Manually",

  // ── Kiosk: Expiry ──
  "kiosk.expiring": "Expiring Soon",
  "kiosk.expired": "Expired",
};

const dictionaries: Record<Lang, Record<string, string>> = { th: TH, en: EN };

let currentLang: Lang = (localStorage.getItem("lang") as Lang) || "th";

export function t(key: string, params?: Record<string, string | number>): string {
  const dict = dictionaries[currentLang];
  let value = dict[key] || key;
  if (params) {
    for (const [k, v] of Object.entries(params)) {
      value = value.replace(`{${k}}`, String(v));
    }
  }
  return value;
}

export function getLanguage(): Lang {
  return currentLang;
}

export function setLanguage(lang: Lang) {
  currentLang = lang;
  localStorage.setItem("lang", lang);
}

export function toggleLanguage(): Lang {
  const next = currentLang === "th" ? "en" : "th";
  setLanguage(next);
  return next;
}

// Subscribe to language changes
const listeners = new Set<() => void>();
export function onLanguageChange(fn: () => void) {
  listeners.add(fn);
  return () => listeners.delete(fn);
}
export function notifyLanguageChange() {
  for (const fn of listeners) fn();
}
