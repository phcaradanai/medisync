# วิธีเปิด Kiosk Tester

`kiosktester` เป็นเครื่องมือสำหรับนักพัฒนา ใช้จำลอง flow ตั้งแต่สร้างรายการเบิกยา
ผ่าน NATS, สแกน sticker, ยืนยันตัวตนเจ้าหน้าที่ และติดตามสถานะการจ่ายยา

หน้าเว็บของ tester ไม่ใช่ไฟล์ HTML ที่เปิดตรง ๆ ต้องรันโปรแกรม Go เพื่อให้มี web
server และ API ของ tester ก่อน

## เปิดใช้งานแบบเร็ว

สิ่งที่ต้องมีในเครื่อง:

- Docker Desktop ที่กำลังทำงาน

Go 1.26 และ Node.js 20 ขึ้นไปจำเป็นเฉพาะกรณีรัน service แบบ local โดยไม่ใช้
Docker

เปิด PowerShell แล้วรันจาก parent workspace:

```powershell
Set-Location D:\Projects\adm-chura3inter

# เปิดทั้งระบบ, seed ข้อมูล demo และเปิด Kiosk Tester
docker compose up -d --build --wait
```

ตรวจ log เพื่อยืนยันว่า tester พร้อม:

```powershell
docker compose logs medisync-kiosktester
```

ให้เปิดเบราว์เซอร์ที่ <http://localhost:8899>

Compose หลักจะเปิด service `medisync-kiosktester` ที่พอร์ต `8899` พร้อมรัน
`medisync-demo-seed` แบบ idempotent ก่อน tester เริ่มทำงาน

เมื่อ container `medisync-demo-seed` ถูกสร้างใหม่ ระบบจะ refresh ข้อมูลที่ขึ้นต้นด้วย
`DEMO-` และจำนวนยาใน demo slots กลับเป็นค่าตั้งต้น

ตรวจสถานะได้ด้วย:

```powershell
docker compose ps
```

ถ้าแก้โค้ดของ tester หรือ Dockerfile ให้ rebuild ด้วย:

```powershell
docker compose up -d --build --wait medisync-kiosktester
```

ถ้าต้องการเปิดเฉพาะ stack ภายใน `medisync` ก็ยังใช้คำสั่งต่อไปนี้ได้ โดย Compose
ชุดนี้จะใช้ Kiosk URL `http://localhost:5173`:

```powershell
Set-Location D:\Projects\adm-chura3inter\medisync
npm run infra:up
```

## ค่า demo ที่ใช้

| รายการ | ค่า |
|---|---|
| Core API | `http://localhost:8080` |
| NATS | `nats://localhost:4222` |
| Kiosk UI (Compose หลัก) | `http://localhost:5175` |
| Kiosk UI (`medisync/infra`) | `http://localhost:5173` |
| Tester UI | `http://localhost:8899` |
| Kiosk code | `00010001` (project `0001`, ตู้ลำดับ `0001`) |
| Kiosk PIN | `123456` |
| Ward | `WARD-3A` |
| Staff เริ่มต้น | `pharmacist` |
| Staff password | `demo-pharmacist-2026` |

ข้อมูลเหล่านี้ใช้สำหรับ local development เท่านั้น ห้ามนำ credential ชุด demo ไปใช้ใน
production

## เริ่มทดสอบจากหน้าเว็บ

1. ตรวจช่อง **Kiosk code (ปลายทาง)** ให้เป็น `00010001` ระบบใช้ code นี้ทั้ง login,
   อ่าน stock, เปิด transaction และเลือก hardware agent ห้ามใช้ UUID ของตู้
2. กด **โหลดยาในตู้** ต้องเห็นยา `DEMO-PARA500`, `DEMO-AMOX500` และ
   `DEMO-OME20`
3. กด **สร้าง sticker** เพื่อสร้าง Prescription ID ผ่าน NATS
4. เปิด Kiosk UI เองที่ <http://localhost:5175> หรือใช้หน้าที่เปิดค้างไว้อยู่แล้ว
   จากนั้น login ด้วย `00010001` / `123456` ไม่จำเป็นต้องเปิดผ่านปุ่มใน tester
5. ก่อนจำลองการสแกนบัตรครั้งแรก ให้กด **ลงทะเบียนบัตร**
6. จากนั้นกด **ส่ง sticker เข้า Kiosk** และ **ส่งบัตรเข้า Kiosk** ตามลำดับ

Kiosk UI จะเชื่อมกับ `http://localhost:8899/api/kiosk-events?kioskCode=00010001`
อัตโนมัติและ reconnect
เองเมื่อ tester restart ปุ่ม **เปิด Kiosk UI (ทางลัด)** มีไว้เพื่อความสะดวกเท่านั้น
เมื่อหน้า Withdraw เชื่อมสำเร็จ badge ในส่วน **Kiosk UI Integration** จะแสดง
`เชื่อมต่อ 1 หน้า`

คำสั่ง scan ถูกส่งเฉพาะ browser session ที่ login ด้วย code ตรงกับช่อง Kiosk code
เท่านั้น เช่นคำสั่ง `00010001` จะไม่ไปถึง `00010002` แม้เปิดพร้อมกันคนละ browser

## กติกา Kiosk code

code เป็น business identity แบบ immutable รูปแบบ `PPPPKKKK`:

- project แรกได้ code `0001`; ตู้ที่เพิ่มตามลำดับได้ `00010001`, `00010002`, ...
- project ที่สองได้ code `0002`; ตู้แรกได้ `00020001`
- Database จัดลำดับภายใต้ row lock จึงไม่ออก code ซ้ำเมื่อสร้างพร้อมกัน
- UUID `kiosks.id` ยังมีไว้ใช้ภายใน CRUD เท่านั้น ห้ามใช้ route คำสั่งจ่ายยา
- ห้ามแก้ project code หรือ kiosk code หลังสร้าง เพราะ transaction, slot, Compose route
  และรายงานผูกกับ code นี้ตลอดอายุข้อมูล

## Flow การจ่ายยาจริง

1. ระบบต้นทางส่ง prescription พร้อม `project_code` 4 หลัก (Tester หา code นี้จาก
   prefix ของ kiosk code) แล้วผู้ใช้สแกน Sticker ที่ Kiosk UI
2. `PrepareDispense` ตรวจ prescription ใน project เดียวกัน เลือก stock จาก
   `slot.cabinet_id = kiosk.code` เท่านั้น และจอง batch แบบ FIFO
3. ระบบสร้าง `dispense_transaction`, items และ allocations สถานะ
   `AWAITING_IDENTITY`
4. สแกนบัตรเจ้าหน้าที่; `ConfirmDispense` ต้องได้รับทั้ง staff JWT และ kiosk JWT
   ที่ code ตรงกับ transaction
5. transaction เข้า `QUEUED` แล้ว event จะมี kiosk code กับ hardware address ของแต่ละ
   allocation ส่งไปยัง agent ที่ map ไว้เพียงตัวเดียว
6. หลัง hardware ตอบ ระบบบันทึกผลราย allocation, ตัด stock ที่จ่ายจริง, คืน reservation
   ที่ไม่สำเร็จ และจบเป็น `DISPENSED` หรือ `FAILED`

ถ้าปิดหน้าหรือไม่ยืนยันตัวตนภายใน 5 นาที reservation จะถูกคืนและ transaction เป็น
`EXPIRED`; ถ้ากดยกเลิกก่อนยืนยันจะเป็น `CANCELLED`

## ถ้าต้องการรัน E2E จนเป็น `DISPENSED` โดยไม่มีเครื่องจริง

Compose หลักใช้ hardware route จริงเป็นค่าเริ่มต้น:

```text
00010001 -> http://vending-00010001:3303
```

ถ้าต้องการทดสอบโดยไม่มีเครื่องจริง ให้เปิด stack ด้วย fake fulfillment อย่างชัดเจน:

```powershell
$env:FULFILLMENT_FAKE = "true"
$env:VENDING_FAKE = "true"
docker compose up -d --build --wait
```

ขั้นตอนด้านล่างใช้เฉพาะเมื่อต้องการรัน Core และ Kiosk แบบ local แทน Compose หลัก:

Terminal 1 — เปิดเฉพาะ PostgreSQL และ NATS:

```powershell
Set-Location D:\Projects\adm-chura3inter\medisync

# ทำครั้งแรกเท่านั้น และตรวจค่า local ใน .env ก่อนใช้งาน
if (-not (Test-Path .\.env)) { Copy-Item .\.env.example .\.env }

npm run infra:down
docker compose -f .\infra\docker-compose.yml up -d --wait postgres nats

$env:DATABASE_URL = "postgres://medisync:medisync@localhost:5432/medisync?sslmode=disable"
npm run seed:demo
```

Terminal 2 — เปิด Core แบบไม่เรียกฮาร์ดแวร์จริง:

```powershell
Set-Location D:\Projects\adm-chura3inter\medisync
$env:FULFILLMENT_FAKE = "true"
$env:PRINT_OPS_FAKE = "true"
npm run core
```

Terminal 3 — เปิด Kiosk UI:

```powershell
Set-Location D:\Projects\adm-chura3inter\medisync
npm run dev:kiosk
```

Terminal 4 — เปิด Kiosk Tester:

```powershell
Set-Location D:\Projects\adm-chura3inter\medisync\services\core
go run .\cmd\kiosktester -mode=serve
```

จากนั้นเปิด <http://localhost:8899> แล้วกด **รัน E2E flow รวม** หรือทดสอบผ่าน
ส่วน **Kiosk UI Integration**

อย่ารัน Docker Core และ local Core พร้อมกัน เพราะทั้งคู่ใช้พอร์ต `8080`

## รันจากโฟลเดอร์นี้โดยตรง

ถ้า Core, NATS และ demo data พร้อมอยู่แล้ว สามารถรันจากโฟลเดอร์ `kiosktester` ได้:

```powershell
Set-Location D:\Projects\adm-chura3inter\medisync\services\core\cmd\kiosktester
go run . -mode=serve
```

เปลี่ยนพอร์ต tester ได้เมื่อ `8899` ถูกใช้งานอยู่:

```powershell
go run . -mode=serve -addr=:8901
```

แล้วเปิด <http://localhost:8901>

## CLI ที่ใช้บ่อย

รันจาก `medisync\services\core`:

```powershell
# สร้าง Prescription ID โดยเลือกยาจาก slot อัตโนมัติ
go run .\cmd\kiosktester -mode=create -kiosk-code=00010001

# สร้างรายการโดยระบุยาเอง
go run .\cmd\kiosktester -mode=create -drugs="DEMO-PARA500:2,DEMO-AMOX500:3"

# ยืนยันรายการเดิม
go run .\cmd\kiosktester -mode=confirm -kiosk-code=00010001 -id="RX-YYYYMMDD-HHMMSS"

# สร้างและยืนยันต่อเนื่อง
go run .\cmd\kiosktester -mode=flow -kiosk-code=00010001
```

ดู option ทั้งหมด:

```powershell
go run .\cmd\kiosktester -h
```

## แก้ปัญหาเบื้องต้น

### เปิด `localhost:8899` ไม่ได้

- ตรวจว่า terminal ของ `go run ... -mode=serve` ยังทำงานอยู่
- ถ้าพอร์ตถูกใช้ ให้เปลี่ยนเป็น `-addr=:8901`

### ขึ้น `kiosk login` หรือ login ไม่ผ่าน

รัน seed ใหม่:

```powershell
Set-Location D:\Projects\adm-chura3inter
docker compose run --rm medisync-demo-seed
```

### ขึ้น `no stocked drugs found`

- รัน `docker compose run --rm medisync-demo-seed` จาก parent workspace
- ตรวจว่า Kiosk code เป็น `00010001` และ slots ใน Admin แสดง cabinet code เดียวกัน

### เชื่อม Core หรือ NATS ไม่ได้

ตรวจ container และพอร์ต:

```powershell
Set-Location D:\Projects\adm-chura3inter
docker compose ps
Test-NetConnection localhost -Port 8080
Test-NetConnection localhost -Port 4222
```

ดู log ของ Core:

```powershell
docker compose logs --tail=100 medisync-core medisync-kiosktester
```

### E2E ค้างที่ `DISPENSING` หรือ timeout

ตรวจ log ของ `medisync-core`, `vending-00010001` และ `medisync-kiosktester` ถ้าไม่มี
hardware ให้เปิด Compose ใหม่ด้วย `FULFILLMENT_FAKE=true` และ `VENDING_FAKE=true`
ตามหัวข้อด้านบน

## ดู transaction และทำรายงาน

เปิด Admin ที่ <http://localhost:5176> แล้วเลือกเมนู **Dispense Transactions** เพื่อ:

- กรองตาม kiosk code, prescription และสถานะ
- ดูผู้ยืนยัน, จำนวนที่ขอ/จ่ายจริง, slot, lot และ failure detail
- ส่งออก CSV แบบหนึ่งแถวต่อ allocation พร้อม hardware address และ timestamp ทุกขั้น

ข้อมูลต้นทางอยู่ใน `medisync.dispense_transaction`,
`medisync.dispense_transaction_item` และ `medisync.dispense_allocation`

## หยุดระบบ

หยุดเฉพาะ tester:

```powershell
Set-Location D:\Projects\adm-chura3inter
docker compose stop medisync-kiosktester
```

หยุด Compose หลักทั้งชุด:

```powershell
docker compose down
```
