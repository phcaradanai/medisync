# Demo Seed Data

ข้อมูล demo สำหรับทดสอบ flow จริงตั้งแต่ Sticker → ยืนยันพนักงาน → จ่ายยา → รายงาน
transaction โดยใช้ immutable business code แทน kiosk UUID

## เริ่มระบบ

จาก `D:\Projects\adm-chura3inter`:

<<<<<<< HEAD
# 2. Seed demo data
cd services/core
DATABASE_URL="postgres://postgres:dev%214555%402026@122.155.164.15:5434/medisync?sslmode=disable" \
  go run ./cmd/demoseed

# 3. Start the core (or rebuild the container)
npm run core
=======
```powershell
# ทดสอบโดยไม่ยิง hardware จริง
$env:FULFILLMENT_FAKE="true"
$env:VENDING_FAKE="true"
docker compose up -d --build --wait
>>>>>>> main
```

Compose รัน `medisync-demo-seed` ให้อัตโนมัติก่อนเปิด Tester หากรัน core นอก Docker
สามารถ seed ซ้ำแบบ idempotent ได้จาก `medisync\services\core`:

<<<<<<< HEAD
```bash
# Seed (idempotent — safe to re-run)
cd services/core
go run ./cmd/demoseed

# Reset and re-seed (clears demo data first)
go run ./cmd/demoseed --reset

# Smoke test (requires running core + postgres)
DATABASE_URL="postgres://postgres:dev%214555%402026@122.155.164.15:5434/medisync?sslmode=disable" \
  bash scripts/test/smoke_demo_seed.sh
=======
```powershell
$env:DATABASE_URL="postgres://medisync:medisync@localhost:5432/medisync?sslmode=disable"
go run .\cmd\demoseed
>>>>>>> main
```

## Credentials

ใช้เฉพาะ local development เท่านั้น:

| ประเภท | Username / Code | Password / PIN | สิทธิ์ |
|---|---|---|---|
| Staff | `admin` | `medisync-local-admin-2026` | ADMIN |
| Staff | `pharmacist` | `demo-pharmacist-2026` | PHARMACIST / WARD-3A |
| Staff | `nurse` | `demo-nurse-2026` | NURSE / WARD-3A |
| Staff | `refiller` | `demo-refiller-2026` | REFILLER / WARD-3A |
| Kiosk | `00010001` | `123456` | project `0001` |

Kiosk code สร้างโดยฐานข้อมูลจาก `project code + sequence ภายใน project` ดังนั้น project
`0001` จะได้ `00010001`, `00010002`, ... และ project `0002` จะเริ่มที่ `00020001`
code แก้ภายหลังไม่ได้

## URLs จาก Compose หลัก

- Kiosk UI: <http://localhost:5175>
- Admin UI: <http://localhost:5176>
- Kiosk Tester: <http://localhost:8899>
- Core API: <http://localhost:8080>

Kiosk UI เปิดเองได้ ไม่ต้องเปิดผ่านปุ่ม Tester ให้ login ด้วย `00010001` / `123456`;
Tester จะส่ง scan event เฉพาะ browser ที่เชื่อมด้วย code เดียวกัน

## ข้อมูลที่ seed

| Slot | Drug | Quantity | Hardware address |
|---|---|---:|---|
| `S01` | `DEMO-PARA500` | 80 | door 1 / layer 1 / channel 1 |
| `S02` | `DEMO-AMOX500` | 60 | door 1 / layer 2 / channel 1 |
| `S03` | `DEMO-OME20` | 45 | door 1 / layer 3 / channel 1 |

ทุก slot ใช้ `cabinet_id = 00010001` และมี FIFO batch/lot จริงสำหรับ reservation
มี prescription `DEMO-RX-001` สถานะ `READY` ของ WARD-3A

## ทดสอบ E2E โดยไม่ใช้ browser

จาก `medisync\services\core`:

```powershell
go run .\cmd\kiosktester -mode=flow -kiosk-code=00010001
```

คำสั่งนี้สร้าง prescription ที่มี `project_code=0001`, login ตู้ `00010001`, เรียก
`PrepareDispense`, login พนักงาน, เรียก `ConfirmDispense` และ poll จนได้
`DISPENSED`/`FAILED`

ดู transaction ที่ Admin เมนู `/dispense-transactions`; export CSV จะมี kiosk,
operator, drug, lot, slot, hardware address, hardware result และ timestamps

## Reset

```powershell
$env:DATABASE_URL="postgres://medisync:medisync@localhost:5432/medisync?sslmode=disable"
go run .\cmd\demoseed --reset
```

Reset ลบ dispense transactions ของข้อมูล demo ก่อน prescription/slot เพื่อรักษา foreign
keys แล้ว seed ข้อมูลกลับมาใหม่ ไม่ลบ admin bootstrap account

คู่มือ Tester แบบละเอียด: [`services/core/cmd/kiosktester/README.md`](../services/core/cmd/kiosktester/README.md)
