# data-export API fixture sample

本样本来自自动化 handler 的虚构空账号/default Settings fixture；不含 owner 或真实客户数据。

```http
HTTP/1.1 200 OK
Content-Type: application/json
Content-Disposition: attachment; filename="photographer-crm-export-20260721T083015Z.json"
Content-Length: 531
Cache-Control: no-store
X-Content-Type-Options: nosniff
```

```json
{"counts":{"customer_notes":0,"customers":0,"orders":0,"packages":0,"reminders":0,"schedule_slots":0,"social_identities":0},"customer_notes":[],"customers":[],"exported_at":"2026-07-21T08:30:15Z","orders":[],"packages":[],"reminders":[],"schedule_slots":[],"schema_version":1,"settings":{"timezone":"Asia/Shanghai","birthday_lead_days":3,"follow_up_after_days":7,"churn_thresholds":[{"shoot_type":"portrait","days":180},{"shoot_type":"cosplay","days":180},{"shoot_type":"other","days":180}],"digest_hour":9},"social_identities":[]}
```

核对：UTF-8 bytes 长度为 531；七个数组均为 `[]`，七项 counts 均为 0；settings 为完整默认值；样本不含头像二进制、内部对象信息、凭证或运行状态。
