# smsmail

[![CI](https://github.com/akaporn-katip/sms-email-ui/actions/workflows/ci.yml/badge.svg)](https://github.com/akaporn-katip/sms-email-ui/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/akaporn-katip/sms-email-ui)](https://github.com/akaporn-katip/sms-email-ui/releases)

เครื่องมือทดสอบอีเมลและ SMS สำหรับ development — แนวคิดเดียวกับ [Mailpit](https://github.com/axllent/mailpit)
แต่เพิ่ม **mock ของ SMS REST API** เข้ามาด้วย

- **SMTP catcher** — ดักจับอีเมลทุกฉบับที่แอปส่งออกมา ไม่ส่งต่อไปไหน (port `1025`)
- **Mock SMS API** — REST API จำลองสำหรับส่ง SMS/OTP ครบ 13 endpoints เก็บข้อความไว้ในหน่วยความจำ (port `8080`)
- **Web UI** — inbox รวม Email + SMS + OTP ดูรายละเอียด ค้นหา ลบ (port `8025`)
- **Management API** — ให้ integration test ดึงข้อความและรหัส OTP ไปตรวจสอบได้

ทุกอย่างเก็บในหน่วยความจำ (in-memory) ล้วน — ปิดโปรเซสแล้วหายหมด ไม่มีไฟล์ ไม่มี database

---

## เริ่มใช้งาน

```bash
go build -o smsmail ./cmd/smsmail
./smsmail
```

จากนั้นเปิด http://localhost:8025

```
  smsmail is ready
  ────────────────────────────────────────────
  Version       dev
  Web UI        http://localhost:8025
  SMS API       http://localhost:8080/api/v1
  API via UI    http://localhost:8025/api/v1
  SMTP          localhost:1025 (no auth required)
  ────────────────────────────────────────────
```

เช็คเวอร์ชันที่ build อยู่: `./smsmail -version`

## ติดตั้ง

**ดาวน์โหลด binary** — ดู [Releases](https://github.com/akaporn-katip/sms-email-ui/releases)
มีให้สำหรับ linux/darwin/windows (amd64, arm64) พร้อม `SHA256SUMS`

```bash
VERSION=v0.1.0 OS=linux ARCH=amd64
curl -fsSLO "https://github.com/akaporn-katip/sms-email-ui/releases/download/${VERSION}/smsmail_${VERSION}_${OS}_${ARCH}.tar.gz"
tar -xzf "smsmail_${VERSION}_${OS}_${ARCH}.tar.gz"
./smsmail -version
```

**Docker**

```bash
docker run --rm -p 1025:1025 -p 8025:8025 -p 8080:8080 \
  ghcr.io/akaporn-katip/sms-email-ui:latest
```

หรือ `docker compose`:

```yaml
services:
  smsmail:
    image: ghcr.io/akaporn-katip/sms-email-ui:latest
    ports:
      - "1025:1025"   # SMTP
      - "8025:8025"   # Web UI + management API
      - "8080:8080"   # mock SMS API
    command: ["-credit", "500"]        # flag เพิ่มเติมต่อท้ายได้เลย
    # environment:
    #   SMSMAIL_RATE_LIMIT: "true"
```

build เองจาก source:

```bash
docker build -t smsmail .
docker run --rm -p 1025:1025 -p 8025:8025 -p 8080:8080 smsmail
```

container รันเป็น user `smsmail` (uid 10001) ไม่ใช่ root

หรือรันตรงจาก source: `go run ./cmd/smsmail`

### ตั้งค่าแอปให้ยิงเข้า smsmail

| บริการ | ค่าที่ตั้ง |
|---|---|
| SMTP | host `localhost` port `1025` — ไม่ต้อง auth (หรือใส่ user/pass อะไรก็ได้) |
| SMS API | base URL `http://localhost:8080` (หรือ `http://localhost:8025`) |
| API Key | อะไรก็ได้ที่ขึ้นต้นด้วย `sk_` เช่น `sk_test` |

---

## Configuration

ค่าทั้งหมดตั้งผ่าน flag, environment variable (`SMSMAIL_*`) หรือใช้ค่า default
flag ชนะ env, env ชนะ default

| Flag | Env | Default | ความหมาย |
|---|---|---|---|
| `-smtp-addr` | `SMSMAIL_SMTP_ADDR` | `:1025` | SMTP listen address (ใส่ `''` เพื่อปิด) |
| `-web-addr` | `SMSMAIL_WEB_ADDR` | `:8025` | Web UI + management API |
| `-api-addr` | `SMSMAIL_API_ADDR` | `:8080` | Mock SMS API (ใส่ `''` เพื่อปิด) |
| `-api-key` | `SMSMAIL_API_KEY` | ว่าง | บังคับให้ bearer token ต้องตรงค่านี้เป๊ะ (ว่าง = รับทุก `sk_…`) |
| `-rate-limit` | `SMSMAIL_RATE_LIMIT` | `false` | เปิดใช้ rate limit |
| `-credit` | `SMSMAIL_CREDIT` | `1500` | เครดิต SMS เริ่มต้น |
| `-account-name` | `SMSMAIL_ACCOUNT_NAME` | `Local Developer` | ชื่อบัญชีที่ `/api/v1/balance` ตอบกลับ |
| `-account-email` | `SMSMAIL_ACCOUNT_EMAIL` | `dev@example.com` | อีเมลบัญชีที่ `/api/v1/balance` ตอบกลับ |
| `-failure-numbers` | `SMSMAIL_FAILURE_NUMBERS` | `0000000000` | เบอร์ที่บังคับให้ delivery ล้มเหลว (คั่นด้วย comma) |

ตัวอย่าง:

```bash
./smsmail -rate-limit -credit 10 -failure-numbers '0000000000,0999999999'
```

---

## ส่งอีเมลเข้าระบบ

```bash
swaks --to you@example.com --server localhost:1025
# หรือ
curl smtp://localhost:1025 --mail-from a@example.com --mail-rcpt b@example.com -T message.eml
```

```python
import smtplib
from email.message import EmailMessage

msg = EmailMessage()
msg["From"] = "somchai@example.com"
msg["To"] = "you@example.com"
msg["Subject"] = "ทดสอบ"
msg.set_content("plain text")
msg.add_alternative("<h1>HTML</h1>", subtype="html")

with smtplib.SMTP("127.0.0.1", 1025) as s:
    s.login("any", "any")        # AUTH รับทุก credential
    s.send_message(msg)
```

รองรับ multipart (text + HTML + attachments), RFC 2047 encoded-word ใน subject,
และ decode เนื้อหาที่เป็น charset อื่น (เช่น ISO-8859-1, TIS-620) เป็น UTF-8 ให้อัตโนมัติ

---

## Mock SMS API

Base URL `http://localhost:8080` — ทุก request ต้องมี
`Authorization: Bearer sk_…` (ถ้าไม่ได้ตั้ง `-api-key` ไว้ รับทุก token ที่ขึ้นต้นด้วย `sk_`)

### SMS

```bash
AUTH='Authorization: Bearer sk_test'

# ส่ง SMS
curl -X POST localhost:8080/api/v1/sms/send -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"sender":"MySender","to":"0891234567","message":"สวัสดีครับ"}'
# → {"id":"c…","status":"pending","sms_used":1,"sms_remaining":1499}

# ส่งแบบกลุ่ม
curl -X POST localhost:8080/api/v1/sms/batch -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"sender":"MySender","to":["0891234567","0812345678"],"message":"โปรโมชัน"}'

# เช็คสถานะ
curl "localhost:8080/api/v1/sms/status?id=c…" -H "$AUTH"

# ตั้งเวลาส่ง
curl -X POST localhost:8080/api/v1/sms/scheduled -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"to":"0891234567","message":"นัดหมาย","sender":"MySender","scheduledAt":"2030-03-10T03:00:00Z"}'
```

### OTP

```bash
# ขอ OTP
curl -X POST localhost:8080/api/v1/otp/send -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"phone":"0891234567","purpose":"verify"}'
# → {"id":"c…","ref":"GT8PTQXT","phone":"+66891234567","purpose":"verify","expiresAt":"…","expiresIn":300,"smsUsed":1,"smsRemaining":1499}

# ยืนยัน OTP
curl -X POST localhost:8080/api/v1/otp/verify -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"ref":"GT8PTQXT","code":"565414"}'
# → {"valid":true,"verified":true,"ref":"GT8PTQXT","phone":"+66891234567","purpose":"verify"}
```

### ที่เหลือ

```bash
curl localhost:8080/api/v1/balance   -H "$AUTH"   # โควต้าคงเหลือ
curl localhost:8080/api/v1/analytics -H "$AUTH"   # สถิติวันนี้ / เดือนนี้
curl localhost:8080/api/v1/senders   -H "$AUTH"   # ชื่อผู้ส่งที่อนุมัติ
curl localhost:8080/api/v1/contacts  -H "$AUTH"   # รายชื่อผู้ติดต่อ (pagination)
curl -X POST localhost:8080/api/v1/contacts          -H "$AUTH" -H 'Content-Type: application/json' -d '{"name":"สมชาย","phone":"0891234567"}'
curl -X POST localhost:8080/api/v1/contacts/import   -H "$AUTH" -H 'Content-Type: application/json' -d '{"contacts":[{"name":"สมชาย","phone":"0891234567"}]}'
curl -X POST localhost:8080/api/v1/api-keys          -H "$AUTH" -H 'Content-Type: application/json' -d '{"name":"Production Key"}'
```

---

## พฤติกรรมที่จำลองขึ้น

### Lifecycle ของข้อความ

ข้อความจะไล่สถานะตามเวลาโดยอัตโนมัติเมื่อมีคนอ่าน (ไม่มี background worker):

```
pending (0s) → processing (1s) → sent (2s) → delivered (4s)
```

- `status` = สถานะดิบ (`pending`, `processing`, `sent`, `delivered`, `failed`)
- `statusDetail` = สถานะที่แสดงบนหน้าเว็บ (`sent`, `delivered`, `delivery_failed`)
- เมื่อถึง `delivered` จะมี `sentAt` และ `deliveredAt`; เมื่อ `failed` จะมี `detail` และ `detailCategory`

### บังคับให้ล้มเหลว

ส่งไปที่เบอร์ใน `-failure-numbers` (default `0000000000`) จะได้:

```json
{
  "status": "failed",
  "statusDetail": "delivery_failed",
  "detail": "ไม่สามารถส่งข้อความได้ เนื่องจากปลายทางปิดเครื่องหรืออยู่นอกพื้นที่ให้บริการ",
  "detailCategory": "SUBSCRIBER_UNREACHABLE"
}
```

### OTP

- รหัส 6 หลัก, อายุ 5 นาที (`expiresIn: 300`)
- ใส่ผิดได้ 5 ครั้ง (นับรวม) ครั้งที่ 5 จะล็อค → HTTP `410`
- ใช้ซ้ำไม่ได้ (verify สำเร็จแล้ว = ถูก consume) → HTTP `410`
- `ref` ที่ไม่รู้จัก → HTTP `404`
- response ของ 400 จะมี `attempts_remaining` บอกจำนวนครั้งที่เหลือ

รหัส OTP อ่านได้จาก Web UI หรือ management API (`GET /api/otp/latest?phone=…`)
— เหมาะกับการเขียน integration test ที่ต้องกรอก OTP เอง

### Rate limit

เปิดด้วย `-rate-limit` (default ปิด เพราะรำคาญตอนพัฒนา):

| Endpoint | Limit |
|---|---|
| `POST /api/v1/sms/send` | 10 / นาที |
| `POST /api/v1/sms/batch` | 5 / นาที |
| `POST /api/v1/otp/send` | 3 / 5 นาที ต่อเบอร์ |
| `POST /api/v1/otp/verify` | 10 / 15 นาที |
| `POST /api/v1/contacts/import` | 5 / นาที |
| อื่น ๆ | 60 / นาที |

เกินลิมิต → HTTP `429`

### เครดิต

ทุกข้อความที่ส่งใช้ 1 เครดิต (batch คิดตามจำนวนเบอร์) เครดิตหมด → HTTP `402`
ปรับได้ผ่าน `POST /api/credit` หรือ flag `-credit`

---

## Management API (สำหรับ integration test)

อยู่บน port เดียวกับ Web UI (`:8025`)

| Method | Path | ความหมาย |
|---|---|---|
| `GET` | `/api/stats` | สรุปจำนวน email/sms/otp/เครดิต |
| `GET` | `/api/emails?q=&limit=` | รายการอีเมล (ค้นหาได้) |
| `GET` | `/api/emails/{id}` | รายละเอียดอีเมลเต็ม (text/html/headers/attachments) |
| `GET` | `/api/emails/{id}/raw` | ดาวน์โหลด `.eml` ต้นฉบับ |
| `GET` | `/api/emails/{id}/attachments/{index}` | ดาวน์โหลดไฟล์แนบ |
| `POST` | `/api/emails/{id}/read?read=true\|false` | ตั้งสถานะอ่านแล้ว |
| `DELETE` | `/api/emails/{id}` | ลบอีเมล |
| `DELETE` | `/api/emails` | ล้างอีเมลทั้งหมด |
| `GET` | `/api/sms?q=&kind=` | รายการ SMS (`kind` = `sms`, `batch`, `otp`, `scheduled`) |
| `GET` | `/api/sms/{id}` | รายละเอียดข้อความ |
| `DELETE` | `/api/sms/{id}` | ลบข้อความ |
| `DELETE` | `/api/sms` | ล้างข้อความทั้งหมด |
| `GET` | `/api/otps` | รายการ OTP ทั้งหมด (มี `code`) |
| `GET` | `/api/otp/latest?phone=0891234567` | OTP ล่าสุดของเบอร์นั้น |
| `DELETE` | `/api/otps` | ล้าง OTP |
| `POST` | `/api/credit` | ตั้ง/เพิ่มเครดิต `{"credit":100}` หรือ `{"add":50}` |
| `GET` | `/api/health` | health check |

ตัวอย่าง integration test:

```bash
# 1. ให้แอปขอ OTP
REF=$(curl -s -X POST localhost:8080/api/v1/otp/send -H 'Authorization: Bearer sk_test' \
  -H 'Content-Type: application/json' -d '{"phone":"0891234567"}' | jq -r .ref)

# 2. อ่านรหัสจาก smsmail แทนการอ่าน SMS จริง
CODE=$(curl -s "localhost:8025/api/otp/latest?phone=0891234567" | jq -r .code)

# 3. ยืนยัน
curl -s -X POST localhost:8080/api/v1/otp/verify -H 'Authorization: Bearer sk_test' \
  -H 'Content-Type: application/json' -d "{\"ref\":\"$REF\",\"code\":\"$CODE\"}" | jq
```

---

## หมายเหตุเรื่องรูปแบบ response

Response ทุกตัวใช้ชื่อ field แบบเดียวกับที่แอปปลายทางคาดหวัง **รวมถึงความไม่สม่ำเสมอ** ที่มีอยู่:

- `sms_used` / `sms_remaining` (snake_case) ใน `/sms/*` และ `/balance`
- `smsUsed` / `smsRemaining` (camelCase) ใน `/otp/send`
- `senderName` / `creditCost` / `statusDetail` (camelCase) ใน `/sms/status`

จุดที่ไม่ได้ระบุไว้ จึงเลือกตามความสมเหตุสมผล:

- **รูปแบบ error body** — ใช้ `{"error":"…","code":"…"}` (response ของ OTP จะมี `valid`/`verified`/`attempts_remaining` เพิ่ม)
- **`senders[].status`** — ตอบเป็นตัวพิมพ์ใหญ่ (`APPROVED`, `PENDING`)
- **`recipient` ใน `/sms/status`** — echo กลับตามรูปแบบที่ผู้เรียกส่งมา (ส่ง `0891234567` ก็ได้ `0891234567` คืน)
- **ผู้ส่งที่ยังไม่รู้จัก** — auto-register เป็น `APPROVED` เพื่อความสะดวกตอนพัฒนา
- **`/sms/scheduled`** — ยังไม่หักเครดิตตอนตั้งเวลา (หักตอนถึงเวลาส่งจริง)
- **ข้อความ extra fields ใน request body** — ถูกละเว้น ไม่ error

---

## CI/CD (GitHub Actions)

workflow อยู่ที่ [`.github/workflows/`](.github/workflows)

### `ci.yml` — รันทุก push และ pull request

| Job | ทำอะไร |
|---|---|
| `test` | matrix 3 OS (ubuntu / macos / windows) — `gofmt -l`, `go vet`, `go test -race`, `go build` |
| `staticcheck` | [`staticcheck`](https://staticcheck.dev) เวอร์ชัน pin ไว้ (`2025.1.1`) |
| `docker` | build image (ไม่ push) เพื่อจับ Dockerfile พัง |

รันพร้อมกันหลาย branch ได้โดยไม่ตีกัน — push ใหม่จะยกเลิก run เก่าที่ค้างอยู่ (`concurrency.cancel-in-progress`)
coverage report ถูก upload เป็น artifact ชื่อ `coverage` ให้ดาวน์โหลดได้จากหน้า run

รันให้ตรงกับ CI ในเครื่องก่อน push:

```bash
make check     # gofmt + vet + test + staticcheck
```

### `release.yml` — ปล่อยเวอร์ชัน

trigger ด้วยการ push tag ที่ขึ้นต้นด้วย `v`:

```bash
git tag v0.1.0
git push origin v0.1.0
```

สิ่งที่เกิดขึ้น:

1. **`binaries`** — cross-compile 5 เป้า (linux/darwin/windows) ด้วย `CGO_ENABLED=0`
   inject เวอร์ชันผ่าน `-ldflags` (`main.version`, `main.commit`, `main.date`)
   แพ็คเป็น `smsmail_<version>_<os>_<arch>.tar.gz` (windows เป็น `.zip`) ใส่ `README.md` ไปด้วย
2. **`release`** — รวมทุก archive สร้าง `SHA256SUMS` แล้วสร้าง GitHub Release พร้อม release notes อัตโนมัติ
3. **`docker`** — build และ push image ขึ้น GHCR ด้วย tag `latest`, `0.1.0`, `0.1`

ตรวจสอบไฟล์ที่โหลดมา:

```bash
sha256sum -c SHA256SUMS --ignore-missing
```

**หมายเหตุ**

- ไม่ต้องตั้ง secret เอง — ใช้ `GITHUB_TOKEN` ที่ Actions ให้มาอยู่แล้ว (`contents: write` สำหรับ release, `packages: write` สำหรับ GHCR)
- ถ้า release ล้มกลางทาง กด **Re-run all jobs** ในหน้า Actions ได้ หรือลบ tag แล้ว push ใหม่
- ถ้า package ใน GHCR ขึ้นเป็น private ครั้งแรก ให้ไปตั้งเป็น public ที่ Packages → Package settings
- โปรเจกต์นี้ยังไม่มี `LICENSE` — ถ้าใส่ภายหลัง release workflow จะคัดลอกลง archive ให้เองอัตโนมัติ

---

## พัฒนา

```bash
make build     # build binary
make test      # go test ./...
make vet       # go vet ./...
make check     # gofmt + vet + test + staticcheck (เหมือน CI)
make run       # go run ./cmd/smsmail
make fmt       # gofmt
make clean
```

โครงสร้างโปรเจกต์:

```
cmd/smsmail/main.go       # entrypoint, wiring, graceful shutdown
internal/config/          # flags + env
internal/store/           # in-memory store (email, sms, otp, contacts, credit, rate limit)
internal/smtpd/           # SMTP catcher + MIME parsing
internal/smsapi/          # mock SMS API (13 endpoints)
web/                      # Web UI + management API + embedded assets
.github/workflows/        # CI + release pipelines
Dockerfile                # multi-stage build, alpine runtime, non-root user
```

Dependencies มีแค่ `github.com/emersion/go-smtp`, `github.com/emersion/go-message`
และ `golang.org/x/text` (สำหรับ decode charset) — router ใช้ `net/http.ServeMux` มาตรฐาน
