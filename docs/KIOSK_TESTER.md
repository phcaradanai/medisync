# Kiosk flow tester

คู่มือฉบับเต็มอยู่ที่
[`services/core/cmd/kiosktester/README.md`](../services/core/cmd/kiosktester/README.md)

Tester ใช้ flow เดียวกับตู้จริง:

1. สร้าง `PrescriptionCreated` โดยระบุ `project_code` 4 หลัก เช่น `0001`
2. ส่ง scan event ไปยัง Kiosk UI ที่ login ด้วย kiosk code เป้าหมายเท่านั้น
3. Kiosk เรียก `PrepareDispense` เพื่อจองยาเฉพาะ slot ที่มี
   `cabinet_id = kiosk.code`
4. หลังสแกนบัตรพนักงาน Kiosk เรียก `ConfirmDispense` ด้วย staff token และ kiosk
   token คนละชนิด
5. Core ส่ง allocation ไปยัง hardware agent ที่ map กับ code นั้นเพียง endpoint เดียว
6. ผลสำเร็จ/ล้มเหลว, lot, slot, hardware address, ผู้ปฏิบัติงาน และเวลาทุกช่วงถูกเก็บใน
   dispense transaction สำหรับรายงาน

ตัวอย่าง code: project `0001` มีตู้ `00010001`, `00010002`; project `0002` มีตู้
`00020001` โดย code ถูกสร้างจากฐานข้อมูลและแก้ภายหลังไม่ได้

เปิดทุก service จาก workspace root:

```powershell
docker compose up -d --build --wait
```

เปิด Kiosk ที่ <http://localhost:5175> และ Tester ที่ <http://localhost:8899> แล้ว login
ตู้ demo ด้วย `00010001` / `123456` ตู้ไม่จำเป็นต้องเปิดจากปุ่มใน Tester

รัน backend E2E โดยไม่ใช้ browser:

```powershell
cd medisync\services\core
go run .\cmd\kiosktester -mode=flow -kiosk-code=00010001
```

เมื่อต้องทดสอบโดยไม่มี hardware ให้ตั้ง `FULFILLMENT_FAKE=true` และ
`VENDING_FAKE=true` เฉพาะ environment ทดสอบก่อน `docker compose up`; ค่าเริ่มต้นของ
Compose เป็น route จริงและจะ fail closed หากไม่มี mapping ของ kiosk code
