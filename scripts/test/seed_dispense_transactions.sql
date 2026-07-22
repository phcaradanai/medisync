-- ============================================================
-- Seed 40 dispense transactions for testing ListDispenseTransactions
-- Requires existing data in: medisync.kiosks, medisync.slot, medisync.slot_batch
-- ============================================================

-- Clean up previous seed data (order matters due to FK constraints)
DELETE FROM medisync.dispense_allocation;
DELETE FROM medisync.dispense_transaction_item;
DELETE FROM medisync.dispense_transaction;
DELETE FROM medisync.prescription;

-- Create 10 users for operator references
INSERT INTO medisync.users (id, username, password_hash, display_name, role, ward_ids, active, project_id, employee_code, created_at, updated_at)
SELECT
    gen_random_uuid(),
    'operator' || generate_series,
    '$2a$10$dummyhashfordemopurposesonly',
    CASE generate_series
        WHEN 1 THEN 'นางสาว สมหญิง พยาบาล'
        WHEN 2 THEN 'นาย วิชัย เภสัชกร'
        WHEN 3 THEN 'นาง มาลี ผู้ช่วย'
        WHEN 4 THEN 'นาย สมชาย แพทย์'
        WHEN 5 THEN 'นางสาว รัชนี พยาบาล'
        WHEN 6 THEN 'นาย ประยุทธ์ เภสัชกร'
        WHEN 7 THEN 'นาง ดารา ผู้ช่วย'
        WHEN 8 THEN 'นาย ธีรภัทร พยาบาล'
        WHEN 9 THEN 'นางสาว กาญจนา แพทย์'
        ELSE 'นาย อนันต์ เภสัชกร'
    END,
    CASE generate_series % 4
        WHEN 0 THEN 'NURSE'
        WHEN 1 THEN 'PHARMACIST'
        WHEN 2 THEN 'NURSE'
        ELSE 'REFILLER'
    END,
    ARRAY['WARD0' || (1 + (generate_series % 5))],
    true,
    (SELECT project_id FROM medisync.kiosks LIMIT 1),
    'EMP' || LPAD((1000 + generate_series)::TEXT, 4, '0'),
    NOW(), NOW()
FROM generate_series(1, 10)
ON CONFLICT (username) DO NOTHING;

-- Create 40 prescriptions (one per transaction) in READY state
INSERT INTO medisync.prescription (id, prescription_id, source_system, hn, patient_name, ward_id, items, state, project_id, created_at, updated_at)
SELECT
    gen_random_uuid(),
    'RX-' || LPAD(generate_series::TEXT, 6, '0'),
    CASE WHEN generate_series % 3 = 0 THEN 'HIS' WHEN generate_series % 3 = 1 THEN 'EMR' ELSE 'PHARMACY' END,
    'HN' || LPAD((100000 + generate_series)::TEXT, 7, '0'),
    CASE generate_series % 5
        WHEN 0 THEN 'สมชาย ใจดี'
        WHEN 1 THEN 'สมหญิง รักดี'
        WHEN 2 THEN 'ประยุทธ์ ตั้งตรง'
        WHEN 3 THEN 'มาลี มีสุข'
        ELSE 'วิชัย กล้าหาญ'
    END,
    'WARD' || LPAD((1 + (generate_series % 5))::TEXT, 2, '0'),
    '[{"drug_code":"DRG-0001","drug_name":"Paracetamol 500mg","quantity":10,"dosage_text":"1 tab tid"}]'::jsonb,
    'READY',
    (SELECT project_id FROM medisync.kiosks LIMIT 1),
    NOW(), NOW()
FROM generate_series(1, 40);

-- Generate 40 dispense transactions with varying statuses
DO $$
DECLARE
    i INT;
    v_prescription_ids UUID[] := ARRAY(SELECT id FROM medisync.prescription ORDER BY created_at);
    v_kiosk_codes TEXT[] := ARRAY(SELECT code FROM medisync.kiosks ORDER BY code);
    dispense_id UUID;
    statuses TEXT[] := ARRAY['AWAITING_IDENTITY', 'QUEUED', 'DISPENSING', 'DISPENSED', 'FAILED', 'CANCELLED', 'EXPIRED'];
    status_text TEXT;
    kiosk_code TEXT;
    v_project_id UUID := (SELECT project_id FROM medisync.kiosks LIMIT 1);
    slot_rec RECORD;
    batch_rec RECORD;
    item_id UUID;
    created_ts TIMESTAMPTZ;
BEGIN
    FOR i IN 1..40 LOOP
        kiosk_code := v_kiosk_codes[1 + (i % array_length(v_kiosk_codes, 1))];
        status_text := statuses[1 + (i % array_length(statuses, 1))];
        created_ts := NOW() - ((40 - i) * INTERVAL '1 hour') - (i % 5 * INTERVAL '10 minutes');

        -- Insert dispense_transaction
        dispense_id := gen_random_uuid();
        INSERT INTO medisync.dispense_transaction (
            id, prescription_id, prescription_ref, source_system,
            kiosk_code, project_id, operator_user_id, operator_display_name,
            status, trace_id, failure_code, failure_detail,
            hardware_request, hardware_response,
            sticker_scanned_at, identity_confirmed_at,
            queued_at, started_at, completed_at, failed_at, cancelled_at,
            expires_at, created_at, updated_at
        ) VALUES (
            dispense_id,
            v_prescription_ids[1 + (i % array_length(v_prescription_ids, 1))],
            'RX-' || LPAD(i::TEXT, 6, '0'),
            CASE WHEN i % 3 = 0 THEN 'HIS' WHEN i % 3 = 1 THEN 'EMR' ELSE 'PHARMACY' END,
            kiosk_code, v_project_id,
            CASE WHEN status_text IN ('QUEUED','DISPENSING','DISPENSED','FAILED') THEN (SELECT id FROM medisync.users WHERE active=true AND project_id=v_project_id ORDER BY random() LIMIT 1) ELSE NULL END,
            CASE WHEN status_text IN ('QUEUED','DISPENSING','DISPENSED','FAILED') THEN 'Operator #' || (1 + (i % 10))::TEXT ELSE '' END,
            status_text,
            'trace-' || gen_random_uuid()::TEXT,
            CASE WHEN status_text = 'FAILED' THEN 'HARDWARE_ERROR' WHEN status_text = 'EXPIRED' THEN 'IDENTITY_TIMEOUT' ELSE '' END,
            CASE WHEN status_text = 'FAILED' THEN 'Vending machine timeout: door did not open' WHEN status_text = 'EXPIRED' THEN 'Sticker scan expired after 5 minutes' ELSE '' END,
            '{}'::jsonb, '{}'::jsonb,
            created_ts - INTERVAL '30 seconds',  -- sticker_scanned_at
            CASE WHEN status_text IN ('QUEUED','DISPENSING','DISPENSED','FAILED') THEN created_ts - INTERVAL '10 seconds' ELSE NULL END,  -- identity_confirmed_at
            CASE WHEN status_text IN ('QUEUED','DISPENSING','DISPENSED','FAILED') THEN created_ts ELSE NULL END,  -- queued_at
            CASE WHEN status_text IN ('DISPENSING','DISPENSED','FAILED') THEN created_ts + INTERVAL '1 minute' ELSE NULL END,  -- started_at
            CASE WHEN status_text = 'DISPENSED' THEN created_ts + INTERVAL '2 minutes' ELSE NULL END,  -- completed_at
            CASE WHEN status_text = 'FAILED' THEN created_ts + INTERVAL '2 minutes' ELSE NULL END,  -- failed_at
            CASE WHEN status_text = 'CANCELLED' THEN created_ts + INTERVAL '1 minute' ELSE NULL END,  -- cancelled_at
            created_ts + INTERVAL '5 minutes',  -- expires_at
            created_ts, created_ts
        );

        -- Insert 1-3 items per transaction
        FOR j IN 1..(1 + (i % 3)) LOOP
            item_id := gen_random_uuid();
            INSERT INTO medisync.dispense_transaction_item (
                id, dispense_id, sequence_no, drug_code, drug_name,
                requested_quantity, allocated_quantity, dispensed_quantity, status
            ) VALUES (
                item_id, dispense_id, j,
                'DRG-' || LPAD((1 + ((i + j) % 20))::TEXT, 4, '0'),
                CASE WHEN j % 3 = 0 THEN 'Paracetamol 500mg'
                     WHEN j % 3 = 1 THEN 'Amoxicillin 250mg'
                     ELSE 'Omeprazole 20mg' END,
                10 + (j * 5),  -- requested
                10 + (j * 5),  -- allocated
                CASE WHEN status_text = 'DISPENSED' THEN 10 + (j * 5) WHEN status_text = 'FAILED' THEN 0 ELSE 0 END,  -- dispensed
                CASE WHEN status_text = 'DISPENSED' THEN 'DISPENSED'
                     WHEN status_text = 'FAILED' THEN 'FAILED'
                     ELSE 'RESERVED' END
            );

            -- Insert allocation for each item (pick random slot/batch)
            FOR slot_rec IN
                SELECT s.id AS slot_id, s.code AS slot_code, s.door_no, s.hardware_layer, s.channel_start, s.channel_end
                FROM medisync.slot s
                WHERE s.project_id = v_project_id AND s.is_active = true
                ORDER BY random() LIMIT 1
            LOOP
                FOR batch_rec IN
                    SELECT b.id AS batch_id, b.lot_number, b.expiry_date
                    FROM medisync.slot_batch b
                    WHERE b.slot_id = slot_rec.slot_id
                    ORDER BY random() LIMIT 1
                LOOP
                    INSERT INTO medisync.dispense_allocation (
                        id, dispense_id, item_id, slot_id, slot_code,
                        batch_id, lot_number, expiry_date,
                        quantity, dispensed_quantity, door_no, hardware_layer,
                        channel_start, channel_end, status,
                        hardware_attempted_at, hardware_success, hardware_detail,
                        hardware_response
                    ) VALUES (
                        gen_random_uuid(), dispense_id, item_id,
                        slot_rec.slot_id, slot_rec.slot_code,
                        batch_rec.batch_id, batch_rec.lot_number, batch_rec.expiry_date,
                        10 + (j * 5),  -- quantity
                        CASE WHEN status_text = 'DISPENSED' THEN 10 + (j * 5) ELSE 0 END,  -- dispensed_quantity
                        slot_rec.door_no, slot_rec.hardware_layer,
                        slot_rec.channel_start, slot_rec.channel_end,
                        CASE WHEN status_text = 'DISPENSED' THEN 'DISPENSED'
                             WHEN status_text = 'FAILED' THEN 'FAILED'
                             ELSE 'RESERVED' END,
                        CASE WHEN status_text IN ('DISPENSED','FAILED') THEN created_ts + INTERVAL '2 minutes' ELSE NULL END,
                        CASE WHEN status_text = 'DISPENSED' THEN true WHEN status_text = 'FAILED' THEN false ELSE NULL END,
                        CASE WHEN status_text = 'DISPENSED' THEN 'Dispensed successfully'
                             WHEN status_text = 'FAILED' THEN 'Hardware error: door stuck'
                             ELSE NULL END,
                        '{}'::jsonb
                    );
                END LOOP;
            END LOOP;
        END LOOP;
    END LOOP;
END $$;