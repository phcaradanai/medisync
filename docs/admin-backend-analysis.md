# วิเคราะห์ระบบหลังบ้าน Admin (MediSync Backend Analysis)

## ภาพรวม (Overview)

MediSync เป็นระบบจัดการจ่ายยาอัตโนมัติ (Automated Medication Dispensing System) ที่ประกอบด้วยระบบหลังบ้าน (Backend) ที่พัฒนาด้วยภาษา Go และระบบหน้าบ้าน (Frontend) สำหรับ Admin ที่พัฒนาด้วย React/TypeScript ระบบหลังบ้านถูกออกแบบเป็น **Modular Monolith** โดยใช้ **Connect-RPC** (gRPC-compatible) สำหรับการสื่อสารระหว่าง Frontend และ Backend และใช้ **NATS JetStream** สำหรับ Event-Driven Architecture ระหว่างโมดูลภายใน

---

## 1. ระบบจัดการผู้ใช้และสิทธิ์การเข้าใช้งาน (Identity & Access Management)

### โมดูล: `services/core/internal/identity/`

### ฟังก์ชันการทำงานหลัก

#### 1.1 การยืนยันตัวตน (Authentication)
- **Login ด้วย Username/Password**: ผู้ใช้สามารถเข้าสู่ระบบด้วย username และ password โดย password จะถูกเข้ารหัสด้วย bcrypt ก่อนจัดเก็บ
- **Login ด้วยบัตร (Card Login)**: รองรับการยืนยันตัวตนผ่าน card token สำหรับการใช้งานที่ Kiosk
- **JWT Token Management**: สร้าง JWT access token หลังจาก login สำเร็จ โดยมีอายุตามที่กำหนด (default 3600 วินาที)
- **Rate Limiting**: จำกัดจำนวนครั้งในการ login (default 10 ครั้ง/60 วินาที) ต่อ username และ IP address เพื่อป้องกัน brute force attack

#### 1.2 การจัดการผู้ใช้ (User Management)
- **CRUD ผู้ใช้**: สร้าง, แก้ไข, ดูรายการ, และปิดการใช้งานผู้ใช้
- **บทบาท (Roles)**:
  - `ADMIN` - ผู้ดูแลระบบ (สามารถดูข้อมูลข้าม ward ได้ทั้งหมด)
  - `PHARMACIST` - เภสัชกร
  - `NURSE` - พยาบาล
  - `REFILLER` - ผู้เติมยา
- **สิทธิ์ตาม Ward**: ผู้ใช้ที่ไม่ใช่ ADMIN จะถูกจำกัดให้เข้าถึงเฉพาะ ward ที่ได้รับมอบหมาย
- **SYSADMIN**: ADMIN ที่ไม่มี project binding สามารถจัดการข้าม project ได้ทั้งหมด

#### 1.3 การจัดการโครงการ (Project Management)
- **Multi-tenant**: รองรับหลายโครงการ (Project) โดยแยกข้อมูลกัน
- **CRUD โครงการ**: สร้าง, แก้ไข, ดูรายการ, และปิดการใช้งานโครงการ
- **SYSADMIN-only**: การจัดการโครงการทำได้โดย SYSADMIN เท่านั้น

#### 1.4 การจัดการ Kiosk (Kiosk Management)
- **Provisioning**: สร้างและจัดการตู้ยา (Kiosk/Cabinet)
- **Kiosk Token**: สร้าง JWT token สำหรับ Kiosk โดยเฉพาะ
- **PIN Management**: จัดการ PIN สำหรับเข้าใช้งานตู้ยา

### API Endpoints (Connect-RPC)
- `IdentityService.Login` - เข้าสู่ระบบด้วย username/password
- `IdentityService.CardLogin` - เข้าสู่ระบบด้วยบัตร
- `IdentityService.WhoAmI` - ตรวจสอบ token และดึงข้อมูลผู้ใช้ปัจจุบัน
- `IdentityService.ListUsers` - ดูรายการผู้ใช้
- `IdentityService.CreateUser` - สร้างผู้ใช้ใหม่
- `IdentityService.UpdateUser` - แก้ไขผู้ใช้
- `ProjectService.ListProjects` - ดูรายการโครงการ
- `ProjectService.CreateProject` - สร้างโครงการใหม่
- `ProjectService.UpdateProject` - แก้ไขโครงการ
- `KioskService.ListKiosks` - ดูรายการตู้ยา
- `KioskService.CreateKiosk` - สร้างตู้ยาใหม่
- `KioskService.UpdateKiosk` - แก้ไขตู้ยา
- `KioskService.ResetKioskPin` - รีเซ็ต PIN ตู้ยา

---

## 2. ระบบจัดการข้อมูลยา (Catalog / Drug Master Data)

### โมดูล: `services/core/internal/catalog/`

### ฟังก์ชันการทำงานหลัก

#### 2.1 การจัดการข้อมูลยา (Drug Master Data)
- **CRUD ยา**: สร้าง, แก้ไข, ดูรายการ, และปิดการใช้งานข้อมูลยา
- **ข้อมูลยาประกอบด้วย**:
  - รหัสยา (Code)
  - ชื่อยา (Name, Display Name, Generic Name)
  - รูปแบบยา (Form): Tablet, Capsule, Syrup, Injection, Cream, Drops
  - ความแรง (Strength)
  - หน่วยนับ (Unit)
  - บาร์โค้ด (Barcode)
  - ข้อความบนสติกเกอร์ (Sticker Note)
  - ความจุเริ่มต้นของช่อง (Default Slot Capacity)
  - หมวดหมู่ (Category)
  - ผู้ผลิต (Manufacturer)
  - การจัดระดับความปลอดภัย (Safety Classification): NORMAL, LASA, HIGH_ALERT
- **ค้นหายา**: ค้นหาด้วย query, กรองตามสถานะ active/inactive
- **Project Scoping**: ข้อมูลยาถูกแยกตาม Project (multi-tenant)

### API Endpoints (Connect-RPC)
- `CatalogService.ListDrugs` - ดูรายการยา
- `CatalogService.GetDrug` - ดูรายละเอียดยา
- `CatalogService.CreateDrug` - สร้างยาใหม่
- `CatalogService.UpdateDrug` - แก้ไขยา
- `CatalogService.DeactivateDrug` - ปิดการใช้งานยา

---

## 3. ระบบจัดการคลังยา (Inventory Management)

### โมดูล: `services/core/internal/inventory/`

### ฟังก์ชันการทำงานหลัก

#### 3.1 การจัดการช่องเก็บยา (Slot Management)
- **CRUD ช่องเก็บยา**: สร้าง, แก้ไข, ดูรายการช่องเก็บยาในตู้
- **ข้อมูลช่องเก็บยาประกอบด้วย**:
  - รหัสช่อง (Code)
  - ชื่อที่แสดง (Display Name)
  - ยาที่จัดเก็บ (Drug ID, Drug Code, Drug Name)
  - ความจุ (Capacity)
  - จำนวนคงเหลือ (Quantity)
  - เกณฑ์แจ้งเตือนเมื่อใกล้หมด (Low Threshold)
  - วันหมดอายุ (Expiry Date)
  - ชั้นและแถว (Shelf, Row)
  - หมวดหมู่, ผู้ผลิต, การจัดระดับความปลอดภัย

#### 3.2 การจัดการ Batch (Slot Batch Management)
- **FIFO Allocation**: จ่ายยาตามหลัก First-In-First-Out โดยใช้ batch ที่หมดอายุก่อนจ่ายก่อน
- **Lot Number**: ติดตาม lot number ของยาแต่ละ batch
- **Expiry Date Tracking**: ติดตามวันหมดอายุของยาแต่ละ batch

#### 3.3 การเติมยา (Refill)
- **เพิ่มสต็อก**: เติมยาเข้าช่องเก็บ
- **ปรับสต็อก**: ปรับจำนวนยาด้วยเหตุผล (Adjust Stock)

#### 3.4 การคำนวณความจุ (Capacity Calculation)
- **คำนวณความจุตามขนาดจริง**: คำนวณจำนวนยาที่สามารถบรรจุในช่องตามขนาดจริง (กว้าง x ลึก x สูง)
- **Slot Group Capacity**: คำนวณความจุรวมของกลุ่มช่องเก็บยา

### API Endpoints (Connect-RPC)
- `InventoryService.ListSlots` - ดูรายการช่องเก็บยา
- `InventoryService.CreateSlot` - สร้างช่องเก็บยาใหม่
- `InventoryService.AssignDrug` - กำหนดยาให้ช่องเก็บ
- `InventoryService.Refill` - เติมยา
- `InventoryService.AdjustStock` - ปรับสต็อก

---

## 4. ระบบจ่ายยา (Dispensing)

### โมดูล: `services/core/internal/dispensing/`

### ฟังก์ชันการทำงานหลัก

#### 4.1 การรับใบสั่งยา (Prescription Intake)
- **รับข้อมูลจากระบบโรงพยาบาล**: รับใบสั่งยาผ่าน NATS consumer จากระบบ HIS (Hospital Information System)
- **Source System**: รองรับหลายระบบต้นทาง

#### 4.2 สถานะใบสั่งยา (Prescription State Machine)
- **State Flow**:
  ```
  RECEIVED → READY → DISPENSING → DISPENSED
     ↓         ↓         ↓
  CANCELLED  CANCELLED  FAILED
     ↓         ↓
  EXPIRED    EXPIRED
  ```
- **Valid Transitions**:
  - RECEIVED → READY, CANCELLED, EXPIRED
  - READY → DISPENSING, CANCELLED, EXPIRED
  - DISPENSING → DISPENSED, FAILED
  - DISPENSED, FAILED, CANCELLED, EXPIRED เป็นสถานะปลายทาง (Terminal)

#### 4.3 การจัดการใบสั่งยา (Prescription Management)
- **ดูรายการใบสั่งยา**: กรองตาม ward, สถานะ
- **เปลี่ยนสถานะ**: อัปเดตสถานะใบสั่งยาตาม state machine
- **Authorization**: ADMIN เห็นทุก ward, ผู้ใช้ทั่วไปเห็นเฉพาะ ward ที่ได้รับมอบหมาย

#### 4.4 Event-Driven Integration
- **Outbox Publisher**: ส่ง event การเปลี่ยนแปลงสถานะผ่าน NATS
- **Dispense Completion Consumer**: รับ event การจ่ายยาสำเร็จ/ล้มเหลว
- **Dispense Requested Consumer**: ส่งคำขอจ่ายยาไปยัง fulfillment system

### API Endpoints (Connect-RPC)
- `DispensingService.ListPrescriptions` - ดูรายการใบสั่งยา
- `DispensingService.TransitionState` - เปลี่ยนสถานะใบสั่งยา

---

## 5. ระบบพิมพ์สติกเกอร์ (Printing)

### โมดูล: `services/core/internal/printing/`

### ฟังก์ชันการทำงานหลัก

#### 5.1 การพิมพ์สติกเกอร์ยา
- **รับคำขอพิมพ์ผ่าน NATS**: รับ event `medisync.print.requested` จาก JetStream
- **สร้างสติกเกอร์**: สร้างสติกเกอร์จากข้อมูลใบสั่งยา (ชื่อยา, จำนวน, วิธีใช้)
- **ส่งไปยัง Print Ops**: ส่งงานพิมพ์ไปยัง print_ops API
- **Audit Trail**: บันทึกประวัติการพิมพ์

#### 5.2 การจัดการข้อผิดพลาด
- **Retry**: ลองใหม่สูงสุด 5 ครั้ง ด้วย BackOff (2s, 5s, 15s, 30s)
- **DLQ**: ข้อความที่ไม่สามารถประมวลผลได้จะถูกส่งไปยัง Dead Letter Queue

### NATS Subjects
- Subscribe: `medisync.print.requested`
- Publish: `medisync.print.completed`

---

## 6. ระบบจ่ายยาอัตโนมัติ (Vending / Fulfillment)

### โมดูล: `services/core/internal/vending/`

### ฟังก์ชันการทำงานหลัก

#### 6.1 การเชื่อมต่อกับ Vending Agent
- **สื่อสารกับ vending-3d-ctl-agent**: ส่งคำขอจ่ายยาไปยังระบบควบคุมตู้ยาจริง
- **HTTP Client**: ส่งคำขอผ่าน HTTP ไปยัง Vending API
- **Fake Client**: สำหรับพัฒนาและทดสอบ (ใช้เมื่อตั้งค่า `FulfillmentFake=true`)

#### 6.2 การจัดการคำขอจ่ายยา
- **รับคำขอผ่าน NATS**: รับ event `medisync.fulfillment.requested`
- **ส่งคำสั่งจ่ายยา**: ส่งคำสั่งไปยัง vending agent
- **รายงานผล**: ส่ง event `medisync.fulfillment.completed` กลับ

### NATS Subjects
- Subscribe: `medisync.fulfillment.requested`
- Publish: `medisync.fulfillment.completed`

---

## 7. ระบบบันทึกประวัติการใช้งาน (Audit Log)

### โมดูล: `services/core/internal/platform/audit/`

### ฟังก์ชันการทำงานหลัก

#### 7.1 การบันทึกประวัติ (Audit Trail)
- **Append-only**: บันทึกประวัติแบบ追加เท่านั้น ไม่สามารถแก้ไขหรือลบได้
- **ข้อมูลที่บันทึก**:
  - Trace ID - สำหรับติดตาม request chain
  - Actor - ผู้กระทำการ (user ID หรือ "system")
  - Action - การกระทำ (เช่น "prescription.received", "drug.created")
  - Entity - ประเภทข้อมูลที่ถูกกระทำ (เช่น "prescription", "drug", "slot")
  - Entity ID - ID ของข้อมูล
  - Project ID - สำหรับ multi-tenant isolation
  - Detail - รายละเอียดเพิ่มเติม (JSON)
  - Created At - เวลาที่บันทึก

#### 7.2 การดูประวัติ (Audit Log Viewing)
- **ค้นหา**: ค้นหาตาม actor, action, entity, entityId, projectId, detail
- **แบ่งหน้า**: รองรับ pagination (default 50 รายการ, สูงสุด 200)
- **เรียงตามเวลา**: เรียงจากล่าสุดไปเก่าสุด

### API Endpoints (Connect-RPC)
- `AuditService.ListAuditLogs` - ดูรายการ audit log

---

## 8. ระบบ Platform Services

### โมดูล: `services/core/internal/platform/`

#### 8.1 การตั้งค่าระบบ (Config) - `platform/config/`
- อ่านค่าจาก environment variables
- ตรวจสอบความปลอดภัยของ secrets (JWT secret, HMAC key)
- รองรับค่า default สำหรับ development

#### 8.2 การเชื่อมต่อฐานข้อมูล (PostgreSQL) - `platform/postgres/`
- Connection pooling ด้วย pgx
- Database migration management

#### 8.3 การเชื่อมต่อ NATS - `platform/natsx/`
- NATS connection management
- JetStream stream management
- Subject constants

#### 8.4 การตรวจสอบสิทธิ์ (Auth) - `platform/auth/`
- Shared authorization types
- Context helpers สำหรับ claims
- Authorization helpers (IsSysadmin, HasProjectAccess, CanAccessWard)

#### 8.5 การจำกัดอัตราการใช้งาน (Rate Limit) - `platform/ratelimit/`
- Sliding window rate limiter
- ใช้สำหรับ login endpoint

#### 8.6 การติดตาม (Tracing) - `platform/tracing/`
- OpenTelemetry-style interceptor
- Logging request processing

#### 8.7 การตรวจสอบสุขภาพ (Monitor) - `platform/monitor/`
- Consumer health tracking
- Error rate monitoring
- Health check endpoint (`/health`)

#### 8.8 การแบ่งหน้า (Pagination) - `platform/pagination/`
- Cursor-based pagination helper

#### 8.9 การบันทึก (Logging) - `platform/logging/`
- Structured logging with slog

---

## 9. ระบบ Frontend (Admin App)

### โมดูล: `apps/admin/`

### ฟังก์ชันการทำงานหลัก

#### 9.1 Dashboard (ภาพรวมระบบ)
- **KPI Overview**: แสดงจำนวนข้อมูลหลักทั้งหมด, ยาที่พร้อมใช้งาน, ช่องเก็บยาทั้งหมด
- **Master Data Cards**: แสดงจำนวนยา, โครงการ, ตู้ยา, ผู้ใช้งาน
- **Stock Alert**: แจ้งเตือนสต็อกใกล้หมด/หมด
- **Navigation**: ลิงก์ไปยังหน้าจัดการต่างๆ

#### 9.2 หน้าจัดการยา (Drugs)
- **รายการยา**: แสดงตารางยาพร้อมค้นหาและกรอง
- **สร้าง/แก้ไขยา**: ฟอร์ม 4 ขั้นตอน (ข้อมูลยา, การแสดงผล, การเชื่อมโยง, สถานะ)
- **Duplicate**: คัดลอกข้อมูลยา
- **Archive**: ปิดการใช้งานยา

#### 9.3 หน้าจัดการคลังยา (Inventory)
- **รายการช่องเก็บยา**: แสดงตารางช่องเก็บยาพร้อมสถานะสต็อก
- **สร้างช่องเก็บยา**: เพิ่มช่องเก็บยาใหม่
- **กำหนดยาให้ช่อง**: เลือกยาและกำหนดความจุ
- **เติมยา**: เพิ่มจำนวนยาในช่อง
- **ปรับสต็อก**: ปรับจำนวนยาพร้อมระบุเหตุผล

#### 9.4 หน้าจัดการอุปกรณ์ (Devices)
- **รายการตู้ยา**: แสดงตารางตู้ยาพร้อมสถานะ
- **สร้าง/แก้ไขตู้ยา**: ฟอร์ม 3 ขั้นตอน (ข้อมูลตู้ยา, ความปลอดภัย, สถานะ)
- **Reset PIN**: รีเซ็ต PIN ตู้ยา

#### 9.5 หน้าจัดการผู้ใช้ (Users)
- **รายการผู้ใช้**: แสดงตารางผู้ใช้พร้อมค้นหาและกรองตามบทบาท
- **สร้าง/แก้ไขผู้ใช้**: ฟอร์ม 3 ขั้นตอน (ข้อมูลบัญชี, สิทธิ์และขอบเขต, สถานะ)
- **กำหนด Ward**: กำหนด ward ที่ผู้ใช้สามารถเข้าถึงได้

#### 9.6 หน้าจัดการโครงการ (Projects)
- **รายการโครงการ**: แสดงตารางโครงการพร้อมค้นหาและกรอง
- **สร้าง/แก้ไขโครงการ**: ฟอร์ม 2 ขั้นตอน (ข้อมูลโครงการ, สถานะ)

#### 9.7 หน้าดูประวัติการใช้งาน (Audit Log)
- **รายการ Audit Log**: แสดงตารางประวัติการเปลี่ยนแปลงข้อมูล
- **ค้นหา**: ค้นหาตาม actor, action, entity, entityId, projectId, detail

---

## 10. Event-Driven Architecture (NATS JetStream)

### Event Flow Diagram

```
HIS System → [NATS: medisync.prescription.received] → Dispensing Consumer
                                                           ↓
                                              Dispensing Store (PostgreSQL)
                                                           ↓
                                              Outbox Publisher
                                                           ↓
                                              [NATS: medisync.dispense.requested]
                                                           ↓
                                              DispenseRequestedConsumer
                                                           ↓
                                              [NATS: medisync.fulfillment.requested]
                                                           ↓
                                              Vending Consumer → Vending Agent
                                                           ↓
                                              [NATS: medisync.fulfillment.completed]
                                                           ↓
                                              Completion Consumer → Update Prescription State
                                                           ↓
                                              [NATS: medisync.print.requested]
                                                           ↓
                                              Printing Consumer → Print Ops API
                                                           ↓
                                              [NATS: medisync.print.completed]
```

### NATS Streams
- `medisync` - Main stream สำหรับ events ทั้งหมด

### Key Subjects
- `medisync.prescription.received` - รับใบสั่งยาจาก HIS
- `medisync.dispense.requested` - คำขอจ่ายยา
- `medisync.fulfillment.requested` - คำขอ fulfillment
- `medisync.fulfillment.completed` - ผลการ fulfillment
- `medisync.print.requested` - คำขอพิมพ์สติกเกอร์
- `medisync.print.completed` - ผลการพิมพ์

---

## 11. สรุปความสัมพันธ์ระหว่างระบบ

| ระบบ | ข้อมูลหลัก | ขึ้นกับระบบ | เชื่อมต่อด้วย |
|------|-----------|------------|-------------|
| Identity | ผู้ใช้, โครงการ, Kiosk | - | Connect-RPC |
| Catalog | ยา | Identity (Project) | Connect-RPC |
| Inventory | ช่องเก็บยา, Batch | Catalog (Drug), Identity (Project) | Connect-RPC, NATS |
| Dispensing | ใบสั่งยา | Identity (Auth), Inventory (Stock) | Connect-RPC, NATS |
| Printing | สติกเกอร์ | Dispensing (Prescription) | NATS |
| Vending | คำสั่งจ่ายยา | Dispensing (Fulfillment) | NATS |
| Audit | ประวัติการใช้งาน | ทุกระบบ | PostgreSQL |

### หลักการออกแบบ
1. **Modular Monolith**: แต่ละ bounded context แยกเป็น package อิสระ แต่ทำงานใน process เดียวกัน
2. **Event-Driven**: ใช้ NATS JetStream สำหรับการสื่อสารแบบ asynchronous ระหว่างโมดูล
3. **Multi-tenant**: ข้อมูลถูกแยกตาม Project (Project ID)
4. **Role-Based Access Control**: สิทธิ์การเข้าถึงขึ้นอยู่กับบทบาท (Role) และ Ward
5. **Audit Trail**: ทุกการเปลี่ยนแปลงข้อมูลสำคัญถูกบันทึกใน audit log
6. **FIFO Dispensing**: จ่ายยาตามหลัก First-In-First-Out เพื่อลดยาหมดอายุ